package agentsmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

func TestGenerate_WithToolsAndStats(t *testing.T) {
	tools := []ToolInfo{
		{
			Name:        "read_wiki",
			Description: "Read a wiki page",
			Parameters: []ParamInfo{
				{Name: "slug", Description: "Wiki page slug", Required: true},
			},
		},
		{
			Name:        "search_index",
			Description: "Search the index",
			Parameters: []ParamInfo{
				{Name: "query", Description: "Search keywords", Required: true},
			},
		},
	}

	stats := VaultStats{
		WikiCount: 42,
		RawCount:  38,
		NoteCount: 15,
		EdgeCount: 120,
		HasSoul:   true,
		TopTags: []TagCount{
			{Tag: "golang", Count: 10},
			{Tag: "kubernetes", Count: 8},
		},
	}

	result := Generate(tools, stats, StrategyFallback)

	// Must contain static template content.
	if !strings.Contains(result, "Vault Access Rules") {
		t.Error("missing 'Vault Access Rules' section from static template")
	}
	if !strings.Contains(result, "Retrieval Strategy") {
		t.Error("missing 'Retrieval Strategy' section from static template")
	}
	if !strings.Contains(result, "Content Guidelines") {
		t.Error("missing 'Content Guidelines' section from static template")
	}

	// Must contain dynamic MCP tools section.
	if !strings.Contains(result, "## Available MCP Tools") {
		t.Error("missing '## Available MCP Tools' section")
	}
	if !strings.Contains(result, "read_wiki") {
		t.Error("missing tool 'read_wiki' in output")
	}
	if !strings.Contains(result, "search_index") {
		t.Error("missing tool 'search_index' in output")
	}
	if !strings.Contains(result, "slug") {
		t.Error("missing parameter 'slug' in output")
	}
	if !strings.Contains(result, "*(required)*") {
		t.Error("missing *(required)* markup for required parameters")
	}

	// Must contain vault stats section.
	if !strings.Contains(result, "## Vault Statistics") {
		t.Error("missing '## Vault Statistics' section")
	}
	if !strings.Contains(result, "42") {
		t.Error("missing wiki count '42' in stats")
	}
	if !strings.Contains(result, "golang") {
		t.Error("missing top tag 'golang' in stats")
	}
	if !strings.Contains(result, "soul.md") {
		t.Error("missing soul.md reference in stats")
	}
}

// sectionBody returns the markdown body of the section introduced by the given
// "## Header", spanning from that header up to (but not including) the next
// level-2 heading. It returns "" if the header is absent. Scoping assertions to
// a section's own body prevents a phrase that also appears elsewhere in
// AGENTS.md (e.g. "Vault" in "Vault Access Rules") from masking a regression
// in the section under test.
func sectionBody(doc, header string) string {
	start := strings.Index(doc, header)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(header):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		return rest[:next]
	}
	return rest
}

func TestGenerate_RetrievalInstructionSections(t *testing.T) {
	// These static sections must render regardless of dynamic content,
	// so assert with empty tools and zero-value stats. Use the default
	// fallback strategy so the Web Search section is emitted.
	result := Generate(nil, VaultStats{}, StrategyFallback)

	// Section 1 -- Query Expansion. Assertions are scoped to the section body
	// so that phrases shared with other sections (soul.md, [[wikilinks]]) still
	// catch a regression inside Query Expansion specifically.
	qe := sectionBody(result, "## Query Expansion")
	if qe == "" {
		t.Error("missing '## Query Expansion' section")
	}
	// Synonym / abbreviation guidance (e.g. k8s -> kubernetes).
	if !strings.Contains(qe, "k8s") || !strings.Contains(qe, "kubernetes") {
		t.Error("missing synonym/abbreviation guidance (k8s -> kubernetes) in Query Expansion")
	}
	// Personalization via soul.md.
	if !strings.Contains(qe, "soul.md") {
		t.Error("missing soul.md personalization guidance in Query Expansion")
	}
	// Lateral terms via tags and wikilinks.
	if !strings.Contains(qe, "[[wikilinks]]") {
		t.Error("missing lateral-term ([[wikilinks]]) guidance in Query Expansion")
	}

	// Section 2 -- Source Attribution.
	sa := sectionBody(result, "## Source Attribution")
	if sa == "" {
		t.Error("missing '## Source Attribution' section")
	}
	// Must cover vault, model knowledge, and web origins -- scoped to the
	// section so the bare word "Vault" elsewhere cannot mask a deleted bullet.
	if !strings.Contains(sa, "Vault") {
		t.Error("missing vault origin guidance in Source Attribution")
	}
	if !strings.Contains(sa, "Model knowledge") {
		t.Error("missing model-knowledge origin guidance in Source Attribution")
	}
	if !strings.Contains(sa, "Web search") {
		t.Error("missing web-search origin guidance in Source Attribution")
	}
	// Sources must be returned as clickable hyperlinks so the owner can open the
	// full website later -- not just bare slugs or plain-text URLs.
	if !strings.Contains(sa, "hyperlink") {
		t.Error("missing clickable-hyperlink guidance in Source Attribution")
	}
	// The markdown link form should be shown so the agent knows the expected shape.
	if !strings.Contains(sa, "[title](url)") {
		t.Error("missing markdown link-form example in Source Attribution")
	}
	// When the vault has nothing on the topic, the agent must say so explicitly
	// rather than silently answering from elsewhere.
	if !strings.Contains(sa, "No vault match") {
		t.Error("missing empty-vault disclosure guidance in Source Attribution")
	}
	// A vault answer MUST include the page's original source URL (from `source:`
	// frontmatter), not just the wiki slug/path -- the owner needs the link to the
	// real website. The guidance must make that mandatory, not optional.
	if !strings.Contains(sa, "required") {
		t.Error("Source Attribution must make the original source URL required for vault answers, not an optional add-on to the wiki path")
	}

	// Section 3 -- Web Search.
	ws := sectionBody(result, "## Web Search")
	if ws == "" {
		t.Error("missing '## Web Search' section")
	}
	// Vault-first guidance, scoped to the section body.
	if !strings.Contains(ws, "vault") {
		t.Error("missing vault-first guidance in Web Search")
	}
	// Cite URLs guidance.
	if !strings.Contains(ws, "URL") {
		t.Error("missing 'cite URLs' guidance in Web Search")
	}
}

