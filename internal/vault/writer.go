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
	dirs := []string{"raw", "wiki", "notes", "templates"}
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
date: {{date}}
tags: []
---

#
`
		if err := os.WriteFile(noteTemplatePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating note template: %w", err)
		}
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

	// Check if entry already exists
	if strings.Contains(content, "[["+slug+"]]") {
		return nil
	}

	tagStr := ""
	if len(tags) > 0 {
		tagStr = " [" + strings.Join(tags, ", ") + "]"
	}

	entry := fmt.Sprintf("- [[%s]] — %s%s\n", slug, title, tagStr)
	content += entry

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

func GenerateRawFilename(dateSaved, slug string) string {
	return fmt.Sprintf("%s-%s.md", dateSaved, slug)
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
