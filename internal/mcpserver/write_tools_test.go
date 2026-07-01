package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// setupWriteTestVault creates a temporary vault with a wiki page and store.
func setupWriteTestVault(t *testing.T) (*vault.Vault, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"wiki", "raw", "notes"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("creating %s dir: %v", sub, err)
		}
	}

	dbPath := filepath.Join(dir, "test.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	v := vault.New(dir)
	return v, db, dir
}

// ---------------------------------------------------------------------------
// HandleUpdateWiki
// ---------------------------------------------------------------------------

const wikiPageWithSections = `---
title: "Test Page"
source: "https://example.com/test"
date_saved: 2024-01-15
tags:
  - test
type: bookmark
---

# Test Page

## Summary
Original summary content.

## Key Takeaways
- Point one
- Point two

## Related
- [[other-page]] — Other page
`

func TestHandleUpdateWiki_ReplacesSection(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/test-page.md"), []byte(wikiPageWithSections), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "test-page", "Summary", "New summary content.\nWith multiple lines.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/test-page.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	// New content should be present.
	if !strings.Contains(s, "New summary content.") {
		t.Errorf("expected new summary content, got:\n%s", s)
	}
	// Old content should be gone.
	if strings.Contains(s, "Original summary content.") {
		t.Errorf("old summary content should be replaced, got:\n%s", s)
	}
	// Other sections should be preserved.
	if !strings.Contains(s, "## Key Takeaways") {
		t.Errorf("Key Takeaways section should be preserved, got:\n%s", s)
	}
	if !strings.Contains(s, "- Point one") {
		t.Errorf("Key Takeaways content should be preserved, got:\n%s", s)
	}
}

func TestHandleUpdateWiki_SectionAtEOF(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/test-page.md"), []byte(wikiPageWithSections), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "test-page", "Related", "- [[new-link]] — New link")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/test-page.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "- [[new-link]] — New link") {
		t.Errorf("expected new Related content, got:\n%s", s)
	}
	if strings.Contains(s, "- [[other-page]]") {
		t.Errorf("old Related content should be replaced, got:\n%s", s)
	}
}

func TestHandleUpdateWiki_EmptySection(t *testing.T) {
	page := `---
title: "Empty Section Test"
tags: []
---

# Empty Section Test

## Summary

## Key Takeaways
- Something
`
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/empty-section.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "empty-section", "Summary", "Now has content.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/empty-section.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "Now has content.") {
		t.Errorf("expected new content in empty section, got:\n%s", s)
	}
	if !strings.Contains(s, "## Key Takeaways") {
		t.Errorf("Key Takeaways should be preserved, got:\n%s", s)
	}
}

func TestHandleUpdateWiki_CodeBlocksWithHashHash(t *testing.T) {
	page := `---
title: "Code Block Test"
tags: []
---

# Code Block Test

## Summary
Some text.

` + "```markdown" + `
## This is inside a code block
Not a real heading
` + "```" + `

## Notes
Real notes section.
`
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/code-block.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "code-block", "Summary", "Updated summary.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/code-block.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "Updated summary.") {
		t.Errorf("expected updated summary, got:\n%s", s)
	}
	// The code block should be preserved as part of the content between Summary and Notes.
	// After update, Summary section ends at the next ## (Notes), so code block is removed with old content.
	if !strings.Contains(s, "## Notes") {
		t.Errorf("Notes section should be preserved, got:\n%s", s)
	}
	if !strings.Contains(s, "Real notes section.") {
		t.Errorf("Notes content should be preserved, got:\n%s", s)
	}
}

func TestHandleUpdateWiki_UpdatesLastUpdated(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/test-page.md"), []byte(wikiPageWithSections), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "test-page", "Summary", "Updated.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/test-page.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "last_updated:") {
		t.Errorf("expected last_updated in frontmatter, got:\n%s", s)
	}
	// Verify the date is in YYYY-MM-DD format and is today's date.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(s, "last_updated: "+today) {
		t.Errorf("expected last_updated to be %q, got:\n%s", today, s)
	}
	// Verify last_updated is inside frontmatter (between --- delimiters).
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		t.Fatalf("expected frontmatter delimiters, got:\n%s", s)
	}
	if !strings.Contains(parts[1], "last_updated: "+today) {
		t.Errorf("last_updated should be inside frontmatter, got frontmatter:\n%s", parts[1])
	}
}

func TestHandleUpdateWiki_SectionNotFound(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/test-page.md"), []byte(wikiPageWithSections), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "test-page", "Nonexistent", "content")
	if err == nil {
		t.Fatalf("expected error for nonexistent section")
	}
	if !strings.Contains(err.Error(), "section") {
		t.Errorf("error should mention section, got: %v", err)
	}
}

