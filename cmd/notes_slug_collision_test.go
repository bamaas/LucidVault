package main

// Tests for issue #95 / ADR-029 "Note Wiki Slugs Are Claimed Once, Suffixed on
// Collision": processNotes must assign a note's wiki slug once, store it in
// notes.wiki_path, and reuse it on every later cycle instead of recomputing it
// from the filename. On collision (another note or a bookmark already owns
// wiki/<slug>.md) the note gets the first free wiki/<slug>-2.md ... -100.md.
//
// These tests are spec-only: they describe the behaviour the ADR mandates and
// are expected to FAIL against the current (unfixed) processNotes, which still
// derives the slug unconditionally from notes.TitleFromFilename on every cycle.
//
// See docs/plans/plan-note-wiki-slug-collision.md and
// docs/adr/029-note-wiki-slug-claim-with-suffix.md.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// noteContent builds a minimal note with one frontmatter tag (so SuggestTags /
// Ollama is never called) and a body containing the given marker text, mirroring
// the fixture style used throughout cmd/main_test.go and cmd/notes_related_test.go.
func noteContent(h1, tag, body string) string {
	return fmt.Sprintf("---\ntags:\n  - %s\n---\n\n# %s\n\n%s\n", tag, h1, body)
}

// writeNoteFile writes content at vaultPath/relPath, creating parent directories
// as needed, and returns the absolute path.
func writeNoteFile(t *testing.T, vaultPath, relPath, content string) string {
	t.Helper()
	full := filepath.Join(vaultPath, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll for %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", relPath, err)
	}
	return full
}

// seedBookmarkFoo writes an existing bookmark wiki page at wiki/foo.md (using
// the shared relatedBookmarkWiki fixture from notes_related_test.go) and
// indexes it, simulating the #95 repro precondition: a bookmark already owns
// the "foo" slug before any colliding note is ever processed.
func seedBookmarkFoo(t *testing.T, v *vault.Vault) {
	t.Helper()
	if _, err := v.WriteWiki("foo.md", relatedBookmarkWiki); err != nil {
		t.Fatalf("WriteWiki bookmark foo.md: %v", err)
	}
	if err := v.UpdateIndex("foo", "Bookmark", []string{"golang", "testing"}); err != nil {
		t.Fatalf("UpdateIndex bookmark foo: %v", err)
	}
}

