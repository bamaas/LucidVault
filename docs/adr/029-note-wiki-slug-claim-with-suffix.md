# 029 — Note Wiki Slugs Are Claimed Once, Suffixed on Collision (amends ADR-014 notes auto-tagging)

## Status

Accepted (amends ADR-014 "Auto-tag notes via wiki copies")

## Context

A note's wiki slug is recomputed on every cycle from its basename (`notes.TitleFromFilename`), and `vault.WriteWiki` truncates whatever is at `wiki/<slug>.md`. Two notes with the same basename in different subfolders (`notes/work/todo.md`, `notes/home/todo.md`), or a note named like an existing bookmark page, therefore overwrite each other's wiki page, and both `notes.wiki_path` records point at the same file (issue #95). Deleting one of them makes the deletion reconcile remove the shared page, so the surviving note loses its wiki copy too. ADR-014 accepted note-vs-bookmark collisions as unlikely; the subfolder case is easy to hit.

## Decision

A note's wiki slug is assigned once — the basename slug if `wiki/<slug>.md` is free, otherwise the first free `<slug>-2` … `<slug>-100` — and stored in `notes.wiki_path`; every later update, index removal and deletion of that note uses the stored path instead of recomputing it from the filename.

## Rejected

- **Path-derived slug** (`notes/a/foo.md` → `a-foo`). Deterministic, but it renames the wiki page of every existing note in a subfolder, which needs a migration of the wiki file, `index.md` entry, edges, `## Related` backlinks in other pages and `notes.wiki_path`, and breaks `[[foo]]` links users already wrote. It also does not solve the note-vs-bookmark case: `notes/a-foo.md` and a bookmark titled "A Foo" still collide, so an ownership check is needed anyway. Paying for a migration on top of the check buys only predictability of the name.
- **Mirror folders under `wiki/notes/<path>.md`.** No collisions at all, but `wiki/` is flat by contract: `ScanWikiDir`, `index.md` links, edges keyed by slug and `[[slug]]` wikilinks all assume one level. Too much surface for a bug fix.
- **Skip the colliding note and log an error.** No data loss, but the note silently never reaches the wiki or the index; the user cannot see why without reading logs. Same reasoning as the `add_note` fix in #97, which chose a suffix over failing.

## Consequences

- No migration. Existing non-colliding notes keep their slug and wiki path unchanged; only a note that would collide gets a suffix.
- The slug is stable after the first write: renaming nothing, editing the note keeps its page. Which of two colliding notes gets the bare slug depends on scan order the first time; accepted, because the alternative (deterministic path slugs) costs a migration.
- "Free" means: no file at `wiki/<slug>.md`, or the file is the one this note's DB record already owns. A bookmark page, a hand-written wiki page, an MCP-created page or another note's page are all "taken" without the pipeline needing to know which kind it is.
- The deletion reconcile and the update path derive the index slug from `notes.wiki_path`, not from the filename, so deleting one of two same-named notes no longer removes the other's page.
- Vaults already hit by #95 hold two note records with the same `wiki_path`. A one-time repair clears the content hash of every record in each duplicate group (not just all-but-one), so the next cycle reprocesses all of them. Rule 1's shared check only sees a sibling's *current* `wiki_path`, which changes only once that sibling itself resolves and is re-upserted during this same cycle, so a record keeps finding the path shared — and keeps falling through to rule 2 — until every sibling ahead of it has already moved off it; rule 2 itself reuses the bare slug instead of suffixing only if `wiki/<slug>.md` has no content on disk at that moment. With the bare-slug file already present from before the repair, this plays out as: every record but one goes through rule 2 and gets suffixed, and whichever record is left once all its siblings have resolved away finds the path no longer shared and reuses it via rule 1 — in practice the record scan visits last in the group, since by then nothing else still points at the bare slug. Separately, a live note that happens to share its stored `wiki_path` with a now-deleted note's leftover DB record resolves via rule 2 (suffixed) even though it is the only real claimant — that leftover record is never itself reprocessed, since its file is gone, so the shared check never clears — until the deletion reconcile cleans up the stale record later in the same cycle.
- Out of scope: the bookmark pipeline still writes `wiki/<GenerateSlug(title)>.md` unconditionally, so a *new* bookmark can still overwrite a note's (or another bookmark's) page. Same class of bug, different writer; tracked separately.
- Shares the `-N` suffix convention and the 100-attempt cap with `add_note` (#97), so users see one naming rule for "name already taken".

## Verify-with

- `go test ./cmd/ -run TestProcessNotes_SlugCollision` — the repro from #95: bookmark `wiki/foo.md`, `notes/a/foo.md`, `notes/b/foo.md` produce three distinct pages and the bookmark page is untouched.
- `grep -n "TitleFromFilename" cmd/main.go` returns only the first-assignment site, never the update or deletion path.
