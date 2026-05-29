# Sub-Plan 05: Hygiene Cycle (Auto-Fix)

## Goal

Add a periodic hygiene cycle to the poll loop that auto-fixes broken edges, stale index entries, raw/wiki drift, and logs orphan pages.

## Context

- **Files:** `cmd/main.go` (poll loop), `internal/vault/writer.go`, `internal/store/sqlite.go`
- Plan decisions: D10 (bidirectional index sync), D13 (FindBrokenEdges), D14 (orphaned raw → delete), D15 (missing raw → rewrite footer).
- `HYGIENE_INTERVAL` env var controls frequency (default: every 10th poll cycle).

### Vault methods needed

- `ScanWikiDir()` — from sub-plan 03
- `ScanRawDir() ([]string, error)` — new, returns raw file paths
- `ParseFrontmatterTags(content string) []string` — parse tags from wiki frontmatter
- `ParseFrontmatterURL(content string) string` — parse `url:` from frontmatter
- `HasRawFooterLink(content, rawPath string) bool` — check if footer links to raw file
- `RewriteFooterLink(filePath, oldTarget, newTarget string) error` — replace raw link with URL

### Existing vault methods used

- `FileExists`, `ReadFile`, `DeleteFile`, `UpdateIndex`, `RemoveFromIndex`, `ReadIndex`

## Tasks

1. **Implement `vault.ScanRawDir() ([]string, error)`** — list all `.md` files in `raw/` directory.

2. **Implement broken edge cleanup** — call `store.FindBrokenEdges(v.FileExists)`, delete each broken edge, log.

3. **Implement `syncIndex(db, v)`** (D10) — bidirectional 3-direction sync in single pass:
   - Parse all index.md entries into map[slug]IndexEntry.
   - Walk all wiki files on disk.
   - Direction 1: index entry exists, file gone → `RemoveFromIndex(slug)`.
   - Direction 2: file exists, not in index → `UpdateIndex(slug, title, tags)`.
   - Direction 3: tags or title drifted → `RemoveFromIndex` + `UpdateIndex`.
   - Handle pages without frontmatter title — fall back to `notes.TitleFromFilename()`.

4. **Implement `cleanRawWikiOrphans(v)`**:
   - D14: raw file exists, no matching wiki → `DeleteFile(rawPath)`, log.
   - D15: wiki exists, raw missing, footer has broken raw link → `RewriteFooterLink` to use URL from frontmatter.

5. **Implement `runHygiene(db, v)`** — orchestrates all hygiene steps:
   - Broken edge cleanup
   - Index sync
   - Raw/wiki consistency
   - Log orphan pages (`store.FindOrphans`)

6. **Add hygiene to poll loop** — run every Nth cycle based on `HYGIENE_INTERVAL` env var (default 10). Add counter to `runPollCycle`.

7. **Add `HYGIENE_INTERVAL` to config** — parse from env, add to `loadConfig`.

## Acceptance Criteria

- [ ] Broken edges removed from DB (page content unchanged)
- [ ] Stale index entries removed when wiki file deleted
- [ ] Missing index entries added when wiki file exists but not in index
- [ ] Tag/title drift in index corrected to match frontmatter
- [ ] Orphaned raw files (no wiki match) deleted
- [ ] Broken raw footer links rewritten to original URL
- [ ] Orphan pages logged as warnings (not auto-deleted)
- [ ] Hygiene runs every Nth poll cycle (configurable)
- [ ] Empty vault / fresh install: hygiene is a no-op
- [ ] `mise run test` passes, `mise run lint` passes

## Dependencies

- **02** — Edges table store methods (FindBrokenEdges, DeleteEdge, FindOrphans)
- **03** — ScanWikiDir, edge rebuild
