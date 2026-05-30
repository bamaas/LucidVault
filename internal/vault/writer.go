package vault

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]`)

type Vault struct {
	BasePath string
}

func New(basePath string) *Vault {
	return &Vault{BasePath: basePath}
}

func (v *Vault) Init() error {
	dirs := []string{"raw", "wiki", "notes", "templates", "inbox"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(v.BasePath, d), 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	indexPath := filepath.Join(v.BasePath, "index.md")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		content := fmt.Sprintf("# Wiki Index\n\nLast updated: %s\n\n## Pages\n\n", time.Now().Format("2006-01-02"))
		if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating index.md: %w", err)
		}
	}

	// Create note template
	noteTemplatePath := filepath.Join(v.BasePath, "templates", "note.md")
	if _, err := os.Stat(noteTemplatePath); os.IsNotExist(err) {
		content := `---
date: "{{date}}"
tags: []
---

#
`
		if err := os.WriteFile(noteTemplatePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating note template: %w", err)
		}
	}

	// Create inbox template
	inboxTemplatePath := filepath.Join(v.BasePath, "templates", "inbox.md")
	if _, err := os.Stat(inboxTemplatePath); os.IsNotExist(err) {
		content := `---
title: ""
tags: []
---

https://
`
		if err := os.WriteFile(inboxTemplatePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating inbox template: %w", err)
		}
	}

	return nil
}

// ScanWikiDir returns a list of relative paths (e.g. "wiki/foo.md", "wiki/notes/bar.md")
// for all .md files under the wiki/ directory. Returns empty slice if wiki/ doesn't exist.
func (v *Vault) ScanWikiDir() ([]string, error) {
	wikiDir := filepath.Join(v.BasePath, "wiki")
	if _, err := os.Stat(wikiDir); os.IsNotExist(err) {
		return nil, nil
	}

	var paths []string
	err := filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(v.BasePath, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning wiki directory: %w", err)
	}
	return paths, nil
}

func (v *Vault) FileExists(relPath string) bool {
	absPath := filepath.Join(v.BasePath, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(data))) > 0
}

func (v *Vault) ReadFile(relPath string) (string, error) {
	absPath := filepath.Join(v.BasePath, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading file %s: %w", relPath, err)
	}
	return string(data), nil
}


func (v *Vault) DeleteFile(relPath string) error {
	absPath := filepath.Join(v.BasePath, relPath)
	if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting file %s: %w", relPath, err)
	}
	return nil
}

func (v *Vault) WriteRaw(filename, content string) (string, error) {
	relPath := filepath.Join("raw", filename)
	absPath := filepath.Join(v.BasePath, relPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing raw file %s: %w", relPath, err)
	}
	return relPath, nil
}

func (v *Vault) WriteWiki(filename, content string) (string, error) {
	relPath := filepath.Join("wiki", filename)
	absPath := filepath.Join(v.BasePath, relPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing wiki file %s: %w", relPath, err)
	}
	return relPath, nil
}

func (v *Vault) ReadIndex() (string, error) {
	data, err := os.ReadFile(filepath.Join(v.BasePath, "index.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading index.md: %w", err)
	}
	return string(data), nil
}

func (v *Vault) ReadSoul() (string, error) {
	data, err := os.ReadFile(filepath.Join(v.BasePath, "soul.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading soul.md: %w", err)
	}
	return string(data), nil
}

func (v *Vault) UpdateIndex(slug, title string, tags []string) error {
	indexPath := filepath.Join(v.BasePath, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("reading index.md: %w", err)
	}

	content := string(data)

	// Check if entry already exists by matching the index line format
	entryPrefix := "- [[" + slug + "]]"
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, entryPrefix) {
			return nil
		}
	}

	tagStr := ""
	if len(tags) > 0 {
		tagStr = " [" + strings.Join(tags, ", ") + "]"
	}

	entry := fmt.Sprintf("- [[%s]] — %s%s\n", slug, sanitizeMarkdown(title), tagStr)
	content += entry

	// Update the "Last updated" line
	content = updateLastUpdated(content)

	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing index.md: %w", err)
	}
	return nil
}

func (v *Vault) RemoveFromIndex(slug string) error {
	indexPath := filepath.Join(v.BasePath, "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("reading index.md: %w", err)
	}

	content := string(data)
	target := "[[" + slug + "]]"

	// If slug not present, no-op
	if !strings.Contains(content, target) {
		return nil
	}

	lines := strings.Split(content, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.Contains(line, target) {
			filtered = append(filtered, line)
		}
	}
	content = strings.Join(filtered, "\n")

	// Update the "Last updated" line
	content = updateLastUpdated(content)

	if err := os.WriteFile(indexPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing index.md: %w", err)
	}
	return nil
}

func updateLastUpdated(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Last updated:") {
			lines[i] = fmt.Sprintf("Last updated: %s", time.Now().Format("2006-01-02"))
			break
		}
	}
	return strings.Join(lines, "\n")
}

func GenerateSlug(title string) string {
	slug := strings.ToLower(title)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = slugRe.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")

	// Collapse multiple hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.TrimRight(slug, "-")
	}

	if slug == "" {
		slug = "untitled"
	}

	return slug
}

// sanitizeMarkdown escapes characters that could break markdown link/list syntax.
var mdReplacer = strings.NewReplacer(
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"|", "\\|",
)

func sanitizeMarkdown(s string) string {
	return mdReplacer.Replace(s)
}

func GenerateRawFilename(slug string) string {
	return slug + ".md"
}

var utmParams = map[string]bool{
	"utm_source":   true,
	"utm_medium":   true,
	"utm_campaign": true,
	"utm_term":     true,
	"utm_content":  true,
}

func NormalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	// Strip UTM params
	q := u.Query()
	for key := range q {
		if utmParams[key] {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()

	result := u.String()
	result = strings.TrimRight(result, "/")

	return result
}

func FormatRawContent(title, sourceURL, dateSaved string, tags []string, content string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %q\n", title)
	fmt.Fprintf(&b, "source: %q\n", sourceURL)
	fmt.Fprintf(&b, "date_saved: %s\n", dateSaved)
	b.WriteString("source_tags:\n")
	for _, t := range tags {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	b.WriteString("type: raw\n")
	b.WriteString("---\n\n")
	b.WriteString(content)
	return b.String()
}
