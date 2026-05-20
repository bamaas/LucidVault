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
		for _, t := range strings.Split(tagStr, ",") {
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

// ParseWikiLinks extracts all [[link]] targets from markdown content.
func ParseWikiLinks(content string) []string {
	matches := wikiLinkRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []string{}
	}
	var links []string
	for _, m := range matches {
		links = append(links, m[1])
	}
	return links
}

// ParseFrontmatterTitle extracts the title field from YAML frontmatter.
func ParseFrontmatterTitle(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return ""
	}
	frontmatter := content[3 : end+3]

	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "title:"))
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
