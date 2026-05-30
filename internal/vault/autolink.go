package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// BacklinkCandidate represents a page that shares tags with a newly enriched page.
type BacklinkCandidate struct {
	Slug       string
	SharedTags []string
}

// BacklinkLine formats the backlink entry to be inserted into the candidate's ## Related section.
// Format: "[[newSlug]] — shared tags: tag1, tag2"
func (c BacklinkCandidate) BacklinkLine(newSlug string, newTags []string) string {
	// Compute shared tags between candidate and newTags
	shared := c.SharedTags
	if len(shared) == 0 {
		shared = intersectTags(c.SharedTags, newTags)
	}
	return fmt.Sprintf("[[%s]] — shared tags: %s", newSlug, strings.Join(shared, ", "))
}

// UpdateRelatedSection appends backlinks to a wiki file's ## Related section.
//   - If ## Related exists: append new links (skip duplicates).
//   - If no ## Related but LucidVault footer exists: insert ## Related before footer.
//   - If neither: append ## Related at end of file.
//
// Footer detection: a line that is exactly "---" followed by a line starting with "*Source:".
func (v *Vault) UpdateRelatedSection(relPath string, newLinks []string) error {
	absPath := filepath.Join(v.BasePath, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("reading file %s: %w", relPath, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Find ## Related section index
	relatedIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Related" {
			relatedIdx = i
			break
		}
	}

	// Find footer index (--- followed by *Source:)
	footerIdx := -1
	for i := 0; i < len(lines)-1; i++ {
		if strings.TrimSpace(lines[i]) == "---" && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "*Source:") {
			footerIdx = i
			break
		}
	}

	if relatedIdx >= 0 {
		// Existing ## Related section — find the end of the section and collect existing links
		existingLinks := collectExistingLinks(lines, relatedIdx)
		var toAdd []string
		for _, link := range newLinks {
			slug := extractSlugFromLink(link)
			if slug != "" && existingLinks[slug] {
				continue
			}
			toAdd = append(toAdd, "- "+link)
		}
		if len(toAdd) == 0 {
			return nil
		}

		// Find insertion point: after last non-empty line in the Related section
		insertIdx := findRelatedSectionEnd(lines, relatedIdx)
		// Insert the new links
		newLines := make([]string, 0, len(lines)+len(toAdd))
		newLines = append(newLines, lines[:insertIdx]...)
		newLines = append(newLines, toAdd...)
		newLines = append(newLines, lines[insertIdx:]...)
		lines = newLines
	} else if footerIdx >= 0 {
		// No ## Related but footer exists — insert before footer
		section := []string{"## Related", ""}
		for _, link := range newLinks {
			section = append(section, "- "+link)
		}
		section = append(section, "")

		newLines := make([]string, 0, len(lines)+len(section))
		newLines = append(newLines, lines[:footerIdx]...)
		newLines = append(newLines, section...)
		newLines = append(newLines, lines[footerIdx:]...)
		lines = newLines
	} else {
		// No section, no footer — append at end
		// Ensure there's a blank line before the section
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "## Related", "")
		for _, link := range newLinks {
			lines = append(lines, "- "+link)
		}
		lines = append(lines, "")
	}

	result := strings.Join(lines, "\n")
	if err := os.WriteFile(absPath, []byte(result), 0o644); err != nil {
		return fmt.Errorf("writing file %s: %w", relPath, err)
	}
	return nil
}

