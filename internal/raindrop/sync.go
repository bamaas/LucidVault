package raindrop

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// SyncToInbox creates inbox files for bookmarks not yet known to the DB.
// Each new URL is recorded in the DB immediately, so it won't be re-added
// on subsequent runs — even if the user deletes the inbox file to reject it.
// When force is true, the DB dedup check is skipped and inbox files are
// created for all bookmarks (existing inbox files on disk are still skipped).
// Returns the number of new inbox files created.
func SyncToInbox(bookmarks []Bookmark, db *store.Store, vaultPath string, force bool) (int, error) {
	inboxDir := filepath.Join(vaultPath, "inbox")
	created := 0

	for _, bm := range bookmarks {
		normalizedURL := vault.NormalizeURL(bm.Link)

		// Skip if already processed (unless force is set)
		if !force {
			processed, err := db.IsProcessedByURL(normalizedURL)
			if err != nil {
				return created, fmt.Errorf("checking processed status for %q: %w", bm.Link, err)
			}
			if processed {
				continue
			}
		}

		// Atomically create inbox file (O_EXCL fails if file already exists,
		// avoiding TOCTOU race between stat and write). An existing file may be
		// a slug collision from a different URL — will resolve next cycle after
		// the existing file is processed and deleted.
		slug := vault.GenerateSlug(bm.Title)
		inboxFile := filepath.Join(inboxDir, slug+".md")
		content := formatInboxFile(bm)

		f, err := os.OpenFile(inboxFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			slog.Warn("inbox file already exists, skipping", "slug", slug, "url", bm.Link)
			continue
		}
		if err != nil {
			return created, fmt.Errorf("creating inbox file %s: %w", inboxFile, err)
		}
		_, writeErr := f.WriteString(content)
		closeErr := f.Close()
		if writeErr != nil {
			return created, fmt.Errorf("writing inbox file %s: %w", inboxFile, writeErr)
		}
		if closeErr != nil {
			return created, fmt.Errorf("closing inbox file %s: %w", inboxFile, closeErr)
		}

		// Record in DB so the URL is never re-added to inbox (e.g. after
		// restart or if the user deletes the file to reject it).
		// When force=true, skip the upsert for already-processed bookmarks
		// to avoid overwriting their wiki_path/raw_path with empty values.
		skipUpsert := false
		if force {
			processed, err := db.IsProcessedByURL(normalizedURL)
			if err != nil {
				return created, fmt.Errorf("checking processed status for %q: %w", bm.Link, err)
			}
			skipUpsert = processed
		}
		if !skipUpsert {
			if err := db.UpsertBookmark(&store.BookmarkRecord{
				Title:         bm.Title,
				URL:           bm.Link,
				URLNormalized: normalizedURL,
				ProcessedAt:   time.Now(),
			}); err != nil {
				return created, fmt.Errorf("recording bookmark %q: %w", bm.Link, err)
			}
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
