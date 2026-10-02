# Plan: Note Wiki Slugs Never Collide

> Fixes issue #95. Decision: `docs/adr/029-note-wiki-slug-claim-with-suffix.md`
> (status Proposed — accept before implementing).

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
   record shares it, reuse it (slug = basename of `wiki_path` without `.md`).
2. Otherwise start from `notes.TitleFromFilename(nf.Path)` and take the first
   candidate `<slug>`, `<slug>-2` … `<slug>-100` where `wiki/<candidate>.md` does not
   exist. Cap reached → log an error naming the slug and skip the note; nothing is
   overwritten.

The existence check and the write happen inside the existing `db.WithFileLock`
block, so a concurrent MCP writer (ADR-019) cannot claim the same file in between.

## Implementation Plan (TDD)

Failing tests first (spec-only subagent), then minimal implementation.

### 1. Store — `internal/store/sqlite.go`

- `GetNote(path) (NoteRecord, bool, error)` — returns the record including
  `WikiPath`. Replaces the `GetNoteHash` call in `processNotes` (one query instead of
  two).
- `NoteWikiPathShared(path, wikiPath string) (bool, error)` — true when another note
  record has the same `wiki_path`. Used by rule 1 and by the deletion reconcile.

### 2. Slug resolution — `cmd/main.go`

- New helper `resolveNoteWikiSlug(db, v, rec, nf) (string, error)` implementing the
  two rules above. Called inside `WithFileLock`, before `WriteWiki`.
- Old-index removal and `## Related` carry-over read from the **resolved** slug's
  path, not the recomputed one.

### 3. Deletion reconcile — `cmd/main.go`

- Derive the index slug from `rec.WikiPath`, not `TitleFromFilename(rec.Path)`.
- Delete the wiki file and the index entry only when `NoteWikiPathShared` is false.
  When shared, drop only the DB record — the page belongs to the other note.

### 4. One-time repair for vaults already hit by #95

- At the start of `processNotes`: for each `wiki_path` held by more than one note
  record, keep the record whose `path` sorts first and clear the content hash of the
  rest (`UPDATE notes SET content_hash = '' …`). The next pass of the same cycle
  reprocesses them; rule 1 sends them to rule 2 because their path is shared.
- Idempotent: once paths are unique the query returns nothing.

### 5. Docs

- ADR-029 → Accepted.
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
  a slug fresh. Unchanged behaviour.
- Bookmark created later with a note's slug: still overwrites (bookmark writer is out of
  scope, see ADR-029). File a follow-up issue.
- A user hand-deletes `wiki/foo-2.md`: next content change re-claims via rule 1
  (`wiki_path` still recorded, file just gets recreated).

## Acceptance

- `mise run test` and `mise run lint` green.
- The #95 repro test passes; no existing test weakened.
