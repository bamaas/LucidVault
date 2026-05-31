package vault

import (
	"regexp"
	"strings"
)

// IndexEntry represents a parsed line from index.md.
type IndexEntry struct {
	Slug  string
	Title string
	Tags  []string
}

var indexLineRe = regexp.MustCompile(`^- \[\[([^\]]+)\]\] — (.+?) \[([^\]]*)\]$`)

// ParseIndexEntry parses a line like "- [[slug]] — Title [tag1, tag2]" into an IndexEntry.
// Returns nil for non-matching lines.
func ParseIndexEntry(line string) *IndexEntry {
	matches := indexLineRe.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}
	slug := matches[1]
	title := matches[2]
	tagStr := strings.TrimSpace(matches[3])

	var tags []string
	if tagStr != "" {
		for t := range strings.SplitSeq(tagStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	return &IndexEntry{Slug: slug, Title: title, Tags: tags}
}
