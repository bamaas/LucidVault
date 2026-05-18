package raindrop

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// SyncToInbox creates inbox files for bookmarks that haven't been processed yet.
// It checks the DB for already-processed URLs and skips them.
// Returns the number of new inbox files created.
func SyncToInbox(bookmarks []Bookmark, db *store.Store, vaultPath string) (int, error) {
	inboxDir := filepath.Join(vaultPath, "inbox")
	created := 0

	for _, bm := range bookmarks {
		normalizedURL := vault.NormalizeURL(bm.Link)

		// Skip if already processed
		processed, err := db.IsProcessedByURL(normalizedURL)
		if err != nil {
			return created, fmt.Errorf("checking processed status for %q: %w", bm.Link, err)
		}
		if processed {
			continue
		}

		// Skip if inbox file already exists (may be a slug collision from a
		// different URL with a similar title — will resolve next cycle after
		// the existing file is processed and deleted).
		slug := vault.GenerateSlug(bm.Title)
		inboxFile := filepath.Join(inboxDir, slug+".md")
		if _, err := os.Stat(inboxFile); err == nil {
			slog.Warn("inbox file already exists, skipping", "slug", slug, "url", bm.Link)
			continue
		}

		// Write inbox file
		content := formatInboxFile(bm)
		if err := os.WriteFile(inboxFile, []byte(content), 0o644); err != nil {
			return created, fmt.Errorf("writing inbox file %s: %w", inboxFile, err)
		}

		slog.Info("created inbox file from raindrop", "title", bm.Title, "slug", slug)
		created++
	}

	return created, nil
}

// formatInboxFile creates the content for an inbox file with optional frontmatter.
func formatInboxFile(bm Bookmark) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %q\n", bm.Title)
	if len(bm.Tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range bm.Tags {
			fmt.Fprintf(&b, "  - %s\n", tag)
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(bm.Link)
	b.WriteString("\n")
	return b.String()
}
