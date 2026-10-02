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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/notes"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// noteContent builds a minimal note with one frontmatter tag (so SuggestTags /
// Ollama is never called) and a body containing the given marker text, mirroring
// the fixture style used throughout cmd/main_test.go and cmd/notes_related_test.go.
func noteContent(h1, tag, body string) string {
	return fmt.Sprintf("---\ntags:\n  - %s\n---\n\n# %s\n\n%s\n", tag, h1, body)
}

// realNoteHash computes the hex-encoded SHA-256 of content the same way
// notes.Scan does, so tests that seed a note's DB record directly (via
// db.UpsertNote) can seed a hash that genuinely matches the file on disk,
// instead of a hash that can never match and masks what's under test.
func realNoteHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
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

	recA := findNoteRecord(t, db, "notes/a/foo.md")
	recBBefore := findNoteRecord(t, db, "notes/b/foo.md")
	if recBBefore.WikiPath == "" {
		t.Fatal("expected notes/b/foo.md to have a resolved wiki_path after the first cycle")
	}
	if recBBefore.WikiPath == "wiki/foo.md" {
		t.Fatalf("expected notes/b/foo.md to be suffixed off the bookmark's slug, got %q", recBBefore.WikiPath)
	}
	bFile := filepath.Join(tmpDir, recBBefore.WikiPath)
	assertContains(t, readFile(t, bFile), "version one")

	aFile := filepath.Join(tmpDir, recA.WikiPath)
	aContentBefore := readFile(t, aFile)
	bookmarkFile := filepath.Join(tmpDir, "wiki", "foo.md")
	bookmarkContentBefore := readFile(t, bookmarkFile)

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

	// Only note B's own page changed — note A's page and the bookmark's page
	// must be byte-unchanged by B's edit+reprocess.
	aContentAfter := readFile(t, aFile)
	if aContentAfter != aContentBefore {
		t.Errorf("expected note A's wiki page to be untouched by editing note B\nbefore:\n%s\nafter:\n%s", aContentBefore, aContentAfter)
	}
	bookmarkContentAfter := readFile(t, bookmarkFile)
	if bookmarkContentAfter != bookmarkContentBefore {
		t.Errorf("expected the bookmark's wiki page to be untouched by editing note B\nbefore:\n%s\nafter:\n%s", bookmarkContentBefore, bookmarkContentAfter)
	}

	// index.md must contain exactly one [[<bSlug>]] entry — no duplicate from
	// the remove-then-re-add index dance on reprocessing.
	bSlug := strings.TrimSuffix(filepath.Base(recBAfter.WikiPath), ".md")
	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	count := strings.Count(indexContent, "[["+bSlug+"]]")
	if count != 1 {
		t.Errorf("expected exactly one [[%s]] entry in index.md, got %d\nindex:\n%s", bSlug, count, indexContent)
	}
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

	// The bookmark's own [[foo]] index entry survives too — this is the
	// exact entry the old filename-based deletion logic used to wrongly
	// remove in #95.
	assertContains(t, indexAfter, "[[foo]]")
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

	contentA := noteContent("Note A", "alpha", "Legacy content A.")
	contentB := noteContent("Note B", "beta", "Legacy content B.")
	writeNoteFile(t, tmpDir, "notes/a/foo.md", contentA)
	writeNoteFile(t, tmpDir, "notes/b/foo.md", contentB)

	// Seed the legacy pre-fix DB state: both note records already point at the
	// same wiki_path, as the old unconditional-write code would have left them
	// after the second note silently overwrote the first's page. Seed each
	// record's REAL content hash (computed the same way notes.Scan does) so
	// the repair path is genuinely exercised: without this, both notes would
	// be fully reprocessed anyway because of a hash mismatch unrelated to the
	// repair, and the test couldn't tell whether repair actually works.
	if err := db.UpsertNote("notes/a/foo.md", realNoteHash(contentA), "wiki/foo.md"); err != nil {
		t.Fatalf("seeding legacy note record a: %v", err)
	}
	if err := db.UpsertNote("notes/b/foo.md", realNoteHash(contentB), "wiki/foo.md"); err != nil {
		t.Fatalf("seeding legacy note record b: %v", err)
	}
	// And the wiki page on disk as the old code left it: whichever note was
	// processed last fully overwrote the file. Neither note's real hash
	// matches this content — it's stale "last writer wins" legacy content.
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

	// Both must now have real, distinct pages carrying their own content —
	// never the other note's stale legacy content, and never the orphaned
	// "last writer wins" placeholder.
	aContent := readFile(t, filepath.Join(tmpDir, recA.WikiPath))
	bContent := readFile(t, filepath.Join(tmpDir, recB.WikiPath))
	assertContains(t, aContent, "Legacy content A.")
	assertContains(t, bContent, "Legacy content B.")
	assertNotContains(t, aContent, "Legacy content B.")
	assertNotContains(t, bContent, "Legacy content A.")
	assertNotContains(t, aContent, "legacy shared content, last writer wins")
	assertNotContains(t, bContent, "legacy shared content, last writer wins")

	// Whichever note landed on the bare "foo" slug must hold ITS OWN
	// content, not the other note's. This is the exact bug the production
	// fix corrects: the old "keep first, clear the rest" repair left the
	// kept record's hash (and therefore its stale page) untouched, so it was
	// never reprocessed and could permanently show the other note's content.
	fooContent := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))
	switch {
	case recA.WikiPath == "wiki/foo.md":
		assertContains(t, fooContent, "Legacy content A.")
		assertNotContains(t, fooContent, "Legacy content B.")
	case recB.WikiPath == "wiki/foo.md":
		assertContains(t, fooContent, "Legacy content B.")
		assertNotContains(t, fooContent, "Legacy content A.")
	default:
		t.Fatalf("expected one of the two notes to resolve to wiki/foo.md, got a=%q b=%q", recA.WikiPath, recB.WikiPath)
	}
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

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	processNotes(ctx, en, db, v)

	// The skip must be logged as an error, not silently swallowed.
	if !strings.Contains(logBuf.String(), "failed to write wiki copy for note") {
		t.Errorf("expected the cap-exceeded failure to be logged, got: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "no free wiki slug") {
		t.Errorf("expected the logged error to name the exhausted-candidates condition, got: %s", logBuf.String())
	}

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

// TestProcessNotes_SlugCollision_NinetyNineCandidatesTaken is the boundary
// case for TestProcessNotes_SlugCollision_AllHundredCandidatesTaken: slots
// 1-99 are taken, leaving exactly one free slot. The colliding note must land
// on it (wiki/foo-100.md) rather than being skipped — the cap-exceeded
// assertion in the all-100-taken test above can't tell "cap reached" apart
// from "off-by-one in the loop bound" without this case to anchor the other
// side of the boundary.
func TestProcessNotes_SlugCollision_NinetyNineCandidatesTaken(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	wikiDir := filepath.Join(tmpDir, "wiki")
	for n := 1; n <= 99; n++ {
		filename := "foo.md"
		if n > 1 {
			filename = fmt.Sprintf("foo-%d.md", n)
		}
		content := fmt.Sprintf("pre-existing content for %s\n", filename)
		if err := os.WriteFile(filepath.Join(wikiDir, filename), []byte(content), 0o644); err != nil {
			t.Fatalf("pre-creating %s: %v", filename, err)
		}
	}

	writeNoteFile(t, tmpDir, "notes/foo.md", noteContent("Foo", "delta", "Lands on the last free slot."))

	processNotes(ctx, en, db, v)

	rec := findNoteRecord(t, db, "notes/foo.md")
	if rec.WikiPath != "wiki/foo-100.md" {
		t.Errorf("expected notes/foo.md to land on the last free slot wiki/foo-100.md, got %q", rec.WikiPath)
	}

	content := readFile(t, filepath.Join(wikiDir, "foo-100.md"))
	assertContains(t, content, "Lands on the last free slot.")

	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertContains(t, indexContent, "[[foo-100]]")
}

// newResolveTestEnv builds a bare store+vault pair for unit-testing
// resolveNoteWikiSlug directly, without the HTTP-backed fixtures
// setupTestEnv provides for full processNotes integration tests.
func newResolveTestEnv(t *testing.T) (*store.Store, *vault.Vault) {
	t.Helper()
	tmpDir := t.TempDir()

	v := vault.New(tmpDir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	db, err := store.New(filepath.Join(tmpDir, ".lucidvault.db"))
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db, v
}

// TestResolveNoteWikiSlug table-drives the ADR-029 slug resolution rules
// directly against resolveNoteWikiSlug (cmd/main.go), covering cases the
// full-pipeline processNotes tests above don't isolate cleanly: reuse vs.
// fresh resolution, an empty stored wiki_path, whitespace-only files counting
// as free, and both edges of the 100-candidate cap.
func TestResolveNoteWikiSlug(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, db *store.Store, v *vault.Vault)
		rec       store.NoteRecord
		recExists bool
		notePath  string
		wantSlug  string
		wantErr   bool
	}{
		{
			name: "reuses the stored wiki_path when no other record shares it",
			setup: func(t *testing.T, db *store.Store, _ *vault.Vault) {
				t.Helper()
				if err := db.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo-5.md"); err != nil {
					t.Fatalf("UpsertNote: %v", err)
				}
			},
			rec:       store.NoteRecord{Path: "notes/a/foo.md", WikiPath: "wiki/foo-5.md"},
			recExists: true,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo-5",
		},
		{
			name: "resolves fresh when the stored wiki_path is shared with another note",
			setup: func(t *testing.T, db *store.Store, v *vault.Vault) {
				t.Helper()
				if err := db.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
					t.Fatalf("UpsertNote a: %v", err)
				}
				if err := db.UpsertNote("notes/b/foo.md", "hash-b", "wiki/foo.md"); err != nil {
					t.Fatalf("UpsertNote b: %v", err)
				}
				if _, err := v.WriteWiki("foo.md", "taken\n"); err != nil {
					t.Fatalf("WriteWiki: %v", err)
				}
			},
			rec:       store.NoteRecord{Path: "notes/a/foo.md", WikiPath: "wiki/foo.md"},
			recExists: true,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo-2",
		},
		{
			name: "a record with an empty stored wiki_path resolves fresh",
			setup: func(t *testing.T, db *store.Store, _ *vault.Vault) {
				t.Helper()
				if err := db.UpsertNote("notes/a/foo.md", "hash-a", ""); err != nil {
					t.Fatalf("UpsertNote: %v", err)
				}
			},
			rec:       store.NoteRecord{Path: "notes/a/foo.md", WikiPath: ""},
			recExists: true,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo",
		},
		{
			name:      "no existing record resolves fresh",
			rec:       store.NoteRecord{},
			recExists: false,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo",
		},
		{
			name: "a whitespace-only existing file counts as free",
			setup: func(t *testing.T, _ *store.Store, v *vault.Vault) {
				t.Helper()
				if _, err := v.WriteWiki("foo.md", "   \n\t\n"); err != nil {
					t.Fatalf("WriteWiki: %v", err)
				}
			},
			rec:       store.NoteRecord{},
			recExists: false,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo",
		},
		{
			name: "99 candidates taken resolves to the 100th",
			setup: func(t *testing.T, _ *store.Store, v *vault.Vault) {
				t.Helper()
				for n := 1; n <= 99; n++ {
					filename := "foo.md"
					if n > 1 {
						filename = fmt.Sprintf("foo-%d.md", n)
					}
					if _, err := v.WriteWiki(filename, "taken\n"); err != nil {
						t.Fatalf("WriteWiki %s: %v", filename, err)
					}
				}
			},
			rec:       store.NoteRecord{},
			recExists: false,
			notePath:  "notes/a/foo.md",
			wantSlug:  "foo-100",
		},
		{
			name: "all 100 candidates taken returns an error",
			setup: func(t *testing.T, _ *store.Store, v *vault.Vault) {
				t.Helper()
				for n := 1; n <= 100; n++ {
					filename := "foo.md"
					if n > 1 {
						filename = fmt.Sprintf("foo-%d.md", n)
					}
					if _, err := v.WriteWiki(filename, "taken\n"); err != nil {
						t.Fatalf("WriteWiki %s: %v", filename, err)
					}
				}
			},
			rec:       store.NoteRecord{},
			recExists: false,
			notePath:  "notes/a/foo.md",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, v := newResolveTestEnv(t)
			if tt.setup != nil {
				tt.setup(t, db, v)
			}

			nf := notes.NoteFile{Path: tt.notePath}
			slug, err := resolveNoteWikiSlug(db, v, tt.rec, tt.recExists, nf)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got slug %q", slug)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveNoteWikiSlug: %v", err)
			}
			if slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}
		})
	}
}

