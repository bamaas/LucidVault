package scraper

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

type Scraper struct {
	httpClient *http.Client
}

type Result struct {
	Content string
	OK      bool
}

func New() *Scraper {
	return &Scraper{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

// Scrape fetches the markdown content of a URL via Jina Reader.
func (s *Scraper) Scrape(targetURL string) (*Result, error) {
	jinaURL := "https://r.jina.ai/" + targetURL
	req, err := http.NewRequest("GET", jinaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating jina request: %w", err)
	}
	req.Header.Set("Accept", "text/markdown")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("jina scrape of %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{OK: false}, fmt.Errorf("reading jina response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &Result{OK: false}, fmt.Errorf("jina returned %d for %s", resp.StatusCode, targetURL)
	}

	return &Result{Content: string(body), OK: true}, nil
}
