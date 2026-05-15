package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestIsYouTubeURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		// Positive cases
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://youtu.be/dQw4w9WgXcQ", true},
		{"https://www.youtube.com/shorts/abc123", true},
		{"http://youtube.com/watch?v=abc", true},
		{"https://youtube.com/watch?v=abc&list=PLxyz", true},

		// Negative cases
		{"https://www.youtube.com/", false},
		{"https://www.youtube.com/channel/UCxyz", false},
		{"https://www.youtube.com/playlist?list=PLxyz", false},
		{"https://youtu.be/", false},
		{"https://example.com/watch?v=abc", false},
		{"https://notyoutube.com/watch?v=abc", false},
		{"not a url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsYouTubeURL(tt.url)
			if got != tt.want {
				t.Errorf("IsYouTubeURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestFetchTranscript_Sync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("text") != "true" {
			t.Error("expected text=true query param")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transcriptResponse{
			Content: "Hello world transcript",
			Lang:    "en",
		})
	}))
	defer srv.Close()

	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(srv.URL)

	result, err := yt.FetchTranscript(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatal("expected OK=true")
	}
	if result.Content != "Hello world transcript" {
		t.Errorf("got content %q, want %q", result.Content, "Hello world transcript")
	}
}

func TestFetchTranscript_Async(t *testing.T) {
	var pollCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Initial request returns 202 with jobId
		if r.URL.Path == "/v1/transcript" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(asyncResponse{JobID: "job-123"})
			return
		}

		// Poll endpoint
		if r.URL.Path == "/v1/transcript/job-123" {
			count := pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if count < 3 {
				_ = json.NewEncoder(w).Encode(jobResult{Status: "active"})
			} else {
				_ = json.NewEncoder(w).Encode(jobResult{
					Status:  "completed",
					Content: "Async transcript content",
					Lang:    "en",
				})
			}
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(srv.URL)

	result, err := yt.FetchTranscript(context.Background(), "https://www.youtube.com/watch?v=long-video")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatal("expected OK=true")
	}
	if result.Content != "Async transcript content" {
		t.Errorf("got content %q, want %q", result.Content, "Async transcript content")
	}
	if pollCount.Load() != 3 {
		t.Errorf("expected 3 poll attempts, got %d", pollCount.Load())
	}
}

func TestFetchTranscript_AsyncFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/transcript" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(asyncResponse{JobID: "job-fail"})
			return
		}

		if r.URL.Path == "/v1/transcript/job-fail" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jobResult{
				Status: "failed",
				Error: &struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				}{Code: "processing-error", Message: "could not process video"},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(srv.URL)

	result, err := yt.FetchTranscript(context.Background(), "https://www.youtube.com/watch?v=fail")
	if err == nil {
		t.Fatal("expected error")
	}
	if result.OK {
		t.Fatal("expected OK=false")
	}
}

func TestFetchTranscript_TranscriptUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(206)
		_, _ = fmt.Fprint(w, `{"error":{"code":"transcript-unavailable","message":"no transcript"}}`)
	}))
	defer srv.Close()

	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(srv.URL)

	result, err := yt.FetchTranscript(context.Background(), "https://www.youtube.com/watch?v=notranscript")
	if err == nil {
		t.Fatal("expected error")
	}
	if result.OK {
		t.Fatal("expected OK=false")
	}
}

func TestFetchTranscript_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"invalid key"}}`)
	}))
	defer srv.Close()

	yt := NewYouTubeClient("bad-key")
	yt.SetBaseURL(srv.URL)

	result, err := yt.FetchTranscript(context.Background(), "https://www.youtube.com/watch?v=abc")
	if err == nil {
		t.Fatal("expected error")
	}
	if result.OK {
		t.Fatal("expected OK=false")
	}
}

func TestScrape_RoutesToYouTube(t *testing.T) {
	ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transcriptResponse{
			Content: "youtube transcript",
			Lang:    "en",
		})
	}))
	defer ytSrv.Close()

	sc := New()
	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(ytSrv.URL)
	sc.SetYouTubeClient(yt)

	result, err := sc.Scrape(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "youtube transcript" {
		t.Errorf("got content %q, want %q", result.Content, "youtube transcript")
	}
}

func TestScrape_YouTubeWithoutClient(t *testing.T) {
	jinaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "# YouTube page markdown")
	}))
	defer jinaSrv.Close()

	sc := New()
	sc.SetBaseURL(jinaSrv.URL + "/")

	result, err := sc.Scrape(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# YouTube page markdown" {
		t.Errorf("got content %q, want jina fallback", result.Content)
	}
}

func TestScrape_NonYouTubeUsesJina(t *testing.T) {
	jinaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "# Article content")
	}))
	defer jinaSrv.Close()

	ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("YouTube client should not be called for non-YouTube URL")
	}))
	defer ytSrv.Close()

	sc := New()
	sc.SetBaseURL(jinaSrv.URL + "/")
	yt := NewYouTubeClient("test-key")
	yt.SetBaseURL(ytSrv.URL)
	sc.SetYouTubeClient(yt)

	result, err := sc.Scrape(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "# Article content" {
		t.Errorf("got content %q, want jina content", result.Content)
	}
}
