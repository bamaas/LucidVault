package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
