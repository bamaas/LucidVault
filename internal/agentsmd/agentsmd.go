// Package agentsmd generates and maintains AGENTS.md in the vault root.
// It combines a static template with dynamic MCP tool listings and vault statistics.
package agentsmd

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

//go:embed template.md
var staticTemplate string

// ToolInfo describes an MCP tool for AGENTS.md documentation.
type ToolInfo struct {
	Name        string
	Description string
	Parameters  []ParamInfo
}

// ParamInfo describes a tool parameter.
type ParamInfo struct {
	Name        string
	Description string
	Required    bool
}

// VaultStats holds vault statistics for dynamic AGENTS.md sections.
type VaultStats struct {
	WikiCount int
	RawCount  int
	NoteCount int
	EdgeCount int
	HasSoul   bool
	TopTags   []TagCount
}

// TagCount is a tag with its frequency.
type TagCount struct {
	Tag   string
	Count int
}

// Generate produces the full AGENTS.md content from the static template,
// dynamic MCP tool listings, and vault statistics. The web-search strategy
// controls the `## Web Search` section and the Source Attribution web bullet;
// StrategyOff omits both so AGENTS.md carries no web-search guidance (ADR-024).
func Generate(tools []ToolInfo, stats VaultStats, strategy WebSearchStrategy) string {
	var b strings.Builder

	// Static template.
	b.WriteString(staticTemplate)
	b.WriteString("\n")

	// Source Attribution (web bullet conditional on strategy).
	b.WriteString(sourceAttributionSection(strategy))
	b.WriteString("\n")

	// Web Search (mode-specific; omitted entirely when off).
	if section := webSearchSection(strategy); section != "" {
		b.WriteString(section)
		b.WriteString("\n")
	}

	// Dynamic: Available MCP Tools.
	b.WriteString("## Available MCP Tools\n\n")
	if len(tools) == 0 {
		b.WriteString("No MCP tools registered.\n")
	} else {
		for _, tool := range tools {
			fmt.Fprintf(&b, "### %s\n\n", tool.Name)
			fmt.Fprintf(&b, "%s\n\n", tool.Description)
			if len(tool.Parameters) > 0 {
				b.WriteString("**Parameters:**\n\n")
				for _, p := range tool.Parameters {
					reqStr := ""
					if p.Required {
						reqStr = " *(required)*"
					}
					fmt.Fprintf(&b, "- `%s`%s -- %s\n", p.Name, reqStr, p.Description)
				}
				b.WriteString("\n")
			}
		}
	}

	// Dynamic: Vault Statistics.
	b.WriteString("## Vault Statistics\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n")
	fmt.Fprintf(&b, "|--------|-------|\n")
	fmt.Fprintf(&b, "| Wiki pages | %d |\n", stats.WikiCount)
	fmt.Fprintf(&b, "| Raw sources | %d |\n", stats.RawCount)
	fmt.Fprintf(&b, "| Notes | %d |\n", stats.NoteCount)
	fmt.Fprintf(&b, "| Wikilink edges | %d |\n", stats.EdgeCount)

	if stats.HasSoul {
		b.WriteString("\nsoul.md is present -- read it for user personalization.\n")
	}

	if len(stats.TopTags) > 0 {
		b.WriteString("\n**Top tags:** ")
		tagStrs := make([]string, len(stats.TopTags))
		for i, tc := range stats.TopTags {
			tagStrs[i] = fmt.Sprintf("%s (%d)", tc.Tag, tc.Count)
		}
		b.WriteString(strings.Join(tagStrs, ", "))
		b.WriteString("\n")
	}

	return b.String()
}

// WriteIfChanged writes AGENTS.md to vaultPath only if content differs from
// the existing file. Returns true if the file was written, false if unchanged.
func WriteIfChanged(vaultPath, content string) (bool, error) {
	agentsPath := filepath.Join(vaultPath, "AGENTS.md")

	existing, err := os.ReadFile(agentsPath)
	if err == nil {
		// Compare hashes.
		existingHash := sha256.Sum256(existing)
		newHash := sha256.Sum256([]byte(content))
		if existingHash == newHash {
			return false, nil
		}
	}

	if err := os.WriteFile(agentsPath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("writing AGENTS.md: %w", err)
	}
	return true, nil
}

// CollectStats gathers vault statistics for AGENTS.md generation.
func CollectStats(v *vault.Vault, db *store.Store) (VaultStats, error) {
	var stats VaultStats

	// Count wiki files.
	wikiFiles, err := v.ScanWikiDir()
	if err != nil {
		return stats, fmt.Errorf("scanning wiki dir: %w", err)
	}
	stats.WikiCount = len(wikiFiles)

	// Count raw files.
	rawFiles, err := v.ScanRawDir()
	if err != nil {
		return stats, fmt.Errorf("scanning raw dir: %w", err)
	}
	stats.RawCount = len(rawFiles)

	// Count notes.
	noteFiles, err := v.ScanNotesDir()
	if err != nil {
		return stats, fmt.Errorf("scanning notes dir: %w", err)
	}
	stats.NoteCount = len(noteFiles)

	// Edge count from store.
	if db != nil {
		count, err := db.EdgeCount()
		if err != nil {
			return stats, fmt.Errorf("counting edges: %w", err)
		}
		stats.EdgeCount = count
	}

	// Check soul.md existence.
	stats.HasSoul = v.FileHasContent("soul.md")

	// Parse index.md for top tags.
	indexContent, err := v.ReadIndex()
	if err != nil {
		return stats, fmt.Errorf("reading index: %w", err)
	}

	tagFreq := make(map[string]int)
	for line := range strings.SplitSeq(indexContent, "\n") {
		entry := vault.ParseIndexEntry(line)
		if entry == nil {
			continue
		}
		for _, t := range entry.Tags {
			tagFreq[t]++
		}
	}

	// Sort by frequency desc, then alphabetically.
	var tagCounts []TagCount
	for tag, count := range tagFreq {
		tagCounts = append(tagCounts, TagCount{Tag: tag, Count: count})
	}
	sort.Slice(tagCounts, func(i, j int) bool {
		if tagCounts[i].Count != tagCounts[j].Count {
			return tagCounts[i].Count > tagCounts[j].Count
		}
		return tagCounts[i].Tag < tagCounts[j].Tag
	})

	topN := 10
	if len(tagCounts) < topN {
		topN = len(tagCounts)
	}
	stats.TopTags = tagCounts[:topN]

	return stats, nil
}
