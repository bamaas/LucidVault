package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// NoteFile represents a scanned markdown file from the notes/ directory.
type NoteFile struct {
	Path        string   // relative path from vault root, e.g. "notes/my-note.md"
	ContentHash string   // hex-encoded SHA-256 of file content
	Tags        []string // from YAML frontmatter
	Title       string   // derived from filename
}

// Scan recursively walks the notes/ subdirectory under vaultPath and returns
// a NoteFile for each non-empty .md file found.
func Scan(vaultPath string) ([]NoteFile, error) {
	notesDir := filepath.Join(vaultPath, "notes")

	if _, err := os.Stat(notesDir); os.IsNotExist(err) {
		return nil, nil
	}

	var files []NoteFile

	err := filepath.WalkDir(notesDir, func(absPath string, d os.DirEntry, err error) error {
		if err != nil {
			// Fail if the notes directory itself is unreadable; skip subdirectories.
			if absPath == notesDir {
				return err
			}
			return nil
		}
		if d.IsDir() {
			// Skip symlinked directories to avoid reading files outside the vault
			if d.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinked files to avoid reading files outside the vault
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			// Skip unreadable files instead of aborting the entire scan
			return nil
		}

		// Skip empty or whitespace-only files.
		if strings.TrimSpace(string(data)) == "" {
			return nil
		}

		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])

		relPath, err := filepath.Rel(vaultPath, absPath)
		if err != nil {
			return err
		}

		title := ParseH1(string(data))
		if title == "" {
			title = TitleFromFilename(relPath)
		}

		files = append(files, NoteFile{
			Path:        relPath,
			ContentHash: hash,
			Tags:        ParseFrontmatter(string(data)),
			Title:       title,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

// ParseFrontmatter extracts tags from YAML frontmatter delimited by ---.
// Handles both list style (tags:\n  - foo) and inline array (tags: [foo, bar]).
// Returns nil if no frontmatter or no tags are found.
func ParseFrontmatter(content string) []string {
	// Normalize Windows line endings before parsing.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Frontmatter must start at the very beginning of the file.
	if !strings.HasPrefix(content, "---") {
		return nil
	}

	// Find the closing delimiter, skipping past the opening ---.
	body := content[3:]
	end := strings.Index(body, "\n---")
	if end == -1 {
		return nil
	}
	frontmatter := body[:end]

	// Search for a top-level "tags:" key in the frontmatter.
	// Only match unindented lines to avoid matching nested YAML keys.
	lines := strings.Split(frontmatter, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			// Line has leading whitespace — it's a nested key, skip it
			continue
		}
		if trimmed != "tags:" && !strings.HasPrefix(trimmed, "tags: ") {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "tags:"))

		// Inline array: tags: [foo, bar]
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			inner := value[1 : len(value)-1]
			return splitTrimmed(inner, ",")
		}

		// List style: collect following lines that start with "  -"
		var tags []string
		for j := i + 1; j < len(lines); j++ {
			item := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(item, "-") {
				break
			}
			tag := strings.TrimSpace(strings.TrimPrefix(item, "-"))
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		if len(tags) > 0 {
			return tags
		}
		return nil
	}

	return nil
}

// ParseTitle extracts the title from YAML frontmatter.
// Returns empty string if no frontmatter or no title found.
func ParseTitle(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, "---") {
		return ""
	}

	body := content[3:]
	end := strings.Index(body, "\n---")
	if end == -1 {
		return ""
	}
	frontmatter := body[:end]

	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != line && trimmed != "" {
			continue
		}
		if trimmed != "title:" && !strings.HasPrefix(trimmed, "title: ") {
			continue
		}
		title := strings.TrimPrefix(trimmed, "title:")
		title = strings.TrimSpace(title)
		title = strings.Trim(title, `"'`)
		return title
	}
	return ""
}

// ParseH1 extracts the first top-level heading (# ...) from markdown content.
// Returns empty string if no H1 is found.
func ParseH1(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

// TitleFromFilename returns the base filename without extension from a relative path.
// e.g. "notes/sub/my-note.md" → "my-note"
func TitleFromFilename(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// StripFrontmatter removes YAML frontmatter (--- delimited) from content,
// returning only the body. If no frontmatter is present, returns content as-is.
func StripFrontmatter(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, "---") {
		return content
	}

	body := content[3:]
	end := strings.Index(body, "\n---")
	if end == -1 {
		return content
	}

	// Skip past the closing "---\n"
	rest := body[end+4:]
	return strings.TrimSpace(rest)
}

// splitTrimmed splits s by sep and trims whitespace from each element,
// omitting empty strings.
func splitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Strip surrounding quotes (YAML allows "foo" or 'foo')
		if len(p) >= 2 && ((p[0] == '"' && p[len(p)-1] == '"') || (p[0] == '\'' && p[len(p)-1] == '\'')) {
			p = p[1 : len(p)-1]
		}
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