func TestHandleUpdateWiki_RelatedSyncsEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	page := `---
title: "Edge Test"
tags: []
---

# Edge Test

## Summary
Some content.

## Related
- [[old-link]] — Old
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/edge-test.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	// Insert initial edges.
	initialEdges := []store.Edge{
		{FromSlug: "edge-test", ToSlug: "old-link", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom("edge-test", "wikilink", initialEdges); err != nil {
		t.Fatalf("inserting initial edges: %v", err)
	}

	// Update Related section with new links.
	err := HandleUpdateWiki(v, db, "edge-test", "Related", "- [[new-link-a]] — A\n- [[new-link-b]] — B")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	// Check edges were synced.
	edges, err := db.GetOutboundEdges("edge-test")
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}

	slugs := make(map[string]bool)
	for _, e := range edges {
		slugs[e.ToSlug] = true
	}

	if slugs["old-link"] {
		t.Errorf("old-link edge should be removed after Related update")
	}
	if !slugs["new-link-a"] {
		t.Errorf("new-link-a edge should be present after Related update")
	}
	if !slugs["new-link-b"] {
		t.Errorf("new-link-b edge should be present after Related update")
	}
}

func TestHandleUpdateWiki_PageNotFound(t *testing.T) {
	v, db, _ := setupWriteTestVault(t)

	err := HandleUpdateWiki(v, db, "nonexistent", "Summary", "content")
	if err == nil {
		t.Fatalf("expected error for nonexistent page")
	}
}

// ---------------------------------------------------------------------------
// HandleDeletePage
// ---------------------------------------------------------------------------

func TestHandleDeletePage_DeletesAllArtifacts(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	// Create wiki file, raw file, and index entry.
	wikiContent := `---
title: "Delete Me"
tags: [test]
---

# Delete Me

## Summary
Content to delete.
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/delete-me.md"), []byte(wikiContent), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "raw/delete-me.md"), []byte("raw content"), 0o644); err != nil {
		t.Fatalf("writing raw page: %v", err)
	}

	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[delete-me]] — Delete Me [test]
- [[keep-me]] — Keep Me [other]
`
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	// Add bookmark record.
	if err := db.UpsertBookmark(&store.BookmarkRecord{
		WikiPath:      "wiki/delete-me.md",
		RawPath:       "raw/delete-me.md",
		Title:         "Delete Me",
		URL:           "https://example.com/delete",
		URLNormalized: "https://example.com/delete",
	}); err != nil {
		t.Fatalf("inserting bookmark: %v", err)
	}

	// Add edges.
	edges := []store.Edge{
		{FromSlug: "delete-me", ToSlug: "keep-me", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom("delete-me", "wikilink", edges); err != nil {
		t.Fatalf("inserting edges: %v", err)
	}

	// Add inbound edge from another page.
	inbound := []store.Edge{
		{FromSlug: "other-page", ToSlug: "delete-me", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom("other-page", "wikilink", inbound); err != nil {
		t.Fatalf("inserting inbound edge: %v", err)
	}

	result, err := HandleDeletePage(v, db, "delete-me")
	if err != nil {
		t.Fatalf("HandleDeletePage: %v", err)
	}

	// Wiki file should be deleted.
	if _, err := os.Stat(filepath.Join(dir, "wiki/delete-me.md")); !os.IsNotExist(err) {
		t.Errorf("wiki file should be deleted")
	}

	// Raw file should be deleted.
	if _, err := os.Stat(filepath.Join(dir, "raw/delete-me.md")); !os.IsNotExist(err) {
		t.Errorf("raw file should be deleted")
	}

	// Index should not contain the slug.
	idx, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	if strings.Contains(string(idx), "[[delete-me]]") {
		t.Errorf("index should not contain deleted slug, got:\n%s", string(idx))
	}
	// Other entries preserved.
	if !strings.Contains(string(idx), "[[keep-me]]") {
		t.Errorf("index should preserve other entries, got:\n%s", string(idx))
	}

	// All edges involving slug should be deleted.
	outbound, err := db.GetOutboundEdges("delete-me")
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}
	if len(outbound) != 0 {
		t.Errorf("expected 0 outbound edges, got %d", len(outbound))
	}

	inboundAfter, err := db.GetInboundEdges("delete-me")
	if err != nil {
		t.Fatalf("getting inbound edges: %v", err)
	}
	if len(inboundAfter) != 0 {
		t.Errorf("expected 0 inbound edges, got %d", len(inboundAfter))
	}

	// Bookmark DB record should be deleted.
	rec, err := db.GetBookmarkByURL("https://example.com/delete")
	if err != nil {
		t.Fatalf("getting bookmark: %v", err)
	}
	if rec != nil {
		t.Errorf("bookmark record should be deleted")
	}

	// Result should contain dangling refs.
	if result.Slug != "delete-me" {
		t.Errorf("result slug = %q, want %q", result.Slug, "delete-me")
	}
	if len(result.DanglingRefs) != 1 || result.DanglingRefs[0] != "other-page" {
		t.Errorf("dangling refs = %v, want [other-page]", result.DanglingRefs)
	}
}

func TestHandleDeletePage_NoteSlug(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	// Create wiki/notes subdirectory.
	if err := os.MkdirAll(filepath.Join(dir, "wiki", "notes"), 0o755); err != nil {
		t.Fatalf("creating wiki/notes dir: %v", err)
	}

	noteWiki := `---
