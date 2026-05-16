package enrich

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func chatJSON(content string) []byte {
	resp := chatResponse{}
	resp.Message.Content = content
	b, _ := json.Marshal(resp)
	return b
}

const validContent = `---
title: "Test"
source: "http://example.com"
date_saved: 2024-01-01
tags:
  - test
type: bookmark
---

# Test

## Summary
This is a test summary.

## Key Takeaways
- Test takeaway
`

func TestEnrich_Success(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON(validContent))
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "## Summary") {
		t.Errorf("expected result to contain Summary section, got: %s", result)
	}
}

func TestEnrich_RetryOnValidationFailure(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: return invalid content (no frontmatter)
			_, _ = w.Write(chatJSON("no frontmatter here"))
		} else {
			// Second call: return valid content
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "## Summary") {
		t.Errorf("expected valid result after retry, got: %s", result)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 API calls, got %d", calls.Load())
	}
}

func TestEnrich_FallbackOnPersistentValidationFailure(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatJSON("garbage output"))
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Fallback Test",
		URL:       "http://example.com/fallback",
		DateSaved: "2024-01-01",
		UserTags:  []string{"test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Enrichment failed") {
		t.Errorf("expected minimal fallback page, got: %s", result)
	}
}

func TestEnrich_RetryOnHTTPError(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "## Summary") {
		t.Errorf("expected valid result after retry, got: %s", result)
	}
}

func TestEnrich_RateLimitBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(chatJSON(validContent))
		}
	})
	defer srv.Close()

	c := NewClient("key", "model", 1, 0)
	c.SetBaseURL(srv.URL)

	result, err := c.Enrich(context.Background(), &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "## Summary") {
		t.Errorf("expected valid result after rate limit retry")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 API calls, got %d", calls.Load())
	}
}

func TestEnrich_ContextCancellation(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		// Slow server — never responds
		<-r.Context().Done()
	})
	defer srv.Close()

	c := NewClient("key", "model", 0, 0)
	c.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.Enrich(ctx, &EnrichInput{
		Title:     "Test",
		URL:       "http://example.com",
		DateSaved: "2024-01-01",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestCleanResponse_StripMarkdownFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fences",
			input: "---\ntitle: test\n---",
			want:  "---\ntitle: test\n---",
		},
		{
			name:  "markdown fence",
			input: "```markdown\n---\ntitle: test\n---\n```",
			want:  "---\ntitle: test\n---",
		},
		{
			name:  "generic fence",
			input: "```\n---\ntitle: test\n---\n```",
			want:  "---\ntitle: test\n---",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanResponse(tt.input)
			if got != tt.want {
				t.Errorf("cleanResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateResponse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid",
			content: validContent,
			wantErr: false,
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading",
			wantErr: true,
		},
		{
			name:    "missing title",
			content: "---\ntags:\n  - test\n---\n\n## Summary\nfoo\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing tags",
			content: "---\ntitle: test\n---\n\n## Summary\nfoo\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing summary",
			content: "---\ntitle: test\ntags:\n  - x\n---\n\n## Key Takeaways\nbar",
			wantErr: true,
		},
		{
			name:    "missing key takeaways",
			content: "---\ntitle: test\ntags:\n  - x\n---\n\n## Summary\nfoo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResponse(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildMinimalPage(t *testing.T) {
	input := &EnrichInput{
		Title:       "Test Title",
		URL:         "http://example.com",
		DateSaved:   "2024-01-01",
		UserTags:    []string{"go", "testing"},
		RawFilename: "2024-01-01-test-title.md",
	}

	result := buildMinimalPage(input)

	if !strings.Contains(result, `title: "Test Title"`) {
		t.Error("expected title in frontmatter")
	}
	if !strings.Contains(result, "## Summary") {
		t.Error("expected Summary section")
	}
	if !strings.Contains(result, "## Key Takeaways") {
		t.Error("expected Key Takeaways section")
	}
	if !strings.Contains(result, "Enrichment failed") {
		t.Error("expected fallback message")
	}
	if !strings.Contains(result, "- go") {
		t.Error("expected user tags")
	}
}
