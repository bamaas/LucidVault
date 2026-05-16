package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Scraper struct {
	baseURL    string
	httpClient *http.Client
	youtube    *YouTubeClient
}

// maxResponseBytes limits the size of scrape responses to prevent OOM (10 MB).
const maxResponseBytes = 10 * 1024 * 1024

type Result struct {
	Content string
	OK      bool
}

func New() *Scraper {
	return &Scraper{
		baseURL: "https://r.jina.ai/",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

// SetBaseURL overrides the Jina Reader endpoint (used in tests).
func (s *Scraper) SetBaseURL(url string) { s.baseURL = url }

// SetYouTubeClient attaches a Supadata YouTube client for transcript fetching.
func (s *Scraper) SetYouTubeClient(yt *YouTubeClient) { s.youtube = yt }

// Scrape fetches content for a URL. YouTube URLs are routed to Supadata
// for transcript extraction when a YouTubeClient is configured.
func (s *Scraper) Scrape(ctx context.Context, targetURL string) (*Result, error) {
	if s.youtube != nil && IsYouTubeURL(targetURL) {
		return s.youtube.FetchTranscript(ctx, targetURL)
	}

	jinaURL := s.baseURL + targetURL
	req, err := http.NewRequestWithContext(ctx, "GET", jinaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating jina request: %w", err)
	}
	req.Header.Set("Accept", "text/markdown")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("jina scrape of %s: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("reading jina response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &Result{OK: false}, fmt.Errorf("jina returned %d for %s", resp.StatusCode, targetURL)
	}

	return &Result{Content: string(body), OK: true}, nil
}
