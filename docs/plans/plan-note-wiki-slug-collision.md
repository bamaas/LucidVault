# Plan: Note Wiki Slugs Never Collide

> Fixes issue #95. Decision: `docs/adr/029-note-wiki-slug-claim-with-suffix.md`
> (Accepted).

## Problem

`processNotes` (`cmd/main.go`) derives `wikiSlug := notes.TitleFromFilename(nf.Path)`
on every cycle and writes `wiki/<slug>.md` with `v.WriteWiki`, an unconditional
`os.WriteFile`. Same-basename notes in different subfolders, or a note named like a
bookmark page, overwrite each other. The deletion reconcile then derives the same slug
and deletes the shared page, taking the surviving note's copy with it.

## Proposed Solution

Assign the slug once, store it, reuse it.

```text
notes/a/foo.md   → wiki/foo.md      (first seen, wiki/foo.md free)
notes/b/foo.md   → wiki/foo-2.md    (wiki/foo.md taken by note a)
notes/bar.md     → wiki/bar-2.md    (wiki/bar.md is a bookmark page)
```

Slug resolution for a note `nf`:

1. If the DB record for `nf.Path` has a non-empty `wiki_path` **and** no other note
   record whose source file is still present in the current scan shares it, reuse
   it (slug = basename of `wiki_path` without `.md`).
2. Otherwise start from `notes.TitleFromFilename(nf.Path)` and take the first
   candidate `<slug>`, `<slug>-2` … `<slug>-100` where `wiki/<candidate>.md` has no
   content **and** no other note record already claims that candidate's `wiki_path`
   (`NotesSharingWikiPath`) — this second check catches a hand-deleted wiki page
   whose owning note record hasn't been reprocessed yet. Cap reached → log an error
   naming the slug and skip the note; nothing is overwritten.

The existence check and the write happen inside the existing `db.WithFileLock`
block, so a concurrent MCP writer (ADR-019) cannot claim the same file in between.

## Implementation Plan (TDD)

Failing tests first (spec-only subagent), then minimal implementation.

### 1. Store — `internal/store/sqlite.go`

- `GetNote(path) (NoteRecord, bool, error)` — returns the record including
  `WikiPath`. Replaces the `GetNoteHash` call in `processNotes` (one query instead of
  two).
- `NoteWikiPathShared(path, wikiPath string) (bool, error)` — true when another note
  record has the same `wiki_path`. Used by the deletion reconcile.
- `NotesSharingWikiPath(path, wikiPath string) ([]string, error)` — the paths of
  other note records sharing `wikiPath`. Used by rule 1, which only counts a sharer
  as a real collision if its path is still in the current scan (`scannedPaths`) —
  a leftover record from a deleted note doesn't count (round-1 review fix). Rule 2
  uses it too, for each candidate slug in turn, to catch a hand-deleted wiki page
  whose owning note record hasn't been reprocessed yet; unlike rule 1 it doesn't
  need the `scannedPaths` filter, because the deletion reconcile (section 3) already
  runs before this loop and removes any record whose own source file is no longer
  scanned.

### 2. Slug resolution — `cmd/main.go`

- New helper `resolveNoteWikiSlug(db, v, rec, recExists, nf, scannedPaths) (string, error)`
  implementing the two rules above. Called inside `WithFileLock`, before `WriteWiki`.
- Old-index removal and `## Related` carry-over read from the **resolved** slug's
  path, not the recomputed one.
- Right after `WithFileLock` returns successfully (i.e. once the wiki page has been
  written), persist the resolved `wiki_path` via a dedicated
  `db.UpsertNote(nf.Path, existingHash, wikiPath)` call — before the edge-sync,
  auto-link and index side effects run. Without this early claim, a failure in one
  of those side effects would leave the just-written wiki page orphaned with no DB
  record, and the next cycle's rule 2 would see `wiki/<slug>.md` already has
  content and permanently suffix the note onto `<slug>-2` instead of letting it
  reclaim `<slug>` on retry. The early claim persists `existingHash` (never `""`
  for an existing note reaching this point, since the unchanged-content skip check
  already returned otherwise), not `""` — so a retry's own carry-over check still
  reads the old wiki copy's auto-linked `## Related` lines forward instead of
  treating the retry as a brand-new note.

### 3. Deletion reconcile — `cmd/main.go`

- Derive the index slug from `rec.WikiPath`, not `TitleFromFilename(rec.Path)`.
- Delete the wiki file and the index entry only when `NoteWikiPathShared` is false.
  When shared, drop only the DB record — the page belongs to the other note.

### 4. One-time repair for vaults already hit by #95

- At the start of `processNotes`: for each `wiki_path` held by more than one note
  record, clear the content hash of every record in that duplicate group in one
  atomic statement, not just all-but-one. The next pass of the same cycle
  reprocesses all of them: each one except the last one scanned is suffixed, and
  the last one keeps the bare slug.
- A live note's own stored `wiki_path` is never pushed off it by a leftover
  record belonging to a note no longer on disk — rule 1's shared check only
  counts a sharer still present in the current scan (`scannedPaths`).
- Idempotent: once paths are unique the query returns nothing.

### 5. Docs

- ADR-029 already Accepted; no change.
- `CONTEXT.md`: define "wiki slug (note)" as assigned once, stored in the notes record.
- `README.md`: one line under notes — "same-named notes get `-2`, `-3` wiki pages".

## Tests

| Case | Expect |
|---|---|
| #95 repro: bookmark `wiki/foo.md` + `notes/a/foo.md` + `notes/b/foo.md` | three files; `foo.md` still `type: bookmark`; notes at `foo-2.md`, `foo-3.md` |
| Edit `notes/b/foo.md` after first cycle | rewrites `foo-3.md` only (slug stable) |
| Delete `notes/a/foo.md` | `foo-2.md` and its index entry gone; `foo-3.md` and bookmark untouched |
| Pre-existing duplicate DB records (simulated legacy state) | after one cycle, distinct `wiki_path`s |
| Unique top-level note | slug unchanged from today (`notes/bar.md` → `bar`) |
| 100 candidates taken | note skipped, no file overwritten, error logged |

## Edge Cases

- Note renamed/moved: it's a delete + add under the current model; the new path claims
  a slug fresh. `processNotes` runs the deletion reconcile BEFORE the new/changed-note
  loop (review round 2 fix) specifically so this works correctly: the old path's DB
  record and `wiki/<slug>.md` page are freed first, so the new path's fresh rule-2
  resolution finds the bare slug free and reclaims it instead of being pushed onto
  `<slug>-2`. Running the loop first (the original, buggy order) would resolve the new
  path while the old page was still live, permanently and cumulatively suffixing the
  moved note and orphaning every existing `[[<slug>]]` link elsewhere in the vault.
- Bookmark created later with a note's slug: still overwrites (bookmark writer is out of
  scope, see ADR-029). File a follow-up issue.
- A user hand-deletes `wiki/foo-2.md`: next content change re-claims via rule 1
  (`wiki_path` still recorded, file just gets recreated).

## Acceptance

- `mise run test` and `mise run lint` green.
- The #95 repro test passes; no existing test weakened.
