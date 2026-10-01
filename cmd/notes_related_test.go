package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures for TestProcessNotes_PreservesAutoLinkedRelatedOnNoteEdit.
// Notes carry two frontmatter tags so SuggestTags/Ollama is never called.
const relatedNoteV1 = `---
tags:
  - golang
  - testing
---

# Note

Version one.
`

const relatedNoteV2 = `---
tags:
  - golang
  - testing
---

# Note

Version two.
`

const relatedBookmarkWiki = `---
title: "Bookmark"
tags:
  - golang
  - testing
type: bookmark
---

# Bookmark

## Summary
Body.
`

// TestProcessNotes_PreservesAutoLinkedRelatedOnNoteEdit reproduces the bug
// described in docs/plans/plan-preserve-note-related-on-rebuild.md: editing a
// note whose wiki copy has an auto-linked ## Related section (written by
// autoLinkRelated when another page was later enriched) must not drop that
// section or its graph edge when processNotes rebuilds the wiki copy.
func TestProcessNotes_PreservesAutoLinkedRelatedOnNoteEdit(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	// A bookmark is enriched later and shares two tags with the note.
	bmContent := relatedBookmarkWiki
	if _, err := v.WriteWiki("my-bookmark.md", bmContent); err != nil {
		t.Fatalf("WriteWiki bookmark: %v", err)
	}
	syncEdgesFromContent(db, "my-bookmark", bmContent)
	if err := v.UpdateIndex("my-bookmark", "Bookmark", []string{"golang", "testing"}); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}
	autoLinkRelated(db, v, "my-bookmark", []string{"golang", "testing"}, bmContent)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	before := readFile(t, noteWiki)
	assertContains(t, before, "\n- [[my-bookmark]] — shared tags: golang, testing\n")
	assertNotContains(t, before, "- - ")

	// User edits the note.
	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Version two")
	assertContains(t, after, "\n- [[my-bookmark]] — shared tags: golang, testing\n")
	assertNotContains(t, after, "- - ")

	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	found := false
	for _, e := range out {
		if e.ToSlug == "my-bookmark" {
			found = true
		}
	}
	if !found {
		t.Errorf("edge my-note -> my-bookmark lost after note edit")
	}
}