title: "My Note"
tags: [test]
---

# My Note
Wiki copy of the note.
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/notes/my-note.md"), []byte(noteWiki), 0o644); err != nil {
		t.Fatalf("writing wiki note: %v", err)
	}

	// Add note record (note path is what gets stored).
	if err := db.UpsertNote("notes/my-note.md", "abc123", "wiki/notes/my-note.md"); err != nil {
		t.Fatalf("inserting note: %v", err)
	}

	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[notes/my-note]] — My Note [test]
`
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	result, err := HandleDeletePage(v, db, "notes/my-note")
	if err != nil {
		t.Fatalf("HandleDeletePage: %v", err)
	}

	// Wiki file should be deleted.
	if _, err := os.Stat(filepath.Join(dir, "wiki/notes/my-note.md")); !os.IsNotExist(err) {
		t.Errorf("wiki note file should be deleted")
	}

	// Note DB record should be deleted.
	hash, err := db.GetNoteHash("notes/my-note.md")
	if err != nil {
		t.Fatalf("getting note hash: %v", err)
	}
	if hash != "" {
		t.Errorf("note record should be deleted, but hash = %q", hash)
	}

	// Index should not contain the slug.
	idx, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("reading index: %v", err)
	}
	if strings.Contains(string(idx), "[[notes/my-note]]") {
		t.Errorf("index should not contain deleted note slug")
	}

	if result.Slug != "notes/my-note" {
		t.Errorf("result slug = %q, want %q", result.Slug, "notes/my-note")
	}
}

func TestHandleDeletePage_InboundEdgesCollectedBeforeDelete(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	wikiContent := `---
title: "Target"
tags: []
---

# Target
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/target.md"), []byte(wikiContent), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}
	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[target]] — Target []
`
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	// Multiple pages point to target.
	for _, from := range []string{"page-a", "page-b", "page-c"} {
		edges := []store.Edge{
			{FromSlug: from, ToSlug: "target", Type: "wikilink"},
		}
		if err := db.UpsertEdgesFrom(from, "wikilink", edges); err != nil {
			t.Fatalf("inserting edge from %s: %v", from, err)
		}
	}

	result, err := HandleDeletePage(v, db, "target")
	if err != nil {
		t.Fatalf("HandleDeletePage: %v", err)
	}

	// Should have all 3 dangling refs even though edges are now deleted.
	if len(result.DanglingRefs) != 3 {
		t.Errorf("expected 3 dangling refs, got %d: %v", len(result.DanglingRefs), result.DanglingRefs)
	}
}

func TestHandleDeletePage_NoRawFile(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	wikiContent := `---
title: "No Raw"
tags: []
---

# No Raw
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/no-raw.md"), []byte(wikiContent), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}
	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[no-raw]] — No Raw []
`
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	// No raw file — should not error.
	result, err := HandleDeletePage(v, db, "no-raw")
	if err != nil {
		t.Fatalf("HandleDeletePage: %v", err)
	}
	if result.Slug != "no-raw" {
		t.Errorf("result slug = %q, want %q", result.Slug, "no-raw")
	}
}

func TestHandleDeletePage_PageNotFound(t *testing.T) {
	v, db, _ := setupWriteTestVault(t)

	_, err := HandleDeletePage(v, db, "nonexistent")
	if err == nil {
		t.Fatalf("expected error for nonexistent page")
	}
}

func TestHandleDeletePage_WrappedInFileLock(t *testing.T) {
	// This test verifies the function accepts and uses the store (which provides WithFileLock).
	// The actual locking is an integration concern, but we verify the function signature
	// and that it completes without error when all artifacts exist.
	v, db, dir := setupWriteTestVault(t)

	wikiContent := `---
title: "Lock Test"
tags: []
---

# Lock Test
`
	if err := os.WriteFile(filepath.Join(dir, "wiki/lock-test.md"), []byte(wikiContent), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}
	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[lock-test]] — Lock Test []
`
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	result, err := HandleDeletePage(v, db, "lock-test")
	if err != nil {
		t.Fatalf("HandleDeletePage: %v", err)
	}
	if result.Slug != "lock-test" {
		t.Errorf("result slug = %q, want %q", result.Slug, "lock-test")
	}
}

