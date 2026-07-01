# Plan: `edit_page` MCP Tool

> Depends on: wikilink edges table + `update_wiki`/`delete_page` (already delivered).

## Current Situation

Wiki pages (`wiki/*.md`) are the LLM-enriched, curated layer of the vault. They are
**generated artifacts** derived from a source (`raw/` scrape) or a note, and they carry
derived state that lives outside the file:

- **Graph edges** in SQLite, parsed from `[[wikilinks]]` in the page body.
- **`last_updated`** frontmatter.
- **`index.md`** entry (title + tags), sourced from frontmatter.

Because that derived state is computed from page content, a raw filesystem edit to a
`wiki/` page silently desyncs it: the poll loop does **not** re-scan `wiki/` for manual
edits (edges are only rebuilt on `--rebuild-edges` or an empty edges table — see
`cmd/main.go:117-130`). The MCP write path is therefore the only consistency-preserving
way to mutate an existing wiki page.

Today that path is a single tool:

- **`update_wiki`** (`internal/mcpserver/write_tools.go:22`) — replaces the content under
  **one** `## heading`. Preserves frontmatter, bumps `last_updated`, and syncs edges
  **only when the `Related` section is edited**. Errors if the section does not exist
  (`write_tools.go:49`).

## Problem

`update_wiki` is section-scoped and cannot express whole-page mutations that an agent
routinely needs:

1. **Rewrite the whole body** — restructure a page, fix enrichment across multiple
   sections in one atomic call.
2. **Add a new section** that does not already exist — `update_wiki` rejects unknown
   sections rather than creating them.
3. **Re-derive all edges** — `update_wiki` only syncs edges for the `Related` section, so
   `[[wikilinks]]` introduced elsewhere in the body never become graph edges.

Without a whole-body tool, an agent restricted to MCP writes (the goal for an agentic
coding agent — narrow, typed, DB-consistent surface) hits a wall and is forced back to
raw filesystem edits, which desync the graph. `edit_page` closes that gap.

## Proposed Solution

Add an **`edit_page`** MCP write tool that replaces the **entire body** of an existing
wiki page while preserving system-owned frontmatter and keeping all derived state
consistent.

### Semantics

- **Input** — `slug` (targets `wiki/<slug>.md`) and `content` (the new **body only**, no
  frontmatter).
- **Frontmatter is system-owned and preserved.** The tool reads the existing page, keeps
  its frontmatter verbatim (title, tags, `source`, `created`), and only bumps
  `last_updated`. The caller cannot alter or strip frontmatter. This protects the
  provenance invariant (a wiki page must retain its source URL — see commit `73ea6da`)
  and keeps `index.md` consistent (title/tags unchanged → no index update needed).
- **Edges re-derived from the full new body.** All `[[wikilinks]]` in the new content are
  parsed and upserted as edges for this slug (superset of `update_wiki`'s Related-only
  behavior).
- **Atomic + race-safe.** File read/write happens inside `db.WithFileLock` (mirrors
  `HandleUpdateWiki`), so it cannot race the poll loop.
- **Errors** if the page does not exist (`edit_page` mutates only; creation flows through
  `add_bookmark`/`add_note` by design).

### Relationship to `update_wiki`

Both tools coexist and are complementary:

| Tool | Scope | Use when |
|------|-------|----------|
| `update_wiki` | one `## section` | Surgical edit of an existing section |
| `edit_page` | whole body | Restructure, multi-section rewrite, add new sections |

### Why not `create_page` / frontmatter editing

Out of scope by design. A wiki page with no `raw/` source or note behind it is an orphan
that violates the "wiki = enriched view of a source" invariant; creation belongs to the
`inbox`/`notes` entry points. Frontmatter/tag mutation is a separate concern (index
re-sync) and is deferred.

## Implementation Plan (TDD)

Follow the project TDD workflow: failing tests first (spec-only subagent), then minimum
implementation, then refactor.