// FindRelatedByTags reads index.md and finds pages sharing 2+ tags with newTags.
// Excludes newSlug from results. Returns at most 3 candidates sorted by:
// tag overlap DESC → file mtime DESC → slug ASC.
func (v *Vault) FindRelatedByTags(newSlug string, newTags []string) ([]BacklinkCandidate, error) {
	indexContent, err := v.ReadIndex()
	if err != nil {
		return nil, fmt.Errorf("reading index: %w", err)
	}

	if len(newTags) < 2 {
		return nil, nil
	}

	newTagSet := make(map[string]bool, len(newTags))
	for _, t := range newTags {
		newTagSet[t] = true
	}

	type candidate struct {
		slug       string
		sharedTags []string
		overlap    int
		mtime      int64 // unix nanos, 0 if unavailable
	}

	var candidates []candidate

	for _, line := range strings.Split(indexContent, "\n") {
		entry := parseIndexEntry(line)
		if entry == nil {
			continue
		}
		if entry.Slug == newSlug {
			continue
		}

		// Compute tag overlap
		var shared []string
		for _, t := range entry.Tags {
			if newTagSet[t] {
				shared = append(shared, t)
			}
		}
		if len(shared) < 2 {
			continue
		}

		// Get mtime
		wikiPath := filepath.Join(v.BasePath, "wiki", entry.Slug+".md")
		var mtime int64
		if info, err := os.Stat(wikiPath); err == nil {
			mtime = info.ModTime().UnixNano()
		}

		candidates = append(candidates, candidate{
			slug:       entry.Slug,
			sharedTags: shared,
			overlap:    len(shared),
			mtime:      mtime,
		})
	}

	// Sort: overlap DESC, mtime DESC, slug ASC
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].overlap != candidates[j].overlap {
			return candidates[i].overlap > candidates[j].overlap
		}
		if candidates[i].mtime != candidates[j].mtime {
			return candidates[i].mtime > candidates[j].mtime
		}
		return candidates[i].slug < candidates[j].slug
	})

	// Cap at 3
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	result := make([]BacklinkCandidate, len(candidates))
	for i, c := range candidates {
		result[i] = BacklinkCandidate{
			Slug:       c.slug,
			SharedTags: c.sharedTags,
		}
	}
	return result, nil
}

// collectExistingLinks gathers slugs from [[slug]] links in the ## Related section.
func collectExistingLinks(lines []string, relatedIdx int) map[string]bool {
	existing := make(map[string]bool)
	for i := relatedIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Stop at the next heading
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			break
		}
		slug := extractSlugFromLink(line)
		if slug != "" {
			existing[slug] = true
		}
	}
	return existing
}

// findRelatedSectionEnd returns the index after the last link line in the ## Related section.
func findRelatedSectionEnd(lines []string, relatedIdx int) int {
	lastContent := relatedIdx
	for i := relatedIdx + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			break
		}
		if strings.HasPrefix(line, "---") {
			break
		}
		if line != "" {
			lastContent = i
		}
	}
	return lastContent + 1
}

// extractSlugFromLink extracts the slug from a line like "- [[slug]] — shared tags: ..."
func extractSlugFromLink(line string) string {
	start := strings.Index(line, "[[")
	end := strings.Index(line, "]]")
	if start >= 0 && end > start+2 {
		return line[start+2 : end]
	}
	return ""
}

// indexEntry is a local representation of a parsed index.md line.
// Avoids importing mcpserver (which would create an import cycle).
type indexEntry struct {
	Slug string
	Tags []string
}

var indexEntryRe = regexp.MustCompile(`^- \[\[([^\]]+)\]\] — (.+?) \[([^\]]*)\]$`)

// parseIndexEntry parses a line like "- [[slug]] — Title [tag1, tag2]".
func parseIndexEntry(line string) *indexEntry {
	matches := indexEntryRe.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}
	slug := matches[1]
	tagStr := strings.TrimSpace(matches[3])
	var tags []string
	if tagStr != "" {
		for _, t := range strings.Split(tagStr, ",") {
			tags = append(tags, strings.TrimSpace(t))
		}
	}
	return &indexEntry{Slug: slug, Tags: tags}
}

// intersectTags returns tags present in both a and b.
func intersectTags(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, t := range b {
		set[t] = true
	}
	var result []string
	for _, t := range a {
		if set[t] {
			result = append(result, t)
		}
	}
	return result
}