// ---------------------------------------------------------------------------
// Helper function unit tests
// ---------------------------------------------------------------------------

func TestParseSections_BasicSplit(t *testing.T) {
	body := `# Title

## Summary
Some summary.

## Key Takeaways
- Point one

## Related
- [[link]]
`
	sections := parseSections(body)
	if len(sections) != 4 { // preamble + 3 sections
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}
	if sections[0].heading != "" {
		t.Errorf("preamble should have empty heading, got %q", sections[0].heading)
	}
	if sections[1].heading != "Summary" {
		t.Errorf("section 1 heading = %q, want %q", sections[1].heading, "Summary")
	}
	if sections[2].heading != "Key Takeaways" {
		t.Errorf("section 2 heading = %q, want %q", sections[2].heading, "Key Takeaways")
	}
	if sections[3].heading != "Related" {
		t.Errorf("section 3 heading = %q, want %q", sections[3].heading, "Related")
	}
}

func TestParseSections_CodeBlockPreservesHeadings(t *testing.T) {
	body := "## Real\nBefore code.\n```\n## Fake Heading\n```\n## Also Real\nAfter code.\n"
	sections := parseSections(body)
	// preamble (empty) + Real + Also Real = 3
	headings := make([]string, len(sections))
	for i, s := range sections {
		headings[i] = s.heading
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d: %v", len(sections), headings)
	}
	if sections[1].heading != "Real" {
		t.Errorf("section 1 heading = %q, want %q", sections[1].heading, "Real")
	}
	if !strings.Contains(sections[1].content, "## Fake Heading") {
		t.Errorf("code block heading should be in Real section content, got:\n%s", sections[1].content)
	}
	if sections[2].heading != "Also Real" {
		t.Errorf("section 2 heading = %q, want %q", sections[2].heading, "Also Real")
	}
}

func TestParseSections_RoundTrip(t *testing.T) {
	// parseSections absorbs trailing blank lines before ## headings into the
	// preceding section's content. This is by design — the heading prefix
	// ("## Heading\n") does not include the leading blank line. So a true
	// byte-for-byte round trip is only guaranteed when there are no blank
	// lines immediately before ## headings. We test that case here.
	body := "Preamble text.\n## Summary\nContent A.\n## Notes\nContent B.\n"
	sections := parseSections(body)
	var rebuilt strings.Builder
	for _, s := range sections {
		rebuilt.WriteString(s.raw())
	}
	if rebuilt.String() != body {
		t.Errorf("round-trip failed.\nInput:\n%q\nOutput:\n%q", body, rebuilt.String())
	}

	// Also verify that a round trip with blank lines at least preserves all content
	// (blank lines end up as trailing content of the preceding section).
	bodyWithBlanks := "Preamble.\n\n## A\nContent A.\n\n## B\nContent B.\n"
	sections2 := parseSections(bodyWithBlanks)
	var rebuilt2 strings.Builder
	for _, s := range sections2 {
		rebuilt2.WriteString(s.raw())
	}
	if rebuilt2.String() != bodyWithBlanks {
		// This is expected to match because blank lines are part of the
		// content of the preceding section.
		t.Errorf("round-trip with blanks failed.\nInput:\n%q\nOutput:\n%q", bodyWithBlanks, rebuilt2.String())
	}
}

