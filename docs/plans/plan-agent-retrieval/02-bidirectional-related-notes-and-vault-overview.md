# Sub-plan 02: Bidirectional related_notes + vault_overview MCP Tool

## Goal

Upgrade `related_notes` to use bidirectional edge lookups from SQLite (instead of forward-only wikilink parsing), and add a new `vault_overview` MCP tool for agent orientation.

## Dependencies

- Sub-plan 01 (ExpandGraph store method must exist; `db *store.Store` is already passed to server).

## Context

### Current `HandleRelatedNotes` (tools.go)

Currently parses `[[wikilinks]]` from the wiki file content directly (forward links only). Does NOT use the edges table. Needs to be updated to use `store.GetOutboundEdges` + `store.GetInboundEdges` for bidirectional traversal.

### Existing store methods (edges.go)

- `GetOutboundEdges(slug) ([]Edge, error)` — edges FROM this slug
- `GetInboundEdges(slug) ([]Edge, error)` — edges TO this slug
- `EdgeCount() (int, error)`

### Existing vault methods (writer.go)

- `ScanWikiDir() ([]string, error)` — lists wiki files
- `ReadIndex() (string, error)` — reads index.md
- `ReadFile(relPath) (string, error)` — reads any vault file

### MCP server wiring (server.go)

`registerTools(s, v, db)` — both `vault.Vault` and `store.Store` are available. The `related_notes` tool handler currently only receives `v *vault.Vault`; it needs `db *store.Store` too.

## Tasks

### 1. Update `HandleRelatedNotes` signature to accept `*store.Store`

File: `internal/mcpserver/tools.go`

Change from:

```go
func HandleRelatedNotes(v *vault.Vault, slug string) ([]RelatedEntry, error)
```

To:

```go
func HandleRelatedNotes(v *vault.Vault, db *store.Store, slug string) ([]RelatedEntry, error)
```

Implementation:

- Query `db.GetOutboundEdges(slug)` for forward links.
- Query `db.GetInboundEdges(slug)` for backlinks.
- Merge, deduplicate, mark direction (outbound/inbound/both).
- Update `RelatedEntry` struct to include a `Direction string` field (`"outbound"`, `"inbound"`, `"both"`).
- Keep existing behavior of reading the wiki file for title extraction.

### 2. Update `related_notes` registration in server.go

File: `internal/mcpserver/server.go`

Pass `db` to the handler call. Update the tool description to mention bidirectional traversal.

### 3. Update `HandleRelatedNotes` tests

File: `internal/mcpserver/tools_test.go`

- Test with edges in both directions.
- Test slug with only outbound edges.
- Test slug with only inbound edges.
- Test slug with edges in both directions to same target (marked "both").
- Test slug with no edges.

### 4. Add `HandleVaultOverview` handler

File: `internal/mcpserver/tools.go`

```go
func HandleVaultOverview(v *vault.Vault, db *store.Store) (*VaultOverview, error)
```

`VaultOverview` struct:

```go
type VaultOverview struct {
    WikiCount   int      `json:"wiki_count"`
    RawCount    int      `json:"raw_count"`
    NoteCount   int      `json:"note_count"`
    EdgeCount   int      `json:"edge_count"`
    TopTags     []string `json:"top_tags"`     // top 10 tags by frequency from index.md
    HasSoul     bool     `json:"has_soul"`
    LastUpdated string   `json:"last_updated"` // from index.md "Last updated" field
}
```

Implementation:

- Count wiki files via `v.ScanWikiDir()`
- Count raw files via `v.ScanRawDir()`
- Count notes by scanning notes/ dir
- Edge count via `db.EdgeCount()`
- Parse index.md for tag frequency and last-updated timestamp
- Check soul.md existence via `v.FileExists("soul.md")`

### 5. Register `vault_overview` MCP tool

File: `internal/mcpserver/server.go`

- Name: `vault_overview`
- Description: "Get a high-level overview of the vault: page counts, edge count, top tags, and metadata. Use this for orientation before diving into specific queries."
- No parameters.

### 6. Write `HandleVaultOverview` tests

File: `internal/mcpserver/tools_test.go`

- Test with populated vault (wiki, raw, notes dirs with files).
- Test with empty vault.
- Test tag counting from index.md.

## Acceptance Criteria

- [ ] `related_notes` returns both inbound and outbound edges with direction labels
- [ ] `vault_overview` returns accurate counts for wiki, raw, notes, edges
- [ ] `vault_overview` extracts top tags from index.md
- [ ] All existing `related_notes` tests still pass (backward compatible output format)
- [ ] `mise run test` passes
- [ ] `mise run lint` passes
