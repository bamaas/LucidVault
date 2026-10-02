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
- The slug is stable after the first write: editing the note keeps its page; only moving or renaming it can reassign a slug. Which of two colliding notes gets the bare slug depends on scan order the first time; accepted, because the alternative (deterministic path slugs) costs a migration.
- "Free" means: `wiki/<slug>.md` has no content, or the file is the one this note's DB record already owns, **and** no other note record already claims that candidate's `wiki_path` (`NotesSharingWikiPath`) — this second check is what catches a hand-deleted wiki page whose owning note record hasn't been reprocessed yet; without it, a new note could claim the same `wiki_path` as that still-live record, creating a duplicate. A bookmark page, a hand-written wiki page, an MCP-created page or another note's page are all "taken" without the pipeline needing to know which kind it is.
- The resolved `wiki_path` is persisted to the note's DB record (via a dedicated `UpsertNote` call carrying the note's existing content hash, not an empty one) immediately after the wiki page is written and before the edge-sync, auto-link and index side effects run. Without this early claim, a failure in one of those side effects would leave the just-written wiki page orphaned with no DB record, and the next cycle's rule 2 would see `wiki/<slug>.md` already has content and permanently suffix the note onto `<slug>-2` instead of letting it reclaim `<slug>` on retry.
- The deletion reconcile and the update path derive the index slug from `notes.wiki_path`, not from the filename, so deleting one of two same-named notes no longer removes the other's page.
- Vaults already hit by #95 hold two note records with the same `wiki_path`. A one-time repair clears the content hash of every record in each duplicate group (not just all-but-one), so the next cycle reprocesses all of them. Each one except the last one scanned is suffixed, and the last one keeps the bare slug. Rule 1's shared check only counts a sibling still present in the current scan, so a live note's own stored `wiki_path` is never pushed off it by a leftover record belonging to a note no longer on disk — that case is left for the deletion reconcile to clean up.
- Out of scope: the bookmark pipeline still writes `wiki/<GenerateSlug(title)>.md` unconditionally, so a *new* bookmark can still overwrite a note's (or another bookmark's) page. Same class of bug, different writer; tracked separately.
- Shares the `-N` suffix convention and the 100-attempt cap with `add_note` (#97), so users see one naming rule for "name already taken".

## Verify-with

- `go test ./cmd/ -run TestProcessNotes_SlugCollision` — the repro from #95: bookmark `wiki/foo.md`, `notes/a/foo.md`, `notes/b/foo.md` produce three distinct pages and the bookmark page is untouched.
- `grep -n "TitleFromFilename(nf.Path)" cmd/main.go` returns only the first-assignment site (`resolveNoteWikiSlug`), never the update or deletion path. (A plain `grep -n "TitleFromFilename" cmd/main.go` also matches an unrelated, pre-existing hygiene fallback that derives a wiki *page's* display title from its filename when the page has no frontmatter title or H1 — ignore that hit.)
