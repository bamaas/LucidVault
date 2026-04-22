package raindrop

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"lucidvault/internal/source"
)

const baseURL = "https://api.raindrop.io/rest/v1"

// Compile-time check that Client implements source.Client.
var _ source.Client = (*Client)(nil)

func init() {
	source.Register("raindrop", func(token string) source.Client {
		return NewClient(token)
	})
}

type Client struct {
	token      string
	httpClient *http.Client
}

type raindropBookmark struct {
	ID      int       `json:"_id"`
	Title   string    `json:"title"`
	Link    string    `json:"link"`
	Excerpt string    `json:"excerpt"`
	Tags    []string  `json:"tags"`
	Created time.Time `json:"created"`
}

type raindropsResponse struct {
	Items []raindropBookmark `json:"items"`
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

// FetchBookmarks fetches bookmarks from Raindrop, newest first.
// It paginates through all results since lastSyncAt.
// Returns bookmarks in chronological order (oldest first) for processing.
func (c *Client) FetchBookmarks(lastSyncAt time.Time, batchSize int) ([]source.Bookmark, error) {
	var allBookmarks []source.Bookmark

	for page := 0; ; page++ {
		url := fmt.Sprintf("%s/raindrops/0?sort=created&page=%d&perpage=25", baseURL, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching raindrops page %d: %w", page, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("raindrop API returned %d: %s", resp.StatusCode, string(body))
		}

		var result raindropsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parsing raindrops response: %w", err)
		}

		if len(result.Items) == 0 {
			break
		}

		for _, item := range result.Items {
			if !item.Created.After(lastSyncAt) {
				continue // already seen
			}
			allBookmarks = append(allBookmarks, source.Bookmark{
				ID:      item.ID,
				Title:   item.Title,
				Link:    item.Link,
				Excerpt: item.Excerpt,
				Tags:    item.Tags,
				Created: item.Created,
			})
			if batchSize > 0 && len(allBookmarks) >= batchSize {
				return allBookmarks, nil
			}
		}

		slog.Info("fetched raindrop page", "page", page, "items", len(result.Items), "total", len(allBookmarks))
	}

	return allBookmarks, nil
}
