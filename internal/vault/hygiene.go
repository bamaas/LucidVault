package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanRawDir returns a list of relative paths (e.g. "raw/foo.md")
// for all .md files under the raw/ directory. Returns empty slice if raw/ doesn't exist.
func (v *Vault) ScanRawDir() ([]string, error) {
	rawDir := filepath.Join(v.BasePath, "raw")
	if _, err := os.Stat(rawDir); os.IsNotExist(err) {
		return nil, nil
	}

	var paths []string
	err := filepath.Walk(rawDir, func(path string, info os.FileInfo, err error) error {
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
		return nil, fmt.Errorf("scanning raw directory: %w", err)
	}
	return paths, nil
}

// ParseFrontmatterURL extracts the url (or source as fallback) from YAML frontmatter.
func ParseFrontmatterURL(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}

	var urlVal, sourceVal string
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if strings.HasPrefix(trimmed, "url:") {
			urlVal = extractFrontmatterValue(trimmed[4:])
		} else if strings.HasPrefix(trimmed, "source:") {
			sourceVal = extractFrontmatterValue(trimmed[7:])
		}
	}

	if urlVal != "" {
		return urlVal
	}
	return sourceVal
}

// extractFrontmatterValue strips quotes and whitespace from a YAML value.
func extractFrontmatterValue(raw string) string {
	v := strings.TrimSpace(raw)
	// Remove surrounding quotes
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	return v
}

// HasRawFooterLink checks if content has a footer link pointing to the given raw file path.
func HasRawFooterLink(content, rawPath string) bool {
	return strings.Contains(content, "("+rawPath+")")
}

// RewriteFooterLink replaces a raw file link in the footer with a URL.
func (v *Vault) RewriteFooterLink(relPath, oldRawPath, newURL string) error {
	absPath := filepath.Join(v.BasePath, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", relPath, err)
	}

	content := string(data)
	oldLink := "(" + oldRawPath + ")"
	if !strings.Contains(content, oldLink) {
		return nil // no-op
	}

	newLink := "(" + newURL + ")"
	content = strings.Replace(content, oldLink, newLink, 1)

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing file %s: %w", relPath, err)
	}
	return nil
}
