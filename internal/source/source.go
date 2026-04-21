package source

import (
	"fmt"
	"time"
)

// Bookmark represents a saved item from any bookmark source.
type Bookmark struct {
	ID      int
	Title   string
	Link    string
	Excerpt string
	Tags    []string
	Created time.Time
}

// Client fetches bookmarks from an external source.
type Client interface {
	FetchBookmarks(lastSyncAt time.Time, batchSize int) ([]Bookmark, error)
}

// Factory is a constructor that creates a Client from a token.
type Factory func(token string) Client

var registry = map[string]Factory{}

// Register adds a named source factory to the registry.
func Register(name string, f Factory) {
	registry[name] = f
}

// NewClient creates a Client for the given source name.
func NewClient(name, token string) (Client, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown source: %q", name)
	}
	return f(token), nil
}
