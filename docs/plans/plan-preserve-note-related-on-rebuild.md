# Plan: Preserve auto-linked `## Related` when a note's wiki copy is rebuilt

> Bug fix. No new ADR: ADR-014 (notes get a wiki copy) and ADR-020 (auto-linking at
> enrichment) stay as they are — this makes the rebuild honour links ADR-020 already
> wrote. Related follow-ups filed separately: #95 (note slug collisions), #96
> (`--force-re-enrich` drops `## Related` on bookmark pages), #97 (`add_note`
> overwrite).

## Problem

Every note in `notes/` gets a wiki copy `wiki/<slug>.md` (ADR-014). When another page
is enriched later and shares 2+ tags with the note, `autoLinkRelated` appends a line
like `- [[my-bookmark]] — shared tags: golang, testing` under `## Related` in the
note's wiki copy.

When the user then edits the note, its content hash changes and `processNotes`
(`cmd/main.go`) rebuilds the wiki copy from frontmatter + note body only
(`buildNoteWikiContent`) and overwrites it with `v.WriteWiki`. Result:

- the auto-linked `## Related` lines are gone from the note's wiki copy;
- `syncEdgesFromContent` replaces all outbound edges of the note slug, so the
  `note → bookmark` edge is gone too;
- nothing restores them: `autoLinkRelated(noteSlug)` only writes into *other* pages,
  the bookmark is never reprocessed, and hygiene / `rebuildAllEdges` only re-derive
  from the (now link-less) file.

The source note in `notes/` is never affected — only links that existed solely in the
generated wiki copy are lost.

Confirmed by a temporary test (see **Failing test** below): before the edit the note's
wiki copy ends with the `## Related` line and outbound edges are
`[{my-note → my-bookmark}]`; after `processNotes` on the edited note the section is
gone and outbound edges are `[]`.

## Proposed Solution

When `processNotes` rebuilds an **existing** note wiki copy, carry over the
auto-linked lines from the old file's `## Related` section into the new file, then
derive edges from the final file content.

### Which lines to carry over

Only lines in the old file's `## Related` section that `autoLinkRelated` itself
writes — i.e. list items of the form produced by `BacklinkCandidate.BacklinkLine`
prefixed with `- `:

```text
- [[<slug>]] — shared tags: <tag>, <tag>
```

User-authored lines are not carried over: they live in the note body (the source of
truth) and are re-copied on every rebuild. If the user removes a link from their own
`## Related` section in the note, it must disappear from the wiki copy; carrying over
only auto-link lines guarantees that.

### Where the lines go

Reuse `vault.UpdateRelatedSection` after writing the new content. It already:
- appends to an existing `## Related` section (e.g. one the user wrote in the note
  body) and skips links whose slug is already present;
- otherwise creates `## Related` at the end of the file (note copies have no
  `*Source:` footer).

`UpdateRelatedSection` takes link text without the `- ` prefix (it adds it), so pass
the carried-over lines with the leading `- ` stripped.

### Order of operations in `processNotes` (existing note, `existingHash != ""`)

1. Read the old wiki copy (path from the DB record / `wiki/<slug>.md`); if missing,
   carry over nothing (not an error — e.g. the user deleted it).
2. Extract auto-link lines from its `## Related` section.
3. `v.WriteWiki` the freshly built content (unchanged behaviour).
4. If any lines were extracted: `v.UpdateRelatedSection(wikiPath, lines)`.
5. Re-read the final file and pass **that** content to `syncEdgesFromContent` and
   `autoLinkRelated` (today both receive the pre-write `wikiContent`).

Steps 1–4 run inside `db.WithFileLock` so an MCP process (ADR-019) cannot append a
Related line between the read and the write — consistent with how `autoLinkRelated`
already locks `UpdateRelatedSection`.

New notes (`existingHash == ""`) are unchanged: there is nothing to carry over.

### Helper placement

Add the extraction as a small exported function in `internal/vault/autolink.go`
next to `UpdateRelatedSection` (it owns the `## Related` format), e.g.
`AutoLinkedRelatedLines(content string) []string`. It reuses the existing section
detection (`## Related` heading, section end via `findRelatedSectionEnd`) and the
`— shared tags:` marker from `BacklinkLine`. Keep `processNotes` changes minimal.

## Edge cases

- **Old wiki copy missing or empty** → nothing carried over; rebuild as today.
- **Note body has its own `## Related`** → carried-over lines are appended into that
  section; duplicates by slug are skipped by `UpdateRelatedSection`.
- **User removed a link from the note's own `## Related`** → not resurrected (only
  auto-link-format lines are carried over).
- **Carried-over link points to a page that was deleted** → line is kept, as on any
  other wiki page today; hygiene's broken-edge cleanup handles the edge. Out of scope
  to prune here.
- **Note deleted** → existing reconcile deletes the wiki copy; unaffected.
- **Bookmark re-enrich** (`--force-re-enrich`) has the same defect class → out of
  scope, tracked in #96. The helper should be reusable there.

## Failing test

Add to `cmd/` (reuses `setupTestEnv`, `readFile`, `assertContains` from existing
`cmd/` tests; notes carry two frontmatter tags so `SuggestTags`/Ollama is never
called). This test fails on `main` today:

```go
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
```

Additional tests (spec-level):

- `processNotes`: user removes a line from the note's own `## Related` section → it
  is gone from the wiki copy after rebuild (no resurrection of user lines).
- `processNotes`: note body has its own `## Related` with `[[x]]`, old wiki copy has
  auto-link `[[my-bookmark]]` → rebuilt copy has one `## Related` section containing
  both, no duplicates.
- `processNotes`: old wiki copy deleted from disk, note edited → rebuild succeeds,
  no `## Related` added.
- `vault` helper unit tests: extracts only `— shared tags:` lines from `## Related`;
  ignores other sections, lines outside the section, and user-written lines; returns
  nil when there is no section.

## Acceptance Criteria

- [ ] `TestProcessNotes_PreservesAutoLinkedRelatedOnNoteEdit` passes (fails on `main`)
- [ ] Only auto-link-format lines are carried over; user lines follow the note body
- [ ] Note outbound edges are derived from the final file content (incl. carried lines)
- [ ] Read-old / write / update-Related happens under `db.WithFileLock`
- [ ] New-note path unchanged
- [ ] `mise run test` and `mise run lint` pass
- [ ] No docs change needed beyond this plan (no env var, pipeline step or interface change)
