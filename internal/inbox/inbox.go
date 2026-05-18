package inbox

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"lucidvault/internal/notes"
)

// Item represents a parsed inbox file ready for processing.
type Item struct {
	Path  string   // absolute path to the inbox file
	URL   string   // extracted URL
	Title string   // from frontmatter or filename
	Tags  []string // from frontmatter (optional)
}

// Scan reads all .md files from vaultPath/inbox/ and parses them into Items.
// Files without a valid URL are skipped with a warning.
func Scan(vaultPath string) ([]Item, error) {
	inboxDir := filepath.Join(vaultPath, "inbox")

	if _, err := os.Stat(inboxDir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(inboxDir)
	if err != nil {
		return nil, fmt.Errorf("reading inbox directory: %w", err)
	}

	var items []Item
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		absPath := filepath.Join(inboxDir, entry.Name())
		data, err := os.ReadFile(absPath)
		if err != nil {
			slog.Warn("skipping unreadable inbox file", "file", entry.Name(), "error", err)
			continue
		}

		content := string(data)
		if strings.TrimSpace(content) == "" {
			slog.Warn("skipping empty inbox file", "file", entry.Name())
			continue
		}

		url := extractURL(content)
		if url == "" {
			slog.Warn("skipping inbox file without URL", "file", entry.Name())
			continue
		}

		title := notes.ParseTitle(content)
		if title == "" {
			title = notes.TitleFromFilename(entry.Name())
		}

		tags := notes.ParseFrontmatter(content)

		items = append(items, Item{
			Path:  absPath,
			URL:   url,
			Title: title,
			Tags:  tags,
		})
	}

	return items, nil
}

// Delete removes a processed inbox file. Returns nil if the file doesn't exist.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting inbox file %s: %w", path, err)
	}
	return nil
}

// extractURL finds the first line starting with "http" after any frontmatter.
func extractURL(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	body := content
	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		end := strings.Index(rest, "\n---")
		if end != -1 {
			body = rest[end+4:]
		}
	}

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}

	return ""
}
