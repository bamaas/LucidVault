// Package claudemd upserts a minimal LucidVault pointer section into a
// user-editable CLAUDE.md file, guarded so a user's own edits inside the
// marker block are never silently overwritten (ADR-027).
package claudemd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	StartMarker = "<!-- lucidvault:start -->"
	EndMarker   = "<!-- lucidvault:end -->"
)

const (
	checksumPrefix = "<!-- lucidvault:checksum:"
	checksumSuffix = " -->"
)

// bodyFmt is the current pointer body (ADR-025/028): the vault path, a
// pointer at AGENTS.md, and one sentence covering the vault-absent case that
// names only the always-on MCP tools (search_wiki, related_notes,
// expand_graph) -- never the MCP_READ_TOOLS-gated content-read tools.
const bodyFmt = "## LucidVault Knowledge Base\n\n" +
	"You have a personal knowledge base at %s. It contains `AGENTS.md` - read that file and follow it. `AGENTS.md` is the single source of truth for the vault layout, retrieval strategy, and citation rules.\n\n" +
	"If that path does not exist or is not mounted, tell the user the vault is unavailable instead of guessing at its contents. When the `lucidvault` MCP server is configured, `search_wiki`, `related_notes`, and `expand_graph` can still help with discovery."

// legacyBodyFmt is the pre-checksum ADR-025 pointer body, verbatim. ADR-027
// requires a legacy (checksum-less) block to be recognized as generator-owned
// only when its body is an exact match for this shape with any vault path,
// and treated as diverged otherwise.
const legacyBodyFmt = "## LucidVault Knowledge Base\n\n" +
	"You have a personal knowledge base at %s. It contains `AGENTS.md` — read that file and follow it. `AGENTS.md` is the single source of truth for the vault layout, retrieval strategy, and citation rules."

// blockRe matches the first complete marker pair, capturing the content
// between them. It requires both markers, so an unterminated start marker
// (no matching end marker) never matches -- the block is appended instead of
// swallowing the rest of the file.
var blockRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(StartMarker) + `(.*?)` + regexp.QuoteMeta(EndMarker))

var checksumRe = regexp.MustCompile(regexp.QuoteMeta(checksumPrefix) + `([0-9a-f]{64})` + regexp.QuoteMeta(checksumSuffix))

var legacyBodyRe = buildLegacyBodyRe()

func buildLegacyBodyRe() *regexp.Regexp {
	parts := strings.SplitN(legacyBodyFmt, "%s", 2)
	// The vault path sits inside a single line and is captured, not matched
	// literally, so it survives regex metacharacters and '%' in the path
	// unescaped; only the fixed surrounding text needs QuoteMeta.
	return regexp.MustCompile(`^` + regexp.QuoteMeta(parts[0]) + `[^\n]*` + regexp.QuoteMeta(parts[1]) + `$`)
}

// Status reports what Upsert did to the target file.
type Status int

const (
	// StatusUnknown is the zero value, returned whenever Upsert fails.
	StatusUnknown Status = iota
	// StatusWrote means Upsert wrote (inserted or replaced) the section.
	StatusWrote
	// StatusSkippedDiverged means an existing block diverged from generator
	// output and was left untouched.
	StatusSkippedDiverged
)

// Upsert inserts or replaces the LucidVault section in a CLAUDE.md file.
//
// A block the generator owns (checksum matches, or a legacy checksum-less
// block whose body is an exact match for the pre-checksum template shape) is
// rewritten with the current template and vaultPath. Any other existing
// block is treated as user-edited and left byte-identical (ADR-027).
func Upsert(claudeMDPath, vaultPath string) (Status, error) {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return StatusUnknown, fmt.Errorf("reading %s: %w", claudeMDPath, err)
	}
	content := string(data)
	newSection := renderSection(vaultPath)

	var newContent string
	if loc := blockRe.FindStringSubmatchIndex(content); loc != nil {
		inner := content[loc[2]:loc[3]]
		if !isGeneratorOwned(inner) {
			return StatusSkippedDiverged, nil
		}
		newContent = content[:loc[0]] + newSection + content[loc[1]:]
	} else {
		newContent = appendSection(content, newSection)
	}

	if err := os.WriteFile(claudeMDPath, []byte(newContent), 0o644); err != nil {
		return StatusUnknown, fmt.Errorf("writing %s: %w", claudeMDPath, err)
	}
	return StatusWrote, nil
}

// isGeneratorOwned reports whether inner (the text between the markers) was
// written by this package and is therefore safe to overwrite (ADR-027).
func isGeneratorOwned(inner string) bool {
	if loc := checksumRe.FindStringSubmatchIndex(inner); loc != nil {
		embedded := inner[loc[2]:loc[3]]
		body := strings.TrimSpace(inner[:loc[0]] + inner[loc[1]:])
		return checksumHex(body) == embedded
	}
	return legacyBodyRe.MatchString(strings.TrimSpace(inner))
}

func renderSection(vaultPath string) string {
	body := fmt.Sprintf(bodyFmt, vaultPath)
	comment := checksumPrefix + checksumHex(body) + checksumSuffix
	return StartMarker + "\n" + body + "\n\n" + comment + "\n" + EndMarker
}

func checksumHex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func appendSection(content, section string) string {
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if len(content) > 0 {
		content += "\n"
	}
	return content + section + "\n"
}
