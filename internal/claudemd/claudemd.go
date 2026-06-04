package claudemd

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	StartMarker = "<!-- lucidvault:start -->"
	EndMarker   = "<!-- lucidvault:end -->"
)

var markerRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(StartMarker) + `.*?` + regexp.QuoteMeta(EndMarker))

const sectionTemplate = `<!-- lucidvault:start -->
## LucidVault Knowledge Base

You have a personal knowledge base at %s. It contains ` + "`AGENTS.md`" + ` — read that file and follow it. ` + "`AGENTS.md`" + ` is the single source of truth for the vault layout, retrieval strategy, and citation rules.
<!-- lucidvault:end -->`

// Upsert inserts or replaces the LucidVault section in a CLAUDE.md file.
func Upsert(claudeMDPath, vaultPath string) error {
	section := fmt.Sprintf(sectionTemplate, vaultPath)

	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", claudeMDPath, err)
	}

	content := string(data)

	if markerRe.MatchString(content) {
		content = markerRe.ReplaceAllString(content, section)
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if len(content) > 0 {
			content += "\n"
		}
		content += section + "\n"
	}

	if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", claudeMDPath, err)
	}

	return nil
}
