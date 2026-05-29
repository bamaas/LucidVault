package mcpserver

import (
	"regexp"
	"strings"
)

// IndexEntry represents a parsed line from index.md.
type IndexEntry struct {
	Slug  string   `json:"slug"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
	Type  string   `json:"type"`
}

var indexEntryRe = regexp.MustCompile(`^- \[\[([^\]]+)\]\] — (.+?) \[([^\]]*)\]$`)

// ParseIndexEntry parses a line like "- [[slug]] — Title [tag1, tag2]" into an IndexEntry.
// Returns nil for non-matching lines.
func ParseIndexEntry(line string) *IndexEntry {
	matches := indexEntryRe.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	slug := matches[1]
	title := matches[2]
	tagStr := strings.TrimSpace(matches[3])

	var tags []string
	if tagStr == "" {
		tags = []string{}
	} else {
		for t := range strings.SplitSeq(tagStr, ",") {
			tags = append(tags, strings.TrimSpace(t))
		}
	}

	typ := "wiki"
	if strings.HasPrefix(slug, "notes/") {
		typ = "note"
	}

	return &IndexEntry{
		Slug:  slug,
		Title: title,
		Tags:  tags,
		Type:  typ,
	}
}

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
var inlineCodeRe = regexp.MustCompile("``[^`]+``|`[^`]+`")

// ParseWikiLinks extracts all [[link]] targets from markdown content.
// It skips links inside YAML frontmatter, fenced code blocks, and inline code.
// It handles pipe syntax ([[slug|Display]]), filters .md targets, and deduplicates.
func ParseWikiLinks(content string) []string {
	// 1. Strip YAML frontmatter.
	body := stripFrontmatter(content)

	// 2. Strip fenced code blocks (``` or ~~~).
	body = stripFencedCode(body)

	// 3. Strip inline code spans.
	body = inlineCodeRe.ReplaceAllString(body, "")

	// 4. Extract [[...]] targets.
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{})
	var links []string
	for _, m := range matches {
		target := m[1]

		// 5. Handle pipe syntax: [[slug|Display Name]] -> slug
		if before, _, found := strings.Cut(target, "|"); found {
			target = before
		}

		// 5b. Trim whitespace and skip empty/whitespace-only targets.
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		// 6. Filter .md targets (raw file back-references).
		if strings.HasSuffix(target, ".md") {
			continue
		}

		// 7. Deduplicate.
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		links = append(links, target)
	}
	return links
}

// stripFrontmatter removes YAML frontmatter (--- ... ---) from the start of content.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	// Find closing --- after the opening one.
	rest := content[3:]
	_, after, found := strings.Cut(rest, "\n---")
	if !found {
		return content
	}
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	return after
}

// stripFencedCode removes fenced code blocks (``` or ~~~) from content.
func stripFencedCode(content string) string {
	var result strings.Builder
	inFence := false
	var fenceMarker string

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
				fenceMarker = trimmed[:3]
				continue
			}
			result.WriteString(line)
			result.WriteByte('\n')
		} else {
			// Check for closing fence (must start with same marker).
			if strings.HasPrefix(trimmed, fenceMarker) && strings.TrimSpace(strings.TrimLeft(trimmed, fenceMarker[:1])) == "" {
				inFence = false
			}
		}
	}
	return result.String()
}

// ParseFrontmatterTitle extracts the title field from YAML frontmatter.
func ParseFrontmatterTitle(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	frontmatter, _, found := strings.Cut(content[3:], "---")
	if !found {
		return ""
	}

	for line := range strings.SplitSeq(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if val, ok := strings.CutPrefix(line, "title:"); ok {
			val = strings.TrimSpace(val)
			// Strip quotes
			if len(val) >= 2 {
				if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
					val = val[1 : len(val)-1]
				}
			}
			return val
		}
	}
	return ""
}