### 1. Handler — `HandleEditPage`

`internal/mcpserver/write_tools.go`, modeled on `HandleUpdateWiki` but simpler (no section
parsing):

```go
func HandleEditPage(v *vault.Vault, db *store.Store, slug, body string) error
```

Inside `db.WithFileLock`:

1. `relPath := "wiki/" + slug + ".md"`; read via `v.ReadFile`, error if not found.
2. `frontmatter, _ := splitFrontmatter(existing)` — discard old body.
3. `frontmatter = upsertFrontmatterField(frontmatter, "last_updated", time.Now().Format("2006-01-02"))`.
4. Rebuild page: `frontmatter + body` (normalize a single separating newline; ensure
   trailing newline).
5. `v.WriteWiki(slug+".md", newContent)`.

Then, after the lock (the DB uses its own connection), re-derive edges from the
**full body**:

```go
links := ParseWikiLinks(body)
edges := ...
db.UpsertEdgesFrom(slug, "wikilink", edges)
```

(Same shape as `cmd/main.go:syncEdgesFromContent`; reuse `ParseWikiLinks` +
`UpsertEdgesFrom`.)

Reuses existing helpers only — no new vault/store APIs required.

### 2. Tool registration

`internal/mcpserver/server.go`, mirroring the `update_wiki` block (`server.go:281-312`),
registered in the always-on write-tools group (requires `db`):

- Name: `edit_page`
- Description: full-body replace of an existing wiki page; frontmatter preserved,
  `last_updated` bumped, edges re-synced.
- Params: `slug` (required), `content` (required, "new body markdown, no frontmatter").
- Reject empty `slug`/`content`; return a success string on completion.

### 3. Tests

`internal/mcpserver/write_tools_test.go` (extend existing suite):

- Replaces whole body, other sections gone/replaced as given.
- Frontmatter preserved verbatim except `last_updated` bumped.
- `source`/provenance frontmatter cannot be stripped by the caller.
- New `[[wikilinks]]` anywhere in the body become edges (`GetOutboundEdges`).
- Removed wikilinks drop their edges (upsert replaces the slug's edge set).
- Non-existent slug → error.
- Empty content → error.
- `index.md` unchanged (title/tags untouched).

### 4. Docs

- `CLAUDE.md` — add `edit_page` to the MCP write-tools description alongside
  `update_wiki`/`delete_page`.
- `README.md` — MCP tools list, if enumerated there.
- `internal/agentsmd` — if the generated `AGENTS.md` enumerates write tools, ensure
  `edit_page` appears (verify generator output).

## Edge Cases

- **Page has no frontmatter** — `splitFrontmatter` returns empty frontmatter;
  `upsertFrontmatterField` cannot insert `last_updated` without `---` delimiters. Match
  `update_wiki`'s existing behavior (writes body as-is); do not fabricate frontmatter.
- **Body without a trailing newline** — normalize to exactly one, consistent with
  `WriteWiki` output elsewhere.
- **Slug points at a note's wiki copy** — same target resolution as `update_wiki`
  (`wiki/<slug>.md`); out-of-scope note-reconcile interactions are unchanged and
  documented, not handled here.

## Acceptance Criteria

- `edit_page(slug, content)` replaces the full body of `wiki/<slug>.md`, preserves
  frontmatter, bumps `last_updated`, and re-derives edges from the whole body.
- Frontmatter (incl. `source`) is not caller-mutable.
- `index.md` stays consistent with no explicit index write.
- Non-existent slug and empty content are rejected with clear errors.
- All new tests green; `mise run test` and `mise run lint` pass.
- No new architectural decision required (extends the established "wiki mutations go
  through MCP" pattern); no ADR.

## Out of Scope

- `create_page` (violates provenance invariant — creation is `inbox`/`notes`).
- Frontmatter/tag editing and the corresponding `index.md` re-sync.
- `edit_note` / note-body editing (notes are reconciled by the poll loop; separate plan).
- Rename/move.
