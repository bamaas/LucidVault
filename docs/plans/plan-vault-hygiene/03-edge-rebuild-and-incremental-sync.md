# Sub-Plan 03: Edge Rebuild and Incremental Sync

## Goal

Wire edge population into startup (full rebuild) and pipeline (incremental update after each enrichment).

## Context

- **Files:** `cmd/main.go` (poll loop, `processInboxItem`, `processNotes`), `internal/vault/writer.go`, `internal/mcpserver/parse.go`
- **Depends on:** Sub-plan 01 (hardened `ParseWikiLinks`), sub-plan 02 (edges table + store methods)
- Plan decisions: D5 (rebuild when edges table empty or `--rebuild-edges` flag).

### Vault methods needed

`vault.Vault` already has `FileExists`, `ReadFile`. Need to add:

- `ScanWikiDir() ([]string, error)` — returns list of relative paths like `wiki/foo.md`, `wiki/notes/bar.md`. Recursive walk of `wiki/` directory.

### Flow: Full rebuild (D5)

On startup:

1. Check `store.EdgeCount()` — if 0, trigger full rebuild.
2. Also trigger if `--rebuild-edges` CLI flag is set.
3. Call `vault.ScanWikiDir()` to get all wiki files.
4. For each file: read content, call `ParseWikiLinks(content)`, build `Edge` list with slug derived from path.
5. Call `store.RebuildEdges("wikilink", allEdges)`.

### Flow: Incremental update

In `processInboxItem` and `processNotes`, after calling `vault.WriteWiki()`:

1. Parse `[[wikilinks]]` from the newly written wiki content.
2. Call `store.UpsertEdgesFrom(slug, "wikilink", edges)`.

## Tasks

1. **Add `ScanWikiDir()` to vault** — recursive walk of `wiki/` returning relative paths. Must include `wiki/notes/` subdirectory.
2. **Add `--rebuild-edges` flag** to CLI argument parsing in `main.go`.
3. **Implement full rebuild logic** — triggered by empty edges table or `--rebuild-edges` flag. Runs once at startup before poll loop.
4. **Add incremental edge sync to `processInboxItem`** — after `WriteWiki`, parse wikilinks from content, call `UpsertEdgesFrom`.
5. **Add incremental edge sync to `processNotes`** — same pattern as inbox item.
6. **Add helper: `slugFromWikiPath(path string) string`** — extracts slug from wiki path (e.g., `wiki/foo.md` → `foo`, `wiki/notes/bar.md` → `notes/bar`).

## Acceptance Criteria

- [x] `ScanWikiDir` returns all `.md` files recursively under `wiki/`
- [x] `--rebuild-edges` flag triggers full edge rebuild
- [x] Empty edges table triggers automatic rebuild on startup
- [x] After `processInboxItem` writes wiki, edges are synced incrementally
- [x] After `processNotes` writes wiki, edges are synced incrementally
- [x] Rebuild is idempotent — running twice produces same result
- [x] `mise run test` passes, `mise run lint` passes

## Dependencies

- **01** — ParseWikiLinks hardening
- **02** — Edges table and store methods