func TestSplitFrontmatter_WithFrontmatter(t *testing.T) {
	content := "---\ntitle: Test\ntags: []\n---\n\n# Body\n"
	fm, body := splitFrontmatter(content)
	if !strings.HasPrefix(fm, "---") {
		t.Errorf("frontmatter should start with ---, got:\n%s", fm)
	}
	if !strings.HasSuffix(fm, "\n") {
		t.Errorf("frontmatter should end with newline, got:\n%q", fm)
	}
	if !strings.Contains(fm, "title: Test") {
		t.Errorf("frontmatter should contain title, got:\n%s", fm)
	}
	if !strings.HasPrefix(body, "\n# Body") {
		t.Errorf("body should start with the content after frontmatter, got:\n%q", body)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# No Frontmatter\nJust body.\n"
	fm, body := splitFrontmatter(content)
	if fm != "" {
		t.Errorf("expected empty frontmatter, got:\n%s", fm)
	}
	if body != content {
		t.Errorf("body should be entire content, got:\n%s", body)
	}
}

// TestSplitFrontmatter_ExactlyDelimiter covers content that is exactly "---"
// with nothing else. There's no room for a body-separating newline or a
// closing delimiter, so this must be treated as no-frontmatter: the whole
// content is returned as the body.
func TestSplitFrontmatter_ExactlyDelimiter(t *testing.T) {
	content := "---"
	fm, body := splitFrontmatter(content)
	if fm != "" {
		t.Errorf("expected empty frontmatter for bare %q, got:\n%q", content, fm)
	}
	if body != content {
		t.Errorf("expected body to be the whole content for bare %q, got:\n%q", content, body)
	}
}

// TestSplitFrontmatter_UnterminatedFrontmatter covers an opening "---\n" with
// no matching closing "---". Malformed/unterminated frontmatter must be
// treated as no-frontmatter (frontmatter == "", body == the whole content)
// rather than silently truncating or misparsing the content.
func TestSplitFrontmatter_UnterminatedFrontmatter(t *testing.T) {
	content := "---\ntitle: Unterminated\nno closing delimiter below\nmore body text\n"
	fm, body := splitFrontmatter(content)
	if fm != "" {
		t.Errorf("expected empty frontmatter for unterminated frontmatter, got:\n%q", fm)
	}
	if body != content {
		t.Errorf("expected body to be the whole content for unterminated frontmatter, got:\n%q", body)
	}
}

// TestHandleEditPage_MalformedFrontmatterTreatedAsBody pins the integration
// behavior for a wiki page whose leading "---" is never closed: since
// splitFrontmatter treats this as no-frontmatter, HandleEditPage does not
// preserve any part of the original content (frontmatter-looking or not) —
// consistent with its documented no-frontmatter case (see
// TestHandleEditPage_NoFrontmatterDoesNotFabricateIt) — and does not fabricate
// a frontmatter block for the new body either.
func TestHandleEditPage_MalformedFrontmatterTreatedAsBody(t *testing.T) {
	page := "---\ntitle: Unterminated\nno closing delimiter below\nOriginal content.\n"
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/malformed-fm.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleEditPage(v, db, "malformed-fm", "## Rewritten\nNew content.\n")
	if err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/malformed-fm.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if strings.HasPrefix(s, "---") {
		t.Errorf("edit_page must not fabricate frontmatter for a page with unterminated frontmatter, got:\n%s", s)
	}
	if !strings.Contains(s, "## Rewritten") {
		t.Errorf("expected new body content, got:\n%s", s)
	}
	if strings.Contains(s, "Original content.") || strings.Contains(s, "Unterminated") {
		t.Errorf("old (unparsed) content should be replaced, got:\n%s", s)
	}
}

func TestUpsertFrontmatterField_AddsNew(t *testing.T) {
	fm := "---\ntitle: Test\ntags: []\n---\n"
	result := upsertFrontmatterField(fm, "last_updated", "2024-06-01")
	if !strings.Contains(result, "last_updated: 2024-06-01") {
		t.Errorf("expected last_updated field, got:\n%s", result)
	}
	// Should still have closing ---
	if !strings.Contains(result, "---") {
		t.Errorf("should preserve frontmatter delimiters, got:\n%s", result)
	}
}

func TestUpsertFrontmatterField_UpdatesExisting(t *testing.T) {
	fm := "---\ntitle: Test\nlast_updated: 2024-01-01\ntags: []\n---\n"
	result := upsertFrontmatterField(fm, "last_updated", "2024-06-01")
	if !strings.Contains(result, "last_updated: 2024-06-01") {
		t.Errorf("expected updated last_updated, got:\n%s", result)
	}
	if strings.Contains(result, "2024-01-01") {
		t.Errorf("old date should be replaced, got:\n%s", result)
	}
}

func TestHandleUpdateWiki_PreservesSectionHeading(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/test-page.md"), []byte(wikiPageWithSections), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleUpdateWiki(v, db, "test-page", "Summary", "New content.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/test-page.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	// The ## Summary heading must still be present.
	if !strings.Contains(s, "## Summary") {
		t.Errorf("## Summary heading should be preserved, got:\n%s", s)
	}
	// Frontmatter should be preserved.
	if !strings.Contains(s, "title: \"Test Page\"") {
		t.Errorf("frontmatter title should be preserved, got:\n%s", s)
	}
	if !strings.Contains(s, "source: \"https://example.com/test\"") {
		t.Errorf("frontmatter source should be preserved, got:\n%s", s)
	}
}

func TestHandleUpdateWiki_NonRelatedDoesNotSyncEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	page := "---\ntitle: Edge Test\ntags: []\n---\n\n# Edge Test\n\n## Summary\nOld.\n\n## Related\n- [[existing-link]] — Existing\n"
	if err := os.WriteFile(filepath.Join(dir, "wiki/edge-test.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	// Insert initial edges.
	initialEdges := []store.Edge{
		{FromSlug: "edge-test", ToSlug: "existing-link", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom("edge-test", "wikilink", initialEdges); err != nil {
		t.Fatalf("inserting initial edges: %v", err)
	}

	// Update Summary (NOT Related) — edges should remain unchanged.
	err := HandleUpdateWiki(v, db, "edge-test", "Summary", "Updated summary.")
	if err != nil {
		t.Fatalf("HandleUpdateWiki: %v", err)
	}

	edges, err := db.GetOutboundEdges("edge-test")
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}
	if len(edges) != 1 || edges[0].ToSlug != "existing-link" {
		t.Errorf("edges should be unchanged after non-Related update, got: %v", edges)
	}
}

// ---------------------------------------------------------------------------
// HandleEditPage
// ---------------------------------------------------------------------------
//
// edit_page replaces the ENTIRE body of an existing wiki page in one call
// (superset of update_wiki, which is scoped to a single ## section). See
// docs/plans/plan-edit-page-mcp-tool.md. Frontmatter (title, source, tags,
// created) is system-owned: the tool preserves it verbatim and only bumps
// last_updated. All [[wikilinks]] in the new body — anywhere, not just under
// a "Related" heading — are re-derived as edges via db.UpsertEdgesFrom,
// replacing the slug's full outbound edge set.

const wikiPageForEditPage = `---
title: "Edit Page Test"
source: "https://example.com/edit-test"
created: 2024-01-10
tags:
  - test
  - edit
---

# Edit Page Test

## Summary
Original summary content.

## Key Takeaways
- Point one
- Point two

## Related
- [[old-link-a]] — Old A
- [[old-link-b]] — Old B
`

// writeEditPageFixture writes wikiPageForEditPage (or a custom page) to
// wiki/edit-page-test.md and returns its slug.
func writeEditPageFixture(t *testing.T, dir, page string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "wiki/edit-page-test.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page fixture: %v", err)
	}
	return "edit-page-test"
}

func TestHandleEditPage_ReplacesWholeBody(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	newBody := "## Overview\nCompletely rewritten page.\n\n## Details\nMore new content.\n"
	if err := HandleEditPage(v, db, slug, newBody); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	// New body content is present verbatim.
	if !strings.Contains(s, "## Overview") {
		t.Errorf("expected new '## Overview' heading, got:\n%s", s)
	}
	if !strings.Contains(s, "Completely rewritten page.") {
		t.Errorf("expected new body content, got:\n%s", s)
	}
	if !strings.Contains(s, "## Details") {
		t.Errorf("expected new '## Details' heading, got:\n%s", s)
	}

	// Old sections are entirely gone — this is a whole-body replace, not a
	// section-scoped merge.
	for _, old := range []string{"## Summary", "Original summary content.", "## Key Takeaways", "- Point one", "## Related", "old-link-a"} {
		if strings.Contains(s, old) {
			t.Errorf("old body content %q should be gone after edit_page, got:\n%s", old, s)
		}
	}
}

func TestHandleEditPage_PreservesFrontmatterAndBumpsLastUpdated(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	page := `---
title: "Edit Page Test"
source: "https://example.com/edit-test"
created: 2024-01-10
last_updated: 2024-01-11
tags:
  - test
  - edit
---

# Edit Page Test

## Summary
Original summary content.
`
	slug := writeEditPageFixture(t, dir, page)

	if err := HandleEditPage(v, db, slug, "## New\nNew body.\n"); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	// Frontmatter fields preserved verbatim.
	for _, want := range []string{
		`title: "Edit Page Test"`,
		`source: "https://example.com/edit-test"`,
		"created: 2024-01-10",
		"- test",
		"- edit",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected frontmatter to preserve %q, got:\n%s", want, s)
		}
	}

	// last_updated is bumped to today, old value gone.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(s, "last_updated: "+today) {
		t.Errorf("expected last_updated bumped to %q, got:\n%s", today, s)
	}
	if strings.Contains(s, "last_updated: 2024-01-11") {
		t.Errorf("old last_updated value should be replaced, got:\n%s", s)
	}

	// last_updated must be inside the frontmatter block, not the body.
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		t.Fatalf("expected frontmatter delimiters, got:\n%s", s)
	}
	if !strings.Contains(parts[1], "last_updated: "+today) {
		t.Errorf("last_updated should be inside frontmatter, got frontmatter:\n%s", parts[1])
	}
}

