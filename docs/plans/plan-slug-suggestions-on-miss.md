# Plan: Slug Suggestions on Not-Found Errors

> Follow-up to `docs/plans/plan-mcp-search-wiki.md` (issue #80, ADR-026). No new ADR
> needed: this enriches error messages within the existing discovery-tool boundary —
> no new tool, no gating change, no new data exposed beyond index.md metadata that
> `search_wiki` already returns.

## Problem

When an MCP agent calls `related_notes` or `read_wiki` with a slug that does not
exist, it gets a bare `wiki page "apple-m7" not found` error. Recovery requires the
agent to know it should call `search_wiki` next, costing an extra round-trip — and
weaker agents simply report "lookup failed". The failed input is usually a *near
miss* (a guessed or truncated slug), so the server already has enough signal to
point the agent at the right page.

## Proposed Solution

On a slug not-found error in `HandleRelatedNotes` and `HandleReadWiki`, search
index.md for the closest matching slugs and embed up to 5 suggestions in the error
message:

```text
wiki page "apple-m7" not found; similar pages: apple-m7-ultra-komt-in-2028-en-ondersteunt-tot-15t, apple-silicon-roadmap (use search_wiki for broader discovery)
```

The agent self-heals in the same round-trip: issue #80's flow drops from
fail → `search_wiki` → retry (3 calls) to fail-with-suggestions → retry (2 calls).

`expand_graph` is out of scope: it returns an empty result for unknown seeds rather
than an error, and changing that contract is not worth it — agents reach it via
`related_notes` output, which is already covered.

### Matching semantics (suggestion scoring)

`search_wiki`'s AND semantics are too strict for suggestions: a guessed slug
`apple-m7-chip` contains the term `chip` that matches nothing, and AND would return
zero suggestions exactly when the agent needs them. Instead:

1. **Normalize** the failed input: lowercase, replace `-`, `_`, `/` with spaces,
   split on `strings.Fields` → terms.
2. **Score** each index entry: count of terms that match (case-insensitive substring
   against slug, title, or any tag). Reuse the per-term matching logic from
   `matchesQuery` — extract a shared helper rather than duplicating.
3. **Rank**: keep entries with score ≥ 1, sort by score descending, ties broken by
   index.md file order (deterministic), return top `maxSlugSuggestions = 5` slugs.
4. Zero terms or zero scoring entries → original error unchanged (no
   "similar pages" clause).

## Implementation Plan (TDD)

Follow the project TDD workflow: failing tests first (spec-only subagent), then
minimal impl, then refactor.

### 1. Handler — `internal/mcpserver/tools.go`

- Extract `termMatchesEntry(entry *IndexEntry, term string) bool` from the body of
  `matchesQuery`; `matchesQuery` keeps its AND loop and calls the helper.
- Add `suggestSlugs(v *vault.Vault, input string) []string`:
  - reads index via `v.ReadIndex()`; on read error return nil (suggestions are
    best-effort — never mask the original not-found error with an index error)
  - normalize input (lowercase, `-`/`_`/`/` → space, `strings.Fields`)
  - score entries with `termMatchesEntry`, rank as described, cap at
    `maxSlugSuggestions = 5`
- `HandleReadWiki`: on `safeReadFile` error, call `suggestSlugs`; if non-empty,
  append `; similar pages: <s1>, <s2>, … (use search_wiki for broader discovery)`
  to the error message. Keep `%w` wrapping of the underlying error.
- `HandleRelatedNotes`: same treatment on the existence-check error.

### 2. Registration — `internal/mcpserver/server.go`

- No registration changes (no new tool, no gating change).
- Update tool descriptions for `related_notes` and `read_wiki` to mention that
  not-found errors include similar-slug suggestions (helps agents parse the error
  instead of giving up).

### 3. Tests

- `internal/mcpserver/tools_test.go`:
  - `suggestSlugs` (via handler): guessed slug `apple-m7` → suggestion list contains
    `apple-m7-ultra-…` style entry
  - partial-miss input (`apple-m7-chip`, one dead term) still yields suggestions
    (OR-scored, not AND)
  - ranking: entry matching 2 terms outranks entry matching 1
  - cap: >5 scoring entries → exactly 5 suggestions
  - no matches → error message identical to today (no "similar pages" clause)
  - missing/empty index.md → original not-found error, no panic, no suggestions
  - `HandleRelatedNotes` not-found error includes suggestions; happy path unchanged
  - `HandleReadWiki` not-found error includes suggestions; happy path unchanged
  - existing `matchesQuery` tests stay green after the helper extraction
- `internal/mcpserver/server_test.go`:
  - through-server: `related_notes` with unknown slug returns tool-level error whose
    text contains `similar pages:` (given a seeded index)
  - tool-name tables unchanged — assert no registration diff

### 4. Docs

- `README.md` — MCP tools table: note suggestion behavior on `related_notes` /
  `read_wiki` if error behavior is documented there.
- `CLAUDE.md` — no env-var changes; no update needed unless tool list is enumerated.
- `AGENTS.md` regeneration picks up the new tool descriptions automatically via
  `RegisteredTools`.

## Edge Cases

- **Input is an existing slug** — suggestion path never runs (existence check
  passes first).
- **Whitespace/empty input** — normalization yields zero terms → no suggestions,
  original error stands.
- **Index read fails during suggestion** — swallow the index error, return the
  original not-found error untouched.
- **Notes-prefixed slugs in `related_notes` output** — suggestions only cover wiki
  pages (index.md is wiki-only); acceptable, mirrors `search_wiki` scope.
- **Very long guessed input** — many terms just lower every entry's relative score
  uniformly; cap keeps the message bounded.

## Acceptance Criteria

- `related_notes("apple-m7")` against a vault containing
  `apple-m7-ultra-…` returns an error whose message lists that slug under
  `similar pages:`; retrying with the suggested slug succeeds.
- `read_wiki` behaves identically on miss.
- When nothing scores ≥ 1, error messages are byte-identical to current behavior.
- Suggestions capped at 5, deterministic order (score desc, then index order).
- All existing `mcpserver` tests stay green; no tool registration changes.

## Out of Scope

- `expand_graph` unknown-seed behavior (returns empty by contract, not an error).
- Accepting free-text queries directly in graph tools (option 2 — separate plan if
  suggestion-on-miss proves insufficient).
- Fuzzy matching / edit distance / stemming (term-substring scoring is enough for
  near-miss slugs; KISS).
- Structured error payloads (MCP tool errors are text; embedding in the message is
  what agents actually read).