// TestProcessNotes_DoesNotResurrectRemovedUserRelatedLine verifies that a
// user-authored link under the note's own ## Related section (plain text, no
// "— shared tags:" marker) is NOT carried over on rebuild: if the user
// removes it from the note body, it must be gone from the wiki copy too.
// Only auto-link-format lines are ever carried over from the old wiki copy.
func TestProcessNotes_DoesNotResurrectRemovedUserRelatedLine(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	noteV1 := `---
tags:
  - golang
  - testing
---

# Note

Body one.

## Related

- [[manual-link]]
`
	noteV2 := `---
tags:
  - golang
  - testing
---

# Note

Body one.
`

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(noteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	assertContains(t, readFile(t, noteWiki), "[[manual-link]]")

	// User removes the manual link from the note's own ## Related section.
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	assertNotContains(t, readFile(t, noteWiki), "[[manual-link]]")
}

// TestProcessNotes_MergesCarriedOverLinksWithUsersOwnRelatedSection verifies
// that when the note body has its own ## Related section AND the old wiki
// copy also has an auto-linked line, the rebuilt wiki copy ends up with
// exactly one ## Related section containing both links, with no duplicates.
func TestProcessNotes_MergesCarriedOverLinksWithUsersOwnRelatedSection(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	noteV1 := `---
tags:
  - golang
  - testing
---

# Note

Body one.

## Related

- [[x]]
`
	noteV2 := `---
tags:
  - golang
  - testing
---

# Note

Body two.

## Related

- [[x]]
`

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(noteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	// A bookmark is enriched later and shares two tags with the note,
	// auto-linking into the note's wiki copy's ## Related section.
	bmContent := relatedBookmarkWiki
	if _, err := v.WriteWiki("my-bookmark.md", bmContent); err != nil {
		t.Fatalf("WriteWiki bookmark: %v", err)
	}
	syncEdgesFromContent(db, "my-bookmark", bmContent)
	if err := v.UpdateIndex("my-bookmark", "Bookmark", []string{"golang", "testing"}); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}
	autoLinkRelated(db, v, "my-bookmark", []string{"golang", "testing"}, bmContent)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	before := readFile(t, noteWiki)
	assertContains(t, before, "[[x]]")
	assertContains(t, before, "\n- [[my-bookmark]] — shared tags: golang, testing\n")
	assertNotContains(t, before, "- - ")

	// User edits the note, keeping their own ## Related section intact.
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Body two")
	assertContains(t, after, "\n- [[my-bookmark]] — shared tags: golang, testing\n")
	assertNotContains(t, after, "- - ")
	if got := strings.Count(after, "## Related"); got != 1 {
		t.Errorf("expected exactly 1 ## Related section, got %d:\n%s", got, after)
	}
	if got := strings.Count(after, "[[x]]"); got != 1 {
		t.Errorf("expected exactly 1 occurrence of [[x]], got %d:\n%s", got, after)
	}
	if got := strings.Count(after, "[[my-bookmark]]"); got != 1 {
		t.Errorf("expected exactly 1 occurrence of [[my-bookmark]], got %d:\n%s", got, after)
	}

	// The rebuilt note's outbound edges must include both the user's manual
	// link target ("x") and the carried-over auto-link target ("my-bookmark").
	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	targets := make(map[string]bool, len(out))
	for _, e := range out {
		targets[e.ToSlug] = true
	}
	if !targets["x"] {
		t.Errorf("expected edge my-note -> x after merge, got edges %v", out)
	}
	if !targets["my-bookmark"] {
		t.Errorf("expected edge my-note -> my-bookmark after merge, got edges %v", out)
	}
}

// TestProcessNotes_RebuildSucceedsWhenOldWikiCopyMissing verifies that
// deleting the old wiki copy out from under a note before it is edited does
// not cause processNotes to error, and that no ## Related section is
// fabricated when there is nothing to carry over.
func TestProcessNotes_RebuildSucceedsWhenOldWikiCopyMissing(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	noteV1 := `---
tags:
  - golang
  - testing
---

# Note

Version one.
`
	noteV2 := `---
tags:
  - golang
  - testing
---

# Note

Version two.
`

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(noteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	if _, err := os.Stat(noteWiki); os.IsNotExist(err) {
		t.Fatal("expected wiki copy to exist before deletion")
	}

	// Simulate the old wiki copy being deleted from disk out of band.
	if err := os.Remove(noteWiki); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}

	// Must not panic or otherwise fail to rebuild just because the old wiki
	// copy is gone.
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Version two")
	assertNotContains(t, after, "## Related")
}

// TestProcessNotes_AutoLinkedRelatedStableAcrossRepeatedRebuilds verifies that
// once an auto-linked line has been carried over, it stays stable (no
// duplication, no "- - " corruption) across repeated edits of the note,
// including an edit that reverts the note back to a previously-seen content
// hash (v1 -> v2 -> v1).
func TestProcessNotes_AutoLinkedRelatedStableAcrossRepeatedRebuilds(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	bmContent := relatedBookmarkWiki
	if _, err := v.WriteWiki("my-bookmark.md", bmContent); err != nil {
		t.Fatalf("WriteWiki bookmark: %v", err)
	}
	syncEdgesFromContent(db, "my-bookmark", bmContent)
	if err := v.UpdateIndex("my-bookmark", "Bookmark", []string{"golang", "testing"}); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}
	autoLinkRelated(db, v, "my-bookmark", []string{"golang", "testing"}, bmContent)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	assertContains(t, readFile(t, noteWiki), "\n- [[my-bookmark]] — shared tags: golang, testing\n")

	assertStable := func(step string) {
		t.Helper()
		content := readFile(t, noteWiki)
		if got := strings.Count(content, "## Related"); got != 1 {
			t.Errorf("%s: expected exactly 1 ## Related section, got %d:\n%s", step, got, content)
		}
		if got := strings.Count(content, "[[my-bookmark]]"); got != 1 {
			t.Errorf("%s: expected exactly 1 occurrence of [[my-bookmark]], got %d:\n%s", step, got, content)
		}
		assertContains(t, content, "\n- [[my-bookmark]] — shared tags: golang, testing\n")
		assertNotContains(t, content, "- - ")
	}

	// v1 -> v2
	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)
	assertStable("after v1->v2")

	// v2 -> v1 (reverts to a previously-seen content hash)
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1 again: %v", err)
	}
	processNotes(ctx, en, db, v)
	assertStable("after v2->v1")

	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	found := false
	for _, e := range out {
		if e.ToSlug == "my-bookmark" {
			found = true
		}
	}
	if !found {
		t.Errorf("edge my-note -> my-bookmark lost after repeated rebuilds, got edges %v", out)
	}
}