func TestHandleEditPage_ProvenanceCannotBeStripped(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	// The caller's new body itself contains frontmatter-looking text,
	// including an attempt to overwrite the provenance fields. This must be
	// treated as opaque body content, never merged into or replacing the
	// real system-owned frontmatter.
	maliciousBody := "---\n" +
		"title: \"Hacked Title\"\n" +
		"source: \"https://evil.example.com\"\n" +
		"---\n\n" +
		"Malicious body pretending to carry its own frontmatter.\n"

	if err := HandleEditPage(v, db, slug, maliciousBody); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	// The real (first) frontmatter block must still carry the original,
	// system-owned provenance fields.
	fm, _ := splitFrontmatter(s)
	if !strings.Contains(fm, `title: "Edit Page Test"`) {
		t.Errorf("frontmatter title must not be overridden by body content, got frontmatter:\n%s", fm)
	}
	if !strings.Contains(fm, `source: "https://example.com/edit-test"`) {
		t.Errorf("frontmatter source must not be overridden by body content, got frontmatter:\n%s", fm)
	}
	if strings.Contains(fm, "Hacked Title") || strings.Contains(fm, "evil.example.com") {
		t.Errorf("caller-supplied frontmatter-looking text must not leak into real frontmatter, got:\n%s", fm)
	}
}