// TestProcessNotes_DeleteNoteWithSharedWikiPath covers the shared==true
// branch of the deletion reconcile (cmd/main.go, the NoteWikiPathShared check
// in the "reconcile deletions" section of processNotes): when a deleted
// note's DB record's wiki_path is still claimed by another, live note's
// record, only the deleted note's DB record may be dropped — the wiki file
// and its index entry belong to the surviving note and must be left
// untouched.
//
// This seeds a legacy duplicate record for a note ("gone") whose file never
// existed on disk this cycle — i.e. already deleted — while "keep" is a
// genuinely new note that, in the very same processNotes cycle, resolves
// fresh and independently onto the exact slug "gone"'s stale record still
// claims. This is the realistic shape of the race the shared check guards
// against (see the ADR-029 "vault not yet reprocessed since the one-time
// duplicate repair" case): a leftover duplicate pointer from before a note
// was deleted must never cause the deletion reconcile to tear down a page
// another, still-live note has since (re)claimed. Deliberately not
// pre-written: once a real file already sits at wiki/foo.md, any note
// sharing that slug in the DB is forced through fresh resolution by the
// one-time repair and finds the slot "taken" by its own old content, so it
// gets suffixed away — which would defeat the scenario rather than exercise
// the branch under test.
func TestProcessNotes_DeleteNoteWithSharedWikiPath(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	keepContent := noteContent("Keep", "golang", "Keep this note's content.")
	writeNoteFile(t, tmpDir, "notes/keep/foo.md", keepContent)

	if err := db.UpsertNote("notes/gone/foo.md", "stale-gone-hash", "wiki/foo.md"); err != nil {
		t.Fatalf("seeding stale gone record: %v", err)
	}

	processNotes(ctx, en, db, v)

	// "keep" is the sole claimant of wiki/foo.md: it must have resolved
	// normally onto the bare slug, not been pushed off it, and not been
	// swept up by gone's deletion.
	recKeep := findNoteRecord(t, db, "notes/keep/foo.md")
	if recKeep.WikiPath != "wiki/foo.md" {
		t.Fatalf("expected notes/keep/foo.md to resolve to wiki/foo.md, got %q", recKeep.WikiPath)
	}
	wikiContent := readFile(t, filepath.Join(tmpDir, "wiki", "foo.md"))
	assertContains(t, wikiContent, "Keep this note's content.")

	indexContent := readFile(t, filepath.Join(tmpDir, "index.md"))
	assertContains(t, indexContent, "[[foo]]")

	// gone's stale DB record is removed...
	if noteRecordExists(t, db, "notes/gone/foo.md") {
		t.Error("expected the DB record for notes/gone/foo.md to be removed")
	}
	// ...but keep's page, index entry, and DB record all survive it,
	// because the shared check protected them from gone's cleanup branch.
	if !noteRecordExists(t, db, "notes/keep/foo.md") {
		t.Error("expected the DB record for notes/keep/foo.md to survive")
	}
}