// TestProcessNotes_SkipsCarriedOverDuplicateBySameSlugAsUserLink verifies the
// plan's "duplicates by slug are skipped" edge case: when the note's own
// (post-edit) ## Related section already contains a hand-written link for the
// SAME slug as a carried-over auto-link, the carried-over line must be
// skipped so the slug appears only once in the rebuilt file.
func TestProcessNotes_SkipsCarriedOverDuplicateBySameSlugAsUserLink(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	// A bookmark is enriched later, auto-linking into the note's wiki copy.
	bmContent := relatedBookmarkWiki
	if _, err := v.WriteWiki("my-bookmark.md", bmContent); err != nil {
		t.Fatalf("WriteWiki bookmark: %v", err)
	}
	syncEdgesFromContent(db, "my-bookmark", bmContent)
	if err := v.UpdateIndex("my-bookmark", "Bookmark", []string{"golang", "testing"}); err != nil {
		t.Fatalf("UpdateIndex: %v", err)
	}
	autoLinkRelated(db, v, "my-bookmark", []string{"golang", "testing"}, bmContent)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	assertContains(t, readFile(t, noteWiki), "[[my-bookmark]]")

	// User edits the note, hand-writing their OWN link for the same slug
	// ("my-bookmark") as the carried-over auto-link.
	noteV2 := `---
tags:
  - golang
  - testing
---

# Note

Body two.

## Related

- [[my-bookmark]]
`
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	if got := strings.Count(after, "## Related"); got != 1 {
		t.Errorf("expected exactly 1 ## Related section, got %d:\n%s", got, after)
	}
	if got := strings.Count(after, "[[my-bookmark]]"); got != 1 {
		t.Errorf("expected exactly 1 occurrence of [[my-bookmark]] (no duplicate), got %d:\n%s", got, after)
	}
	// The surviving line must be the user's own plain link, not the
	// carried-over auto-link-formatted line — the dedup must keep the user's
	// hand-written version, not silently swap in the auto-link text.
	assertContains(t, after, "- [[my-bookmark]]")
	assertNotContains(t, after, "— shared tags")

	// Sanity check only: the my-note -> my-bookmark edge comes from the
	// user's own "- [[my-bookmark]]" line in the note body via
	// syncEdgesFromContent, independent of the carry-over/dedup logic above.
	// This does not by itself prove dedup preserves an edge that depended
	// solely on the carried-over (now-skipped) auto-link text.
	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	found := false
	for _, e := range out {
		if e.ToSlug == "my-bookmark" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected edge my-note -> my-bookmark to survive dedup, got edges %v", out)
	}
}

// TestProcessNotes_NewNoteDoesNotCarryOverStaleWikiFile pins the new-note path
// (existingHash == "", i.e. first-time processing): if a wiki/<slug>.md file
// already exists on disk (e.g. left over from an unrelated process) with an
// auto-link-format ## Related line, processNotes must not inherit that stale
// content, because carry-over only reads the OLD wiki copy when the DB
// already has a record for the note (existingHash != "").
func TestProcessNotes_NewNoteDoesNotCarryOverStaleWikiFile(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	staleWiki := `---
title: "Stale"
tags:
  - golang
  - testing
---

# Stale

## Related

- [[leftover-bookmark]] — shared tags: golang, testing
`
	if _, err := v.WriteWiki("my-note.md", staleWiki); err != nil {
		t.Fatalf("WriteWiki stale: %v", err)
	}

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// No DB record exists yet for notes/my-note.md — this is the new-note path.
	processNotes(ctx, en, db, v)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	after := readFile(t, noteWiki)
	assertContains(t, after, "Version one")
	assertNotContains(t, after, "[[leftover-bookmark]]")
	assertNotContains(t, after, "## Related")
}

