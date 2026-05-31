# Sub-plan 01: ExpandGraph Store Method + expand_graph MCP Tool

## Goal

Add multi-hop graph traversal to the store layer via recursive CTE, and expose it as an `expand_graph` MCP tool.

## Dependencies

None (first sub-plan).

## Context

### Existing code

- **`internal/store/edges.go`** — Has `GetOutboundEdges(slug)`, `GetInboundEdges(slug)` returning `[]Edge`. Edge struct: `{FromSlug, ToSlug, EdgeType string}`.
- **`internal/store/sqlite.go`** — `Store` struct with `db *sql.DB`. Constructor: `New(dbPath) (*Store, error)`.
- **`internal/mcpserver/server.go`** — `registerTools(s *server.MCPServer, v *vault.Vault, db *store.Store)` wires up MCP tools. Uses `mcp-go` SDK.
- **`internal/mcpserver/tools.go`** — Handler functions like `HandleSearchIndex(v, query)`, `HandleRelatedNotes(v, slug)`. Each returns data + error, registered in `server.go`.

### MCP tool registration pattern (from server.go)

```go
s.AddTool(mcp.NewTool("tool_name",
    mcp.WithDescription("..."),
    mcp.WithString("param", mcp.Required(), mcp.Description("...")),
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // extract params, call handler, marshal result
})
```

## Tasks

### 1. Add `ExpandGraph` method to `store.Store`

File: `internal/store/edges.go`

```go
// ExpandGraph returns all slugs reachable from the seed slugs within the given
// number of hops, using a recursive CTE over the edges table.
// It traverses both directions (outbound and inbound edges).
func (s *Store) ExpandGraph(seeds []string, maxHops int) ([]string, error)
```

Implementation notes:

- Use a recursive CTE that starts from seed slugs and walks edges in both directions up to `maxHops` levels.
- Return deduplicated list of slugs (excluding seeds themselves).
- Cap `maxHops` at 5 server-side to prevent runaway queries.
- If seeds is empty, return empty slice (no error).

SQL sketch:

```sql
WITH RECURSIVE reachable(slug, depth) AS (
    SELECT ?, 0  -- one per seed (use UNION ALL for multiple seeds)
    UNION
    SELECT CASE WHEN e.from_slug = r.slug THEN e.to_slug ELSE e.from_slug END, r.depth + 1
    FROM edges e
    JOIN reachable r ON (e.from_slug = r.slug OR e.to_slug = r.slug)
    WHERE r.depth < ?  -- maxHops
)
SELECT DISTINCT slug FROM reachable WHERE slug NOT IN (seeds...)
```

### 2. Write tests for `ExpandGraph`

File: `internal/store/edges_test.go`

Test cases:

- Empty seeds returns empty result.
- Single seed, 1 hop — returns direct neighbors only.
- Single seed, 2 hops — returns neighbors of neighbors.
- Multiple seeds — union of reachable sets.
- maxHops capped at 5 even if caller passes higher.
- Cycles don't cause infinite recursion.
- Self-edges (if any) don't appear in results.
- Seeds themselves are excluded from results.

### 3. Add `expand_graph` MCP tool

File: `internal/mcpserver/tools.go` — Add handler function:

```go
func HandleExpandGraph(db *store.Store, seeds []string, hops int) ([]string, error)
```

File: `internal/mcpserver/server.go` — Register the tool:

- Name: `expand_graph`
- Description: "Expand a set of seed slugs by traversing wiki-link edges up to N hops. Returns all connected slugs within the hop radius. Use this to discover clusters of related content."
- Parameters:
  - `seeds` (required, string): comma-separated list of slugs
  - `hops` (optional, number, default 2): max traversal depth (1-5)

### 4. Write MCP tool test

File: `internal/mcpserver/tools_test.go`

Test `HandleExpandGraph` with a seeded store (insert test edges, call handler, verify results).

## Acceptance Criteria

- [ ] `store.ExpandGraph(["kubernetes-networking"], 2)` returns slugs 2 hops away
- [ ] Cycles in the graph don't cause hangs or duplicates
- [ ] `expand_graph` MCP tool is registered and callable
- [ ] `mise run test` passes
- [ ] `mise run lint` passes
