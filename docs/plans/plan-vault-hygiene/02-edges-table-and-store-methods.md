# Sub-Plan 02: Edges Table and Store Methods

## Goal

Add `edges` table to SQLite and implement all edge CRUD methods + cross-process write safety.

## Context

- **File:** `internal/store/sqlite.go` — contains `Store` struct, `migrate()`, all DB methods.
- **Tests:** `internal/store/sqlite_test.go`
- Uses `modernc.org/sqlite` (pure Go, no CGO).
- Plan decisions: D4 (cross-process write safety via exclusive tx), D5 (rebuild on empty table), D13 (FindBrokenEdges accepts exists checker fn).

### Edge struct

```go
type Edge struct {
    FromSlug string
    ToSlug   string
    Type     string
    Created  string
}
```

### Schema

```sql
CREATE TABLE IF NOT EXISTS edges (
    from_slug TEXT NOT NULL,
    to_slug   TEXT NOT NULL,
    type      TEXT NOT NULL DEFAULT 'wikilink',
    created   TEXT NOT NULL,
    PRIMARY KEY (from_slug, to_slug, type)
);
CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_slug);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);
```

## Tasks

1. **Add Edge type** to `store` package.
2. **Add migration** — extend `migrate()` to create `edges` table with indexes.
3. **Implement `RebuildEdges(edgeType string, edges []Edge) error`** — `DELETE FROM edges WHERE type = ?`, then bulk `INSERT OR IGNORE`.
4. **Implement `UpsertEdgesFrom(fromSlug, edgeType string, edges []Edge) error`** — `DELETE FROM edges WHERE from_slug = ? AND type = ?`, then `INSERT OR IGNORE`. Filter self-edges (`from_slug != to_slug`).
5. **Implement `DeleteEdgesInvolving(slug string) error`** — `DELETE FROM edges WHERE from_slug = ? OR to_slug = ?`.
6. **Implement `DeleteEdge(fromSlug, toSlug string) error`** — single edge deletion.
7. **Implement `GetOutboundEdges(slug string) ([]Edge, error)`** — `SELECT * FROM edges WHERE from_slug = ?`.
8. **Implement `GetInboundEdges(slug string) ([]Edge, error)`** — `SELECT * FROM edges WHERE to_slug = ?`.
9. **Implement `EdgeCount() (int, error)`**.
10. **Implement `FindOrphans() ([]string, error)`** — wiki slugs that appear in `from_slug` but never in `to_slug` of any edge (no inbound links). Must also check bookmarks/notes tables for slugs that have no edges at all.
11. **Implement `FindBrokenEdges(exists func(string) bool) ([]Edge, error)` (D13)** — edges where `exists(to_slug)` returns false.
12. **Implement `WithFileLock(fn func() error) error` (D4)** — wraps `fn` in `BEGIN EXCLUSIVE` transaction. Cross-process mutex via SQLite.
13. **Implement `DeleteBookmarkByWikiPath(wikiPath string) error`** — needed by delete_page in sub-plan 06.

## Acceptance Criteria

- [ ] `edges` table created on `migrate()` — existing data untouched
- [ ] All CRUD methods work correctly with test cases
- [ ] Self-edges filtered in `UpsertEdgesFrom`
- [ ] `FindOrphans` returns slugs with zero inbound edges
- [ ] `FindBrokenEdges` correctly uses the exists checker function
- [ ] `WithFileLock` uses exclusive transaction
- [ ] `DeleteBookmarkByWikiPath` deletes by `wiki_path` column
- [ ] `mise run test` passes, `mise run lint` passes

## Dependencies

- **01** — ParseWikiLinks hardening (edges will be populated using hardened parser)