// TestGenerate_RetrievalStrategyEmphasizesAgentJudgment verifies the Retrieval
// Strategy section frames retrieval as agent-driven reasoning -- direct file
// access first, MCP read tools as optional accelerators -- rather than a rigid
// mandatory tool-calling sequence.
func TestGenerate_RetrievalStrategyEmphasizesAgentJudgment(t *testing.T) {
	result := Generate(nil, VaultStats{}, StrategyFallback)

	rs := sectionBody(result, "## Retrieval Strategy")
	if rs == "" {
		t.Fatal("missing '## Retrieval Strategy' section")
	}
	// The agent should be told it is not a fixed pipeline and should adapt.
	if !strings.Contains(rs, "not a fixed search pipeline") {
		t.Error("missing 'agent, not a fixed pipeline' framing in Retrieval Strategy")
	}
	// Direct file access should be framed as the primary path.
	if !strings.Contains(rs, "Direct file access is your primary tool") {
		t.Error("missing direct-file-access-first framing in Retrieval Strategy")
	}
	// MCP read tools should be framed as optional, not mandatory.
	if !strings.Contains(rs, "optional accelerators") {
		t.Error("missing 'MCP tools are optional accelerators' framing in Retrieval Strategy")
	}
	// The write-through-MCP invariant must remain a hard rule even as reads loosen.
	if !strings.Contains(rs, "never edit\nfiles directly") && !strings.Contains(rs, "never edit files directly") {
		t.Error("missing write-through-MCP hard-rule reminder in Retrieval Strategy")
	}
}

func TestGenerate_EmptyTools(t *testing.T) {
	stats := VaultStats{
		WikiCount: 5,
		RawCount:  3,
		NoteCount: 1,
	}

	result := Generate(nil, stats, StrategyFallback)

	// Static template must still render.
	if !strings.Contains(result, "Vault Access Rules") {
		t.Error("missing static template with empty tools")
	}

	// Tools section should indicate no tools registered.
	if !strings.Contains(result, "## Available MCP Tools") {
		t.Error("missing MCP Tools section even with no tools")
	}
	if !strings.Contains(result, "No MCP tools registered.") {
		t.Error("missing 'No MCP tools registered.' fallback message")
	}

	// Stats should still render with correct counts.
	if !strings.Contains(result, "## Vault Statistics") {
		t.Error("missing Vault Statistics section")
	}
	if !strings.Contains(result, "| Wiki pages | 5 |") {
		t.Error("missing correct wiki count in stats")
	}
	if !strings.Contains(result, "| Raw sources | 3 |") {
		t.Error("missing correct raw count in stats")
	}
}