// findNoteRecord returns the note DB record for path, failing the test if it's
// not present. ListNotes is the store's public read API for notes; the plan's
// not-yet-implemented GetNote(path) would do this more directly, but tests must
// not depend on store methods that don't exist yet.
func findNoteRecord(t *testing.T, db *store.Store, path string) store.NoteRecord {
	t.Helper()
	recs, err := db.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	for _, r := range recs {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("no note record found for path %q", path)
	return store.NoteRecord{}
}

// noteRecordExists reports whether a note DB record exists for path.
func noteRecordExists(t *testing.T, db *store.Store, path string) bool {
	t.Helper()
	recs, err := db.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	for _, r := range recs {
		if r.Path == path {
			return true
		}
	}
	return false
}

// TestProcessNotes_SlugCollision is the #95 repro from the plan's Tests table:
// an existing bookmark page at wiki/foo.md, plus two notes with the same
// basename in different subfolders (notes/a/foo.md, notes/b/foo.md). All three
// must end up as distinct files: the bookmark page is untouched and keeps
// type: bookmark, and the two notes land on wiki/foo-2.md and wiki/foo-3.md
// (in some order — which note gets which suffix depends on scan order).
func TestProcessNotes_SlugCollision(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	seedBookmarkFoo(t, v)
	bookmarkBefore := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))

	writeNoteFile(t, tmpDir, "notes/a/foo.md", noteContent("Note A", "alpha", "Content from note A."))
	writeNoteFile(t, tmpDir, "notes/b/foo.md", noteContent("Note B", "beta", "Content from note B."))

	processNotes(ctx, en, db, v)

	// The bookmark page must be completely untouched.
	bookmarkAfter := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))
	if bookmarkAfter != bookmarkBefore {
		t.Errorf("expected wiki/foo.md (bookmark) to be unchanged by colliding notes\nbefore:\n%s\nafter:\n%s", bookmarkBefore, bookmarkAfter)
	}
	assertContains(t, bookmarkAfter, "type: bookmark")

	// Both notes must have been suffixed off the bookmark's slug, onto
	// wiki/foo-2.md and wiki/foo-3.md — never back onto wiki/foo.md.
	foo2Path := filepath.Join(tmpDir, "wiki", "foo-2.md")
	foo3Path := filepath.Join(tmpDir, "wiki", "foo-3.md")
	if _, err := os.Stat(foo2Path); os.IsNotExist(err) {
		t.Fatal("expected wiki/foo-2.md to exist")
	}
	if _, err := os.Stat(foo3Path); os.IsNotExist(err) {
		t.Fatal("expected wiki/foo-3.md to exist")
	}

	// Scan order determines which note gets -2 vs -3; assert both notes'
	// content shows up somewhere across the two suffixed files, and that
	// neither note overwrote the other.
	combined := readFile(t, foo2Path) + readFile(t, foo3Path)
	assertContains(t, combined, "Content from note A.")
	assertContains(t, combined, "Content from note B.")

	// Exactly three "foo*.md" pages exist in wiki/ — no fourth, no overwrite.
	entries, err := os.ReadDir(filepath.Join(tmpDir, "wiki"))
	if err != nil {
		t.Fatalf("ReadDir wiki: %v", err)
	}
	var fooFiles []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "foo") {
			fooFiles = append(fooFiles, e.Name())
		}
	}
	if len(fooFiles) != 3 {
		t.Errorf("expected exactly 3 foo*.md pages in wiki/, got %d: %v", len(fooFiles), fooFiles)
	}

	// Index carries all three distinct slugs.
	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertContains(t, indexContent, "[[foo]]")
	assertContains(t, indexContent, "[[foo-2]]")
	assertContains(t, indexContent, "[[foo-3]]")

	// Both notes' DB records resolved to distinct, non-bookmark wiki paths.
	recA := findNoteRecord(t, db, "notes/a/foo.md")
	recB := findNoteRecord(t, db, "notes/b/foo.md")
	if recA.WikiPath == "" || recB.WikiPath == "" {
		t.Fatalf("expected both notes to have a resolved wiki_path, got a=%q b=%q", recA.WikiPath, recB.WikiPath)
	}
	if recA.WikiPath == recB.WikiPath {
		t.Fatalf("expected notes/a/foo.md and notes/b/foo.md to resolve to distinct wiki_path values, both got %q", recA.WikiPath)
	}
	if recA.WikiPath == "wiki/foo.md" || recB.WikiPath == "wiki/foo.md" {
		t.Errorf("expected neither note to claim the bookmark's wiki/foo.md, got a=%q b=%q", recA.WikiPath, recB.WikiPath)
	}
}

// TestProcessNotes_SlugCollision_EditPersistsSlug covers the plan's second
// test case: once a colliding note has been assigned a suffixed slug, editing
// its content on a later cycle must rewrite that same suffixed file — not pick
// a new slug.
func TestProcessNotes_SlugCollision_EditPersistsSlug(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	seedBookmarkFoo(t, v)
	writeNoteFile(t, tmpDir, "notes/a/foo.md", noteContent("Note A", "alpha", "Content from note A."))
	writeNoteFile(t, tmpDir, "notes/b/foo.md", noteContent("Note B", "beta", "Content from note B, version one."))

	processNotes(ctx, en, db, v)

	recBBefore := findNoteRecord(t, db, "notes/b/foo.md")
	if recBBefore.WikiPath == "" {
		t.Fatal("expected notes/b/foo.md to have a resolved wiki_path after the first cycle")
	}
	if recBBefore.WikiPath == "wiki/foo.md" {
		t.Fatalf("expected notes/b/foo.md to be suffixed off the bookmark's slug, got %q", recBBefore.WikiPath)
	}
	bFile := filepath.Join(tmpDir, recBBefore.WikiPath)
	assertContains(t, readFile(t, bFile), "version one")

	// Edit note B's content on disk.
	writeNoteFile(t, tmpDir, "notes/b/foo.md", noteContent("Note B", "beta", "Content from note B, version two."))

	processNotes(ctx, en, db, v)

	recBAfter := findNoteRecord(t, db, "notes/b/foo.md")
	if recBAfter.WikiPath != recBBefore.WikiPath {
		t.Errorf("expected the resolved slug to stay stable across edits: before %q, after %q", recBBefore.WikiPath, recBAfter.WikiPath)
	}

	after := readFile(t, bFile)
	assertContains(t, after, "version two")
	assertNotContains(t, after, "version one")
}

