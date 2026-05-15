package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// YouTubeClient fetches video transcripts via the Supadata API.
type YouTubeClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

type transcriptResponse struct {
	Content string `json:"content"`
	Lang    string `json:"lang"`
}

type asyncResponse struct {
	JobID string `json:"jobId"`
}

type jobResult struct {
	Status  string `json:"status"`
	Content string `json:"content"`
	Lang    string `json:"lang"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

const (
	pollInterval = 1 * time.Second
	maxPollAttempts = 60
)

// NewYouTubeClient creates a Supadata transcript client.
func NewYouTubeClient(apiKey string) *YouTubeClient {
	return &YouTubeClient{
		apiKey:  apiKey,
		baseURL: "https://api.supadata.ai",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

// SetBaseURL overrides the Supadata API endpoint (used in tests).
func (yt *YouTubeClient) SetBaseURL(u string) { yt.baseURL = u }

// FetchTranscript retrieves the transcript for a YouTube video URL.
// It handles both synchronous (200) and asynchronous (202) responses.
func (yt *YouTubeClient) FetchTranscript(ctx context.Context, videoURL string) (*Result, error) {
	endpoint := fmt.Sprintf("%s/v1/transcript?url=%s&text=true", yt.baseURL, url.QueryEscape(videoURL))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("creating supadata request: %w", err)
	}
	req.Header.Set("x-api-key", yt.apiKey)

	resp, err := yt.httpClient.Do(req)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("supadata transcript request for %s: %w", videoURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("reading supadata response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var tr transcriptResponse
		if err := json.Unmarshal(body, &tr); err != nil {
			return &Result{OK: false}, fmt.Errorf("parsing supadata response: %w", err)
		}
		return &Result{Content: tr.Content, OK: true}, nil

	case http.StatusAccepted:
		var ar asyncResponse
		if err := json.Unmarshal(body, &ar); err != nil {
			return &Result{OK: false}, fmt.Errorf("parsing supadata async response: %w", err)
		}
		if ar.JobID == "" {
			return &Result{OK: false}, fmt.Errorf("supadata returned 202 without jobId")
		}
		return yt.pollJob(ctx, ar.JobID)

	case 206:
		return &Result{OK: false}, fmt.Errorf("supadata: transcript unavailable for %s", videoURL)

	default:
		return &Result{OK: false}, fmt.Errorf("supadata returned %d for %s: %s", resp.StatusCode, videoURL, string(body))
	}
}

func (yt *YouTubeClient) pollJob(ctx context.Context, jobID string) (*Result, error) {
	endpoint := fmt.Sprintf("%s/v1/transcript/%s", yt.baseURL, url.PathEscape(jobID))

	for range maxPollAttempts {
		select {
		case <-ctx.Done():
			return &Result{OK: false}, fmt.Errorf("context cancelled while polling job %s: %w", jobID, ctx.Err())
		case <-time.After(pollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return &Result{OK: false}, fmt.Errorf("creating poll request: %w", err)
		}
		req.Header.Set("x-api-key", yt.apiKey)

		resp, err := yt.httpClient.Do(req)
		if err != nil {
			return &Result{OK: false}, fmt.Errorf("polling supadata job %s: %w", jobID, err)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return &Result{OK: false}, fmt.Errorf("reading poll response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return &Result{OK: false}, fmt.Errorf("supadata poll returned %d for job %s: %s", resp.StatusCode, jobID, string(body))
		}

		var jr jobResult
		if err := json.Unmarshal(body, &jr); err != nil {
			return &Result{OK: false}, fmt.Errorf("parsing poll response: %w", err)
		}

		switch jr.Status {
		case "completed":
			return &Result{Content: jr.Content, OK: true}, nil
		case "failed":
			msg := "unknown error"
			if jr.Error != nil {
				msg = jr.Error.Message
			}
			return &Result{OK: false}, fmt.Errorf("supadata job %s failed: %s", jobID, msg)
		case "queued", "active":
			// keep polling
		default:
			return &Result{OK: false}, fmt.Errorf("supadata job %s unknown status: %s", jobID, jr.Status)
		}
	}

	return &Result{OK: false}, fmt.Errorf("supadata job %s timed out after %d attempts", jobID, maxPollAttempts)
}

// IsYouTubeURL reports whether the given URL points to a YouTube video.
func IsYouTubeURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	switch host {
	case "youtube.com":
		return u.Path == "/watch" || strings.HasPrefix(u.Path, "/shorts/")
	case "youtu.be":
		return u.Path != "" && u.Path != "/"
	default:
		return false
	}
}