func TestGenerate_EmptyStats(t *testing.T) {
	tools := []ToolInfo{
		{Name: "get_soul", Description: "Read soul.md"},
	}

	result := Generate(tools, VaultStats{}, StrategyFallback)

	if !strings.Contains(result, "get_soul") {
		t.Error("missing tool in output with empty stats")
	}
	if !strings.Contains(result, "## Vault Statistics") {
		t.Error("missing stats section")
	}
	// Zero-value stats should render zeros.
	if !strings.Contains(result, "| Wiki pages | 0 |") {
		t.Error("missing zero wiki count in stats")
	}
	// soul.md line should NOT appear when HasSoul is false.
	if strings.Contains(result, "soul.md is present") {
		t.Error("soul.md line should not appear when HasSoul is false")
	}
	// Top tags section should NOT appear when no tags.
	if strings.Contains(result, "**Top tags:**") {
		t.Error("top tags section should not appear when no tags exist")
	}
}

func TestWriteIfChanged_FirstWrite(t *testing.T) {
	dir := t.TempDir()
	content := "# AGENTS.md\nTest content"

	written, err := WriteIfChanged(dir, content)
	if err != nil {
		t.Fatalf("WriteIfChanged: %v", err)
	}
	if !written {
		t.Error("expected first write to return true")
	}

	// Verify file was created.
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("written content mismatch: got %q, want %q", string(data), content)
	}
}

func TestWriteIfChanged_NoChangeSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	content := "# AGENTS.md\nTest content"

	// First write.
	if _, err := WriteIfChanged(dir, content); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with same content.
	written, err := WriteIfChanged(dir, content)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if written {
		t.Error("expected second write with same content to return false")
	}
}

func TestWriteIfChanged_ContentChangedWrites(t *testing.T) {
	dir := t.TempDir()

	// First write.
	if _, err := WriteIfChanged(dir, "version 1"); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with different content.
	written, err := WriteIfChanged(dir, "version 2")
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if !written {
		t.Error("expected write with changed content to return true")
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading updated file: %v", err)
	}
	if string(data) != "version 2" {
		t.Errorf("content not updated: got %q", string(data))
	}
}

func TestCollectStats_AccurateCounts(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	// Create wiki files.
	if _, err := v.WriteWiki("page-one.md", "---\ntags:\n  - golang\n  - testing\n---\n# Page One"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.WriteWiki("page-two.md", "---\ntags:\n  - golang\n---\n# Page Two"); err != nil {
		t.Fatal(err)
	}

	// Create a raw file.
	if _, err := v.WriteRaw("page-one.md", "raw content"); err != nil {
		t.Fatal(err)
	}

	// Create a note.
	notesDir := filepath.Join(dir, "notes")
	if err := os.WriteFile(filepath.Join(notesDir, "my-note.md"), []byte("note content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create soul.md.
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte("I am a developer"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update index with tags.
	if err := v.UpdateIndex("page-one", "Page One", []string{"golang", "testing"}); err != nil {
		t.Fatal(err)
	}
	if err := v.UpdateIndex("page-two", "Page Two", []string{"golang"}); err != nil {
		t.Fatal(err)
	}

	// Create store with edges.
	dbPath := filepath.Join(dir, ".test.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Add some edges.
	edges := []store.Edge{
		{FromSlug: "page-one", ToSlug: "page-two", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom("page-one", "wikilink", edges); err != nil {
		t.Fatalf("UpsertEdgesFrom: %v", err)
	}

	stats, err := CollectStats(v, db)
	if err != nil {
		t.Fatalf("CollectStats: %v", err)
	}

	if stats.WikiCount != 2 {
		t.Errorf("WikiCount = %d, want 2", stats.WikiCount)
	}
	if stats.RawCount != 1 {
		t.Errorf("RawCount = %d, want 1", stats.RawCount)
	}
	if stats.NoteCount != 1 {
		t.Errorf("NoteCount = %d, want 1", stats.NoteCount)
	}
	if stats.EdgeCount != 1 {
		t.Errorf("EdgeCount = %d, want 1", stats.EdgeCount)
	}
	if !stats.HasSoul {
		t.Error("HasSoul = false, want true")
	}
	if len(stats.TopTags) == 0 {
		t.Fatal("TopTags is empty, want at least one tag")
	}
	if stats.TopTags[0].Tag != "golang" {
		t.Errorf("TopTags[0].Tag = %q, want %q", stats.TopTags[0].Tag, "golang")
	}
	if stats.TopTags[0].Count != 2 {
		t.Errorf("TopTags[0].Count = %d, want 2", stats.TopTags[0].Count)
	}
}

func TestCollectStats_NilDB(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	stats, err := CollectStats(v, nil)
	if err != nil {
		t.Fatalf("CollectStats with nil db: %v", err)
	}

	if stats.EdgeCount != 0 {
		t.Errorf("EdgeCount = %d, want 0 with nil db", stats.EdgeCount)
	}
}