// TestProcessNotes_SlugCollision_DeleteOneOfTwoNotes covers the plan's third
// test case: once both colliding notes are indexed under distinct suffixed
// slugs, deleting one of them from disk must remove only its own wiki page and
// index entry — the surviving note's page, its index entry, and the original
// bookmark page must be untouched.
func TestProcessNotes_SlugCollision_DeleteOneOfTwoNotes(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	seedBookmarkFoo(t, v)
	writeNoteFile(t, tmpDir, "notes/a/foo.md", noteContent("Note A", "alpha", "Content from note A."))
	writeNoteFile(t, tmpDir, "notes/b/foo.md", noteContent("Note B", "beta", "Content from note B."))

	processNotes(ctx, en, db, v)

	recA := findNoteRecord(t, db, "notes/a/foo.md")
	recB := findNoteRecord(t, db, "notes/b/foo.md")
	if recA.WikiPath == "" || recB.WikiPath == "" || recA.WikiPath == recB.WikiPath {
		t.Fatalf("test setup invariant violated: expected distinct resolved wiki_path values, got a=%q b=%q", recA.WikiPath, recB.WikiPath)
	}

	aSlug := strings.TrimSuffix(filepath.Base(recA.WikiPath), ".md")
	bSlug := strings.TrimSuffix(filepath.Base(recB.WikiPath), ".md")

	bFile := filepath.Join(tmpDir, recB.WikiPath)
	bContentBefore := readFile(t, bFile)
	bookmarkBefore := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))

	// Delete note A from disk.
	if err := os.Remove(filepath.Join(tmpDir, "notes", "a", "foo.md")); err != nil {
		t.Fatalf("Remove notes/a/foo.md: %v", err)
	}

	processNotes(ctx, en, db, v)

	// Note A's wiki page, index entry, and DB record are gone.
	if _, err := os.Stat(filepath.Join(tmpDir, recA.WikiPath)); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after deleting notes/a/foo.md", recA.WikiPath)
	}
	indexAfter := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertNotContains(t, indexAfter, "[["+aSlug+"]]")
	if noteRecordExists(t, db, "notes/a/foo.md") {
		t.Error("expected the DB record for notes/a/foo.md to be removed")
	}

	// Note B — the still-existing colliding note — is untouched.
	assertContains(t, indexAfter, "[["+bSlug+"]]")
	bContentAfter := readFile(t, bFile)
	if bContentAfter != bContentBefore {
		t.Errorf("expected notes/b/foo.md's wiki page to be untouched by deleting the colliding note\nbefore:\n%s\nafter:\n%s", bContentBefore, bContentAfter)
	}
	if !noteRecordExists(t, db, "notes/b/foo.md") {
		t.Error("expected the DB record for notes/b/foo.md to survive")
	}

	// The original bookmark page is untouched.
	bookmarkAfter := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))
	if bookmarkAfter != bookmarkBefore {
		t.Errorf("expected bookmark wiki/foo.md to be untouched by deleting a colliding note\nbefore:\n%s\nafter:\n%s", bookmarkBefore, bookmarkAfter)
	}
}

// TestProcessNotes_SlugCollision_RepairsPreExistingDuplicateWikiPath covers the
// plan's fourth test case: a vault already hit by #95 holds two note DB records
// that share the same wiki_path (seeded directly here via the store's public
// UpsertNote, simulating the legacy pre-fix state). After one processNotes
// cycle, the one-time repair + re-resolution described in the plan's step 4
// must give the two notes distinct wiki_path values and distinct, correct pages.
func TestProcessNotes_SlugCollision_RepairsPreExistingDuplicateWikiPath(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	writeNoteFile(t, tmpDir, "notes/a/foo.md", noteContent("Note A", "alpha", "Legacy content A."))
	writeNoteFile(t, tmpDir, "notes/b/foo.md", noteContent("Note B", "beta", "Legacy content B."))

	// Seed the legacy pre-fix DB state: both note records already point at the
	// same wiki_path, as the old unconditional-write code would have left them
	// after the second note silently overwrote the first's page.
	if err := db.UpsertNote("notes/a/foo.md", "legacy-hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("seeding legacy note record a: %v", err)
	}
	if err := db.UpsertNote("notes/b/foo.md", "legacy-hash-b", "wiki/foo.md"); err != nil {
		t.Fatalf("seeding legacy note record b: %v", err)
	}
	// And the wiki page on disk as the old code left it: whichever note was
	// processed last fully overwrote the file.
	if _, err := v.WriteWiki("foo.md", "legacy shared content, last writer wins\n"); err != nil {
		t.Fatalf("seeding legacy wiki/foo.md: %v", err)
	}

	processNotes(ctx, en, db, v)

	recA := findNoteRecord(t, db, "notes/a/foo.md")
	recB := findNoteRecord(t, db, "notes/b/foo.md")

	if recA.WikiPath == "" || recB.WikiPath == "" {
		t.Fatalf("expected both legacy note records to have a resolved wiki_path, got a=%q b=%q", recA.WikiPath, recB.WikiPath)
	}
	if recA.WikiPath == recB.WikiPath {
		t.Fatalf("expected the one-time repair to give the two legacy duplicate notes distinct wiki_path values, both got %q", recA.WikiPath)
	}

	// Both must now have real, distinct pages carrying their own content.
	aContent := readFile(t, filepath.Join(tmpDir, recA.WikiPath))
	bContent := readFile(t, filepath.Join(tmpDir, recB.WikiPath))
	assertContains(t, aContent, "Legacy content A.")
	assertContains(t, bContent, "Legacy content B.")
}