func TestHandleEditPage_NewWikilinksAnywhereBecomeEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	// Wikilinks scattered across multiple sections, not just under "Related" —
	// this is the superset behavior over update_wiki.
	newBody := "## Summary\nSee [[foo-page]] for background.\n\n" +
		"## Notes\nAlso relevant: [[bar-page]] and [[baz-page]].\n"

	if err := HandleEditPage(v, db, slug, newBody); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	edges, err := db.GetOutboundEdges(slug)
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}

	got := make(map[string]bool)
	for _, e := range edges {
		got[e.ToSlug] = true
	}
	for _, want := range []string{"foo-page", "bar-page", "baz-page"} {
		if !got[want] {
			t.Errorf("expected edge to %q after edit_page, got edges: %v", want, edges)
		}
	}
}

func TestHandleEditPage_RemovedWikilinksDropEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	// Seed the edges that correspond to the fixture's original "Related"
	// section, as if a prior enrichment/edit had synced them.
	initialEdges := []store.Edge{
		{FromSlug: slug, ToSlug: "old-link-a", Type: "wikilink"},
		{FromSlug: slug, ToSlug: "old-link-b", Type: "wikilink"},
	}
	if err := db.UpsertEdgesFrom(slug, "wikilink", initialEdges); err != nil {
		t.Fatalf("inserting initial edges: %v", err)
	}

	// New body has no wikilinks to the old targets, and introduces one new
	// link. The full edge set for the slug must be replaced.
	newBody := "## Summary\nRewritten, references [[fresh-link]] only.\n"
	if err := HandleEditPage(v, db, slug, newBody); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	edges, err := db.GetOutboundEdges(slug)
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}

	got := make(map[string]bool)
	for _, e := range edges {
		got[e.ToSlug] = true
	}
	if got["old-link-a"] || got["old-link-b"] {
		t.Errorf("stale edges from removed wikilinks should be dropped, got edges: %v", edges)
	}
	if !got["fresh-link"] {
		t.Errorf("expected edge to fresh-link, got edges: %v", edges)
	}
	if len(edges) != 1 {
		t.Errorf("expected exactly 1 outbound edge after replace, got %d: %v", len(edges), edges)
	}
}

func TestHandleEditPage_NonExistentSlugReturnsError(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)

	err := HandleEditPage(v, db, "does-not-exist", "## Body\nSome content.\n")
	if err == nil {
		t.Fatalf("expected error for nonexistent slug")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "wiki/does-not-exist.md")); !os.IsNotExist(statErr) {
		t.Errorf("edit_page must not create a wiki file for a nonexistent slug")
	}
}

func TestHandleEditPage_EmptyContentReturnsError(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	err := HandleEditPage(v, db, slug, "")
	if err == nil {
		t.Fatalf("expected error for empty content")
	}

	// The existing page must be left untouched.
	content, readErr := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if readErr != nil {
		t.Fatalf("reading wiki page: %v", readErr)
	}
	if string(content) != wikiPageForEditPage {
		t.Errorf("wiki page should be unchanged after a rejected empty-content call, got:\n%s", string(content))
	}
}

func TestHandleEditPage_WhitespaceOnlyContentReturnsError(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	err := HandleEditPage(v, db, slug, "\n\n  \n")
	if err == nil {
		t.Fatalf("expected error for whitespace-only content")
	}

	// The existing page must be left untouched: a whitespace-only body must
	// not sneak past the empty-content guard and silently wipe the page.
	content, readErr := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if readErr != nil {
		t.Fatalf("reading wiki page: %v", readErr)
	}
	if string(content) != wikiPageForEditPage {
		t.Errorf("wiki page should be unchanged after a rejected whitespace-only call, got:\n%s", string(content))
	}
}

