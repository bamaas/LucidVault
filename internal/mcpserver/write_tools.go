package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// DeleteResult holds the outcome of a page deletion.
type DeleteResult struct {
	Slug         string   `json:"slug"`
	DanglingRefs []string `json:"dangling_refs"`
}

// HandleUpdateWiki replaces the content of a ## section in a wiki page.
// It preserves frontmatter, other sections, and handles code blocks containing ##.
// If the section is "Related", it syncs edges via UpsertEdgesFrom.
// File mutations are wrapped in WithFileLock (D4).
func HandleUpdateWiki(v *vault.Vault, db *store.Store, slug, section, content string) error {
	// Read, parse, and write inside the file lock.
	err := db.WithFileLock(func() error {
		relPath := "wiki/" + slug + ".md"
		existing, err := v.ReadFile(relPath)
		if err != nil {
			return fmt.Errorf("reading wiki page %q: %w", slug, err)
		}

		// Split into frontmatter and body.
		frontmatter, body := splitFrontmatter(existing)

		// Update last_updated in frontmatter.
		frontmatter = upsertFrontmatterField(frontmatter, "last_updated", time.Now().Format("2006-01-02"))

		// Parse sections from body, respecting code blocks.
		sections := parseSections(body)

		// Find and replace the target section.
		found := false
		for i, s := range sections {
			if s.heading == section {
				sections[i].content = content + "\n"
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("section %q not found in wiki page %q", section, slug)
		}

		// Rebuild the page.
		var b strings.Builder
		b.WriteString(frontmatter)
		for _, s := range sections {
			b.WriteString(s.raw())
		}

		newContent := b.String()
		if _, err := v.WriteWiki(slug+".md", newContent); err != nil {
			return fmt.Errorf("writing wiki page %q: %w", slug, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Sync edges outside the file lock (DB operations use their own connection).
	if section == "Related" {
		links := ParseWikiLinks("## Related\n" + content)
		var edges []store.Edge
		for _, link := range links {
			edges = append(edges, store.Edge{
				FromSlug: slug,
				ToSlug:   link,
				Type:     "wikilink",
			})
		}
		if err := db.UpsertEdgesFrom(slug, "wikilink", edges); err != nil {
			return fmt.Errorf("syncing edges for %q: %w", slug, err)
		}
	}

	return nil
}

// HandleDeletePage deletes a vault page and all its artifacts.
// It collects inbound edges before deletion (D12), then removes:
// wiki file, raw file (if exists), index entry, all edges, and DB record.
// Returns a DeleteResult with dangling references.
// File mutations are wrapped in WithFileLock (D4).
func HandleDeletePage(v *vault.Vault, db *store.Store, slug string) (*DeleteResult, error) {
	var danglingRefs []string

	// All checks and mutations inside the lock to avoid TOCTOU races.
	err := db.WithFileLock(func() error {
		// D12: Collect inbound edges BEFORE any deletion.
		if !v.FileHasContent("wiki/" + slug + ".md") {
			return fmt.Errorf("wiki page %q not found", slug)
		}

		inbound, err := db.GetInboundEdges(slug)
		if err != nil {
			return fmt.Errorf("collecting inbound edges for %q: %w", slug, err)
		}

		danglingRefs = make([]string, 0, len(inbound))
		for _, e := range inbound {
			danglingRefs = append(danglingRefs, e.FromSlug)
		}
		// Delete wiki file.
		if err := v.DeleteFile("wiki/" + slug + ".md"); err != nil {
			return fmt.Errorf("deleting wiki file %q: %w", slug, err)
		}

		// Delete raw file (if exists, no error if missing).
		_ = v.DeleteFile("raw/" + slug + ".md")

		// Remove from index.
		if err := v.RemoveFromIndex(slug); err != nil {
			return fmt.Errorf("removing %q from index: %w", slug, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// DB operations outside the lock.
	if err := db.DeleteEdgesInvolving(slug); err != nil {
		return nil, fmt.Errorf("deleting edges for %q: %w", slug, err)
	}

	// Clean up DB record (D7).
	if strings.HasPrefix(slug, "notes/") {
		notePath := slug + ".md"
		if err := db.DeleteNote(notePath); err != nil {
			return nil, fmt.Errorf("deleting note record %q: %w", notePath, err)
		}
	} else {
		wikiPath := "wiki/" + slug + ".md"
		if err := db.DeleteBookmarkByWikiPath(wikiPath); err != nil {
			return nil, fmt.Errorf("deleting bookmark record for %q: %w", wikiPath, err)
		}
	}

	return &DeleteResult{
		Slug:         slug,
		DanglingRefs: danglingRefs,
	}, nil
}

// sectionBlock represents a parsed section of a markdown document.
type sectionBlock struct {
	heading string // heading text (without "## "), empty for preamble
	prefix  string // the "## Heading\n" line, empty for preamble
	content string // content after the heading line
}

func (s sectionBlock) raw() string {
	return s.prefix + s.content
}

// parseSections splits a markdown body (no frontmatter) into sections by ## headings.
// It respects fenced code blocks (``` or ~~~) — headings inside code blocks are ignored.
func parseSections(body string) []sectionBlock {
	lines := strings.Split(body, "\n")
	var sections []sectionBlock
	var current sectionBlock
	var contentLines []string
	inFence := false
	var fenceMarker string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fenced code blocks.
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
				fenceMarker = trimmed[:3]
				contentLines = append(contentLines, line)
				continue
			}
		} else {
			if strings.HasPrefix(trimmed, fenceMarker) && strings.TrimSpace(strings.TrimLeft(trimmed, fenceMarker[:1])) == "" {
				inFence = false
			}
			contentLines = append(contentLines, line)
			continue
		}

		// Check for ## heading (not inside code block).
		if strings.HasPrefix(line, "## ") {
			// Save current section. Add trailing "\n" because Split consumed
			// the line separator between the last content line and this heading.
			current.content = strings.Join(contentLines, "\n") + "\n"
			sections = append(sections, current)

			// Start new section.
			heading := strings.TrimPrefix(line, "## ")
			heading = strings.TrimSpace(heading)
			current = sectionBlock{
				heading: heading,
				prefix:  line + "\n",
			}
			contentLines = nil
			continue
		}

		contentLines = append(contentLines, line)
	}

	// Save final section.
	current.content = strings.Join(contentLines, "\n")
	sections = append(sections, current)

	return sections
}

// splitFrontmatter separates YAML frontmatter from the body.
// Returns the frontmatter (including --- delimiters and trailing newline) and the body.
func splitFrontmatter(content string) (string, string) {
	if !strings.HasPrefix(content, "---\n") && content != "---" {
		return "", content
	}

	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}

	// Include closing --- and newline.
	end := 3 + idx + 4 // "---" + rest[:idx] + "\n---"
	frontmatter := content[:end]
	body := content[end:]
	if strings.HasPrefix(body, "\n") {
		frontmatter += "\n"
		body = body[1:]
	}

	return frontmatter, body
}

// upsertFrontmatterField adds or updates a field in YAML frontmatter.
func upsertFrontmatterField(frontmatter, key, value string) string {
	entry := key + ": " + value
	lines := strings.Split(frontmatter, "\n")

	// Find existing field.
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+":") {
			lines[i] = entry
			return strings.Join(lines, "\n")
		}
	}

	// Insert before closing ---.
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "---" {
			lines = append(lines[:i+1], lines[i:]...)
			lines[i] = entry
			return strings.Join(lines, "\n")
		}
	}

	return frontmatter
}