// TestProcessNotes_SkipsNoteWhenOldWikiCopyReadFails verifies that when
// reading the OLD wiki copy fails for a reason other than "not found" (e.g.
// the path unexpectedly resolves to a directory), processNotes logs the
// failure and skips the note — without panicking and without marking it
// processed — so the edit is retried on the next poll cycle instead of being
// silently dropped.
func TestProcessNotes_SkipsNoteWhenOldWikiCopyReadFails(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	const noteDBPath = "notes/my-note.md"
	hashBefore, err := db.GetNoteHash(noteDBPath)
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hashBefore == "" {
		t.Fatal("expected note to be recorded after first processNotes run")
	}

	// Replace the old wiki copy with a directory so reading it fails with a
	// non-not-found error.
	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	if err := os.Remove(noteWiki); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.Mkdir(noteWiki, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}

	var logBuf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	// Must not panic even though wiki/my-note.md is now a directory.
	processNotes(ctx, en, db, v)

	if !strings.Contains(logBuf.String(), "failed to write wiki copy for note") {
		t.Errorf("expected the read failure to be logged, got: %s", logBuf.String())
	}
	// Pin down the specific branch: the outer log message alone would also be
	// produced if WriteWiki itself failed on the same directory (since
	// wikiRelPath still points at a directory). Assert the wrapped error text
	// unique to the "reading old wiki copy" branch in cmd/main.go so this test
	// can't pass with that branch removed or swallowed.
	if !strings.Contains(logBuf.String(), "reading old wiki copy") {
		t.Errorf("expected the logged error to come from the \"reading old wiki copy\" branch, got: %s", logBuf.String())
	}

	hashAfter, err := db.GetNoteHash(noteDBPath)
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hashAfter != hashBefore {
		t.Errorf("expected note hash to remain %q (not marked processed) so it retries next cycle, got %q", hashBefore, hashAfter)
	}

	info, err := os.Stat(noteWiki)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected wiki/my-note.md to remain a directory after the failed rebuild")
	}
}

// TestProcessNotes_DropsCarriedOverLinkToDeletedPage verifies
// dropLinksToMissingPages: when the old wiki copy's ## Related section has two
// auto-linked lines, one pointing at a page that still exists and one
// pointing at a page that was deleted, only the surviving page's link is
// carried over into the rebuilt wiki copy (and gets a graph edge) — the
// broken link is dropped rather than resurrected every rebuild.
func TestProcessNotes_DropsCarriedOverLinkToDeletedPage(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	// Simulate two prior auto-links having landed in the old wiki copy: one to
	// a page that still exists ("kept") and one to a page that was since
	// deleted ("gone").
	oldWiki := `---
tags:
  - golang
  - testing
---

# Note

Version one.

## Related

- [[kept]] — shared tags: golang, testing
- [[gone]] — shared tags: golang, testing
`
	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	if err := os.WriteFile(noteWiki, []byte(oldWiki), 0o644); err != nil {
		t.Fatalf("WriteFile oldWiki: %v", err)
	}

	if _, err := v.WriteWiki("kept.md", "# Kept\n\nStill here.\n"); err != nil {
		t.Fatalf("WriteWiki kept: %v", err)
	}
	// wiki/gone.md is intentionally never created.

	// User edits the note so its hash changes and a rebuild is triggered.
	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Version two")
	assertContains(t, after, "[[kept]]")
	assertNotContains(t, after, "[[gone]]")

	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	for _, e := range out {
		if e.ToSlug == "gone" {
			t.Errorf("expected no outbound edge my-note -> gone, got edges %v", out)
		}
	}
}