func TestHandleEditPage_AllWikilinksRemovedClearsEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	// Seed edges via the handler itself, from a body with 2+ wikilinks.
	seedBody := "## Summary\nReferences [[link-a]] and [[link-b]].\n"
	if err := HandleEditPage(v, db, slug, seedBody); err != nil {
		t.Fatalf("HandleEditPage (seed): %v", err)
	}
	seeded, err := db.GetOutboundEdges(slug)
	if err != nil {
		t.Fatalf("getting outbound edges after seed: %v", err)
	}
	if len(seeded) != 2 {
		t.Fatalf("expected 2 seeded edges, got %d: %v", len(seeded), seeded)
	}

	// Edit again with a body containing no wikilinks at all.
	noLinksBody := "## Summary\nNo links here anymore.\n"
	if err := HandleEditPage(v, db, slug, noLinksBody); err != nil {
		t.Fatalf("HandleEditPage (clear): %v", err)
	}

	edges, err := db.GetOutboundEdges(slug)
	if err != nil {
		t.Fatalf("getting outbound edges after clear: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 outbound edges after removing all wikilinks, got %d: %v", len(edges), edges)
	}
}

func TestHandleEditPage_WikilinksInCodeBlockAreNotEdges(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	newBody := "## Summary\nSee [[real-link]] for details.\n\n" +
		"```\nExample: [[should-not-link]]\n```\n"

	if err := HandleEditPage(v, db, slug, newBody); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	edges, err := db.GetOutboundEdges(slug)
	if err != nil {
		t.Fatalf("getting outbound edges: %v", err)
	}

	got := make(map[string]bool)
	for _, e := range edges {
		got[e.ToSlug] = true
	}
	if !got["real-link"] {
		t.Errorf("expected edge to real-link, got edges: %v", edges)
	}
	if got["should-not-link"] {
		t.Errorf("wikilink inside a fenced code block must not become an edge, got edges: %v", edges)
	}
	if len(edges) != 1 {
		t.Errorf("expected exactly 1 outbound edge, got %d: %v", len(edges), edges)
	}
}

func TestHandleEditPage_IndexUnchanged(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	indexContent := `# Wiki Index

Last updated: 2024-01-15

## Pages

- [[edit-page-test]] — Edit Page Test [test, edit]
- [[other-page]] — Other Page [other]
`
	indexPath := filepath.Join(dir, "index.md")
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	// Body-only edit: frontmatter title/tags are untouched, so index.md
	// should require no update at all.
	if err := HandleEditPage(v, db, slug, "## New\nRewritten body, no frontmatter change.\n"); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	after, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading index after edit: %v", err)
	}
	if string(after) != indexContent {
		t.Errorf("index.md should be byte-for-byte unchanged after edit_page.\nbefore:\n%s\nafter:\n%s", indexContent, string(after))
	}
}

func TestHandleEditPage_NormalizesTrailingNewline(t *testing.T) {
	v, db, dir := setupWriteTestVault(t)
	slug := writeEditPageFixture(t, dir, wikiPageForEditPage)

	// Body supplied without a trailing newline.
	if err := HandleEditPage(v, db, slug, "## Body\nNo trailing newline in caller input."); err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/edit-page-test.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if !strings.HasSuffix(s, "\n") {
		t.Errorf("written wiki page should end with exactly one trailing newline, got: %q", s)
	}
	if strings.HasSuffix(s, "\n\n") {
		t.Errorf("written wiki page should not end with multiple trailing newlines, got: %q", s)
	}
}

func TestHandleEditPage_NoFrontmatterDoesNotFabricateIt(t *testing.T) {
	// A wiki page with no frontmatter at all is an edge case explicitly
	// called out in the plan: edit_page must not fabricate a frontmatter
	// block (matching update_wiki's existing behavior for this case).
	page := "# No Frontmatter Page\n\n## Summary\nOriginal content.\n"
	v, db, dir := setupWriteTestVault(t)
	if err := os.WriteFile(filepath.Join(dir, "wiki/no-frontmatter.md"), []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	err := HandleEditPage(v, db, "no-frontmatter", "## Rewritten\nNew content.\n")
	if err != nil {
		t.Fatalf("HandleEditPage: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wiki/no-frontmatter.md"))
	if err != nil {
		t.Fatalf("reading wiki page: %v", err)
	}
	s := string(content)

	if strings.HasPrefix(s, "---") {
		t.Errorf("edit_page must not fabricate frontmatter for a page that had none, got:\n%s", s)
	}
	if !strings.Contains(s, "## Rewritten") {
		t.Errorf("expected new body content, got:\n%s", s)
	}
	if strings.Contains(s, "Original content.") {
		t.Errorf("old body content should be replaced, got:\n%s", s)
	}
}