// TestProcessNotes_NoCollision_SlugUnchanged is the plan's fifth test case
// (regression guard): a unique top-level note with no colliding slug must land
// on its plain wiki/<name>.md exactly as it does today — no suffix, no change
// in behaviour for the common case.
func TestProcessNotes_NoCollision_SlugUnchanged(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	writeNoteFile(t, tmpDir, "notes/bar.md", noteContent("Bar", "gamma", "Unique content, no collision."))

	processNotes(ctx, en, db, v)

	rec := findNoteRecord(t, db, "notes/bar.md")
	if rec.WikiPath != "wiki/bar.md" {
		t.Errorf("expected the unique note's wiki_path to be unchanged at wiki/bar.md, got %q", rec.WikiPath)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "wiki", "bar.md")); os.IsNotExist(err) {
		t.Error("expected wiki/bar.md to exist")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "wiki", "bar-2.md")); !os.IsNotExist(err) {
		t.Error("expected no wiki/bar-2.md to be created when there is no collision")
	}

	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertContains(t, indexContent, "[[bar]]")
	assertNotContains(t, indexContent, "[[bar-2]]")
}

// TestProcessNotes_SlugCollision_AllHundredCandidatesTaken covers the plan's
// sixth test case: when wiki/foo.md through wiki/foo-100.md are all already
// taken, a new colliding note notes/foo.md must be skipped entirely — none of
// the 100 pre-existing pages may be overwritten, and the note must not gain a
// DB record (nothing was ever successfully claimed for it).
func TestProcessNotes_SlugCollision_AllHundredCandidatesTaken(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	wikiDir := filepath.Join(tmpDir, "wiki")
	existing := make(map[string]string, 100)
	for n := 1; n <= 100; n++ {
		filename := "foo.md"
		if n > 1 {
			filename = fmt.Sprintf("foo-%d.md", n)
		}
		content := fmt.Sprintf("pre-existing content for %s\n", filename)
		if err := os.WriteFile(filepath.Join(wikiDir, filename), []byte(content), 0o644); err != nil {
			t.Fatalf("pre-creating %s: %v", filename, err)
		}
		existing[filename] = content
	}

	writeNoteFile(t, tmpDir, "notes/foo.md", noteContent("Foo", "delta", "Should not land anywhere."))

	processNotes(ctx, en, db, v)

	// None of the 100 pre-existing pages may have been modified.
	for filename, want := range existing {
		got := readFile(t, filepath.Join(wikiDir, filename))
		if got != want {
			t.Errorf("expected %s to remain unchanged\nbefore:\n%s\nafter:\n%s", filename, want, got)
		}
	}

	// The note itself must not be indexed — no slug was available for it.
	hash, err := db.GetNoteHash("notes/foo.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "" {
		t.Errorf("expected notes/foo.md to be skipped (no DB record) when all 100 candidate slugs are taken, got hash %q", hash)
	}
	if noteRecordExists(t, db, "notes/foo.md") {
		t.Error("expected no note record for notes/foo.md when all 100 candidate slugs are taken")
	}

	// No 101st candidate file should ever be created — the cap is 100 attempts.
	if _, err := os.Stat(filepath.Join(wikiDir, "foo-101.md")); !os.IsNotExist(err) {
		t.Error("expected no foo-101.md to be created — the cap is 100 attempts")
	}

	// index.md must not gain an entry for the skipped note under any candidate slug.
	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertNotContains(t, indexContent, "[[foo-101]]")
}
