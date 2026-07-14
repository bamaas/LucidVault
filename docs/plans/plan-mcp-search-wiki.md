# Plan: Always-On `search_wiki` MCP Tool

> Resolves issue #80. Architectural decision: `docs/adr/026-always-on-search-wiki-discovery-tool.md` (amends ADR-023).

## Problem

MCP-only agents cannot find anything in the vault unless they already know the exact slug.

The always-on graph tools `related_notes` and `expand_graph` take an exact wiki slug as
input (they resolve `wiki/<slug>.md` and the SQLite edge table by slug). Slugs are
derived from a saved article's title or URL — e.g.
`apple-m7-ultra-komt-in-2028-en-ondersteunt-tot-15t` — not from its topic. An agent
asked about "Apple M7 Ultra" guesses `apple-m7`, `apple-silicon`, gets not-found for
each, and must report "lookup failed" even though a matching page exists.

The existing `search_index` tool would solve this, but ADR-023 gates it behind
`MCP_READ_TOOLS` (default `false`). Filesystem-capable agents route around the gap with
native grep; clients reachable only over MCP HTTP (browser chat, remote agents) are
stuck: the default deployment gives them graph tools they cannot feed.

## Proposed Solution

Rename `search_index` → `search_wiki` and move it from the gated content-read group to
the always-on group. One handler, one tool name — `search_index` is removed, not
aliased (any client using it already set `MCP_READ_TOOLS=true` and rediscovers the
renamed tool from the tool list).

Rationale for always-on (see ADR-026): the tool returns only index.md metadata — slug,
title, tags, type — never page content. It is a *discovery* tool in the same category
as the graph tools: it produces the identifiers other tools consume. ADR-023's
native-first argument is about content reads; exposing the map does not undercut it.

Name is `search_wiki` (not `search_vault` / `search_notes`) because index.md indexes
wiki pages only — the name must not promise notes/raw coverage it does not have.

### Behavior changes to the handler

1. **Multi-word AND matching.** `matchesQuery` currently requires the whole query as a
   single substring; `"Apple M7"` fails against slug `apple-m7-ultra-…`. Change: split
   the query on whitespace; every term must match (each term independently against
   slug, title, or tags, case-insensitive). Single-term queries behave exactly as
   before.
2. **Result cap.** `HandleSearchIndex` currently returns all matches. Cap at 50
   (grep_vault precedent is 20; discovery lists are cheaper per entry).
3. **Whitespace trim.** Trim the query before the empty-check so `"  "` is rejected as
   empty rather than matching everything/nothing.

## Implementation Plan (TDD)

Follow the project TDD workflow: failing tests first (spec-only subagent), then minimal
implementation, then refactor.

### 1. Handler — `internal/mcpserver/tools.go`

- `matchesQuery(entry, queryLower)` → split `queryLower` on `strings.Fields`; return
  true only if every term matches slug, title, or any tag (substring,
  case-insensitive). Zero terms → false.
- `HandleSearchIndex`: trim query; cap results at `maxSearchResults = 50`.
- Keep the function name `HandleSearchIndex` (it describes the data source; only the
  MCP tool name is user-facing).

### 2. Registration — `internal/mcpserver/server.go`

- Move the tool block out of the `if readTools { … }` group into the always-on group.
- Rename tool `search_index` → `search_wiki`.
- Description: search wiki pages by topic keywords across slug, title, and tags;
  returns slugs for use with `related_notes` / `expand_graph` / `read_wiki`. Note for
  filesystem-capable agents: prefer native grep for content search.
- Handler closure: trim query, reject empty with a clear error.
- Update `RegisteredTools(readTools)` the same way: `search_wiki` in the always-on
  list, `search_index` removed from the read-tools list.

### 3. Tests

- `internal/mcpserver/tools_test.go` — extend `TestHandleSearchIndex`:
  - multi-word query matches when terms hit different fields
    (`"apple m7"` → slug `apple-m7-ultra-…`; `"kubernetes networking"` → title term +
    tag term)
  - multi-word query with one non-matching term → no match
  - single-word behavior unchanged (existing cases stay green)
  - whitespace-only query → empty result (handler) / error (tool layer)
  - result cap: >50 matching entries returns exactly 50
- `internal/mcpserver/server_test.go` — update the tool-name tables:
  - add `search_wiki` to `alwaysOnToolNames`
  - remove `search_index` from `readToolNames`
  - `TestRegisteredToolsMatchesServer` enforces registerTools/RegisteredTools sync;
    it must pass with both flag values.

### 4. Docs

- `CLAUDE.md` — `MCP_READ_TOOLS` env-var description: remove `search_index` from the
  gated tool list; mention `search_wiki` alongside graph tools as always registered.
- `README.md` — MCP tools list, if enumerated.
- `docs/adr/023-native-first-retrieval-read-tool-gating.md` — leave as-is (ADR-026
  records the amendment); ADRs are immutable history.
- `AGENTS.md` regeneration is automatic via `RegisteredTools` (verify generator output
  lists `search_wiki` with the always-on group).

## Edge Cases

- **Empty index.md / missing index** — existing `ReadIndex` error path unchanged.
- **Query matches hundreds of entries** — cap to 50; order remains index.md file order
  (deterministic). Ranking is out of scope.
- **Unicode/diacritics in titles** — `strings.ToLower` semantics unchanged from today;
  no new normalization.
- **`MCP_READ_TOOLS=true` deployments** — they lose the `search_index` name and gain
  `search_wiki`; behavior is a strict superset (multi-word AND, cap). No alias.

## Acceptance Criteria

- Default deployment (`MCP_READ_TOOLS=false`): `search_wiki("apple m7")` returns the
  entry whose slug is `apple-m7-ultra-…`; the returned slug feeds `related_notes`
  without error. Issue #80's flow works end-to-end.
- `search_index` is no longer registered under any flag value.
- `TestRegisteredToolsMatchesServer` passes for both flag values.
- All new tests green; `mise run test` and `mise run lint` pass.
- CLAUDE.md / README.md updated; generated AGENTS.md lists `search_wiki` as always-on.

## Out of Scope

- Searching `notes/` or `raw/` (index.md is wiki-only; a broader `search_vault` would
  need a new data source — separate plan if ever needed).
- Relevance ranking / fuzzy matching / stemming (KISS; substring AND is enough for
  slug discovery).
- Full-text content search (that is `grep_vault`, gated by design).
- MCP resources exposure of `wiki/` (listing loses to searching for this use case).
