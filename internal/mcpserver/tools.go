package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lucidvault/internal/vault"
)

// safeReadFile validates that a relative path stays within the vault before reading.
func safeReadFile(v *vault.Vault, relPath string) (string, error) {
	absPath := filepath.Join(v.BasePath, relPath)
	cleanAbs := filepath.Clean(absPath)
	cleanBase := filepath.Clean(v.BasePath)
	if !strings.HasPrefix(cleanAbs, cleanBase+string(os.PathSeparator)) && cleanAbs != cleanBase {
		return "", fmt.Errorf("path escapes vault: %q", relPath)
	}
	return v.ReadFile(relPath)
}

// GrepResult represents a single match from grep_vault.
type GrepResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// RelatedEntry represents a linked page from related_notes.
type RelatedEntry struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Exists bool   `json:"exists"`
}

// HandleGetSoul returns the content of soul.md.
func HandleGetSoul(v *vault.Vault) (string, error) {
	content, err := v.ReadSoul()
	if err != nil {
		return "", fmt.Errorf("reading soul.md: %w", err)
	}
	if content == "" {
		return "No soul.md found. Create one at the vault root to personalize your experience.", nil
	}
	return content, nil
}

// HandleSearchIndex searches index.md for entries matching the query.
func HandleSearchIndex(v *vault.Vault, query string) ([]IndexEntry, error) {
	indexContent, err := v.ReadIndex()
	if err != nil {
		return nil, fmt.Errorf("reading index: %w", err)
	}

	queryLower := strings.ToLower(query)
	var results []IndexEntry

	for _, line := range strings.Split(indexContent, "\n") {
		entry := ParseIndexEntry(line)
		if entry == nil {
			continue
		}

		// Match against slug, title, and tags (case insensitive).
		if matchesQuery(entry, queryLower) {
			results = append(results, *entry)
		}
	}

	if results == nil {
		results = []IndexEntry{}
	}
	return results, nil
}

func matchesQuery(entry *IndexEntry, queryLower string) bool {
	if strings.Contains(strings.ToLower(entry.Slug), queryLower) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Title), queryLower) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), queryLower) {
			return true
		}
	}
	return false
}

// HandleReadWiki reads a wiki page by slug.
func HandleReadWiki(v *vault.Vault, slug string) (string, error) {
	content, err := safeReadFile(v, "wiki/"+slug+".md")
	if err != nil {
		return "", fmt.Errorf("wiki page %q not found: %w", slug, err)
	}
	return content, nil
}

// HandleGrepVault searches for a query string across vault files in the given scope.
func HandleGrepVault(v *vault.Vault, query, scope string) ([]GrepResult, error) {
	if scope == "" {
		scope = "wiki"
	}

	var dirs []string
	switch scope {
	case "wiki":
		dirs = []string{"wiki"}
	case "notes":
		dirs = []string{"notes"}
	case "raw":
		dirs = []string{"raw"}
	case "all":
		dirs = []string{"wiki", "notes", "raw"}
	default:
		dirs = []string{"wiki"}
	}

	queryLower := strings.ToLower(query)
	var results []GrepResult
	const maxResults = 20

	for _, dir := range dirs {
		dirPath := filepath.Join(v.BasePath, dir)
		err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			if len(results) >= maxResults {
				return filepath.SkipAll
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil // skip unreadable files
			}

			relPath, _ := filepath.Rel(v.BasePath, path)
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), queryLower) {
					results = append(results, GrepResult{
						File:    relPath,
						Line:    i + 1,
						Content: line,
					})
					if len(results) >= maxResults {
						return filepath.SkipAll
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("searching %s: %w", dir, err)
		}
	}

	if results == nil {
		results = []GrepResult{}
	}
	return results, nil
}

// HandleReadNote reads a personal note by path. Path must start with "notes/".
func HandleReadNote(v *vault.Vault, path string) (string, error) {
	if !strings.HasPrefix(path, "notes/") {
		return "", fmt.Errorf("path must start with notes/, got %q", path)
	}
	content, err := safeReadFile(v, path)
	if err != nil {
		return "", fmt.Errorf("note %q not found: %w", path, err)
	}
	return content, nil
}

// HandleReadRaw reads a raw source file by filename.
func HandleReadRaw(v *vault.Vault, filename string) (string, error) {
	if strings.Contains(filename, "/") || strings.Contains(filename, "..") {
		return "", fmt.Errorf("filename must not contain path separators: %q", filename)
	}
	content, err := safeReadFile(v, "raw/"+filename)
	if err != nil {
		return "", fmt.Errorf("raw file %q not found: %w", filename, err)
	}
	return content, nil
}

// HandleRelatedNotes returns pages linked from the given wiki page.
func HandleRelatedNotes(v *vault.Vault, slug string) ([]RelatedEntry, error) {
	content, err := safeReadFile(v, "wiki/"+slug+".md")
	if err != nil {
		return nil, fmt.Errorf("wiki page %q not found: %w", slug, err)
	}

	links := ParseWikiLinks(content)
	var results []RelatedEntry
	for _, link := range links {
		entry := RelatedEntry{Slug: link}

		// Determine file path based on link type.
		var relPath string
		if strings.HasPrefix(link, "notes/") {
			relPath = link + ".md"
		} else {
			relPath = "wiki/" + link + ".md"
		}

		entry.Exists = v.FileExists(relPath)
		if entry.Exists {
			fileContent, err := v.ReadFile(relPath)
			if err == nil {
				entry.Title = ParseFrontmatterTitle(fileContent)
			}
		}
		if entry.Title == "" {
			// Use slug as fallback title.
			parts := strings.Split(link, "/")
			entry.Title = parts[len(parts)-1]
		}

		results = append(results, entry)
	}

	if results == nil {
		results = []RelatedEntry{}
	}
	return results, nil
}