// TestProcessNotes_DropsCarriedOverLinkToWhitespaceOnlyPage extends
// TestProcessNotes_DropsCarriedOverLinkToDeletedPage: a wiki page that exists
// on disk but is whitespace-only counts as missing (vault.FileHasContent
// treats whitespace-only content as absent), so its carried-over link must
// also be dropped.
func TestProcessNotes_DropsCarriedOverLinkToWhitespaceOnlyPage(t *testing.T) {
	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(relatedNoteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	oldWiki := `---
tags:
  - golang
  - testing
---

# Note

Version one.

## Related

- [[kept]] — shared tags: golang, testing
- [[gone]] — shared tags: golang, testing
`
	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	if err := os.WriteFile(noteWiki, []byte(oldWiki), 0o644); err != nil {
		t.Fatalf("WriteFile oldWiki: %v", err)
	}

	if _, err := v.WriteWiki("kept.md", "# Kept\n\nStill here.\n"); err != nil {
		t.Fatalf("WriteWiki kept: %v", err)
	}
	// wiki/gone.md exists but is whitespace-only, which FileHasContent treats
	// as missing.
	goneWiki := filepath.Join(tmpDir, "wiki", "gone.md")
	if err := os.WriteFile(goneWiki, []byte("   \n\t\n"), 0o644); err != nil {
		t.Fatalf("WriteFile gone: %v", err)
	}

	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "[[kept]]")
	assertNotContains(t, after, "[[gone]]")

	out, err := db.GetOutboundEdges("my-note")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	for _, e := range out {
		if e.ToSlug == "gone" {
			t.Errorf("expected no outbound edge my-note -> gone, got edges %v", out)
		}
	}
}

// TestProcessNotes_KnownLimitation_HandWrittenAutoLinkFormatSurvivesDeletion
// pins down an observed limitation of the carry-over heuristic in
// vault.AutoLinkedRelatedLines: it cannot distinguish a genuine auto-link
// (written by autoLinkRelated) from a user-authored line that merely matches
// the same format ("- [[slug]] — shared tags: ..."). If a user hand-writes
// such a line inside their own ## Related section and later deletes it, the
// OLD wiki copy (read during the next rebuild, before it is overwritten)
// still contains the line, so processNotes "carries it over" into the new
// wiki copy — resurrecting text the user explicitly removed from the note
// body.
//
// docs/plans/plan-preserve-note-related-on-rebuild.md only says user-removed
// links "must not be resurrected" in general and does not explicitly carve
// out this hand-written-format-collision case. The test asserts the DESIRED
// behavior (deletion sticks) and is skipped until the carry-over heuristic
// can distinguish provenance (genuine auto-link vs. hand-written lookalike)
// and this is tracked as a fixable issue, so it doesn't lock in the bug.
func TestProcessNotes_KnownLimitation_HandWrittenAutoLinkFormatSurvivesDeletion(t *testing.T) {
	t.Skip("known limitation: AutoLinkedRelatedLines carries over by text format, not provenance, so a deleted hand-written lookalike line is resurrected; needs a tracked issue before fixing")

	tmpDir, db, v, _, en := setupTestEnv(t)
	ctx := context.Background()

	noteV1 := `---
tags:
  - golang
  - testing
---

# Note

Body one.

## Related

- [[my-bookmark]] — shared tags: golang, testing
`
	noteV2 := `---
tags:
  - golang
  - testing
---

# Note

Body one.
`

	notePath := filepath.Join(tmpDir, "notes", "my-note.md")
	if err := os.WriteFile(notePath, []byte(noteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	processNotes(ctx, en, db, v)

	noteWiki := filepath.Join(tmpDir, "wiki", "my-note.md")
	assertContains(t, readFile(t, noteWiki), "[[my-bookmark]] — shared tags: golang, testing")

	// User deletes the hand-written line from the note body.
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	// Desired behavior: a user-deleted line must not reappear, even if it
	// happens to match the auto-link format.
	assertNotContains(t, readFile(t, noteWiki), "[[my-bookmark]] — shared tags: golang, testing")
}
