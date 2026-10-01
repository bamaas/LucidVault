package main

import (
	"context"
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
	assertContains(t, readFile(t, noteWiki), "[[my-bookmark]]")

	// User edits the note.
	if err := os.WriteFile(notePath, []byte(relatedNoteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Version two")
	assertContains(t, after, "[[my-bookmark]]")

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
	assertContains(t, before, "[[my-bookmark]]")

	// User edits the note, keeping their own ## Related section intact.
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	processNotes(ctx, en, db, v)

	after := readFile(t, noteWiki)
	assertContains(t, after, "Body two")
	if got := strings.Count(after, "## Related"); got != 1 {
		t.Errorf("expected exactly 1 ## Related section, got %d:\n%s", got, after)
	}
	if got := strings.Count(after, "[[x]]"); got != 1 {
		t.Errorf("expected exactly 1 occurrence of [[x]], got %d:\n%s", got, after)
	}
	if got := strings.Count(after, "[[my-bookmark]]"); got != 1 {
		t.Errorf("expected exactly 1 occurrence of [[my-bookmark]], got %d:\n%s", got, after)
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
