# Sub-Plan 01: ParseWikiLinks Hardening

## Goal

Harden `ParseWikiLinks()` so it produces reliable, clean edges — prerequisite for edges table.

## Context

- **File:** `internal/mcpserver/parse.go` — contains `ParseWikiLinks(content string) []string`
- **Tests:** `internal/mcpserver/parse_test.go`
- Current implementation is a simple regex that extracts `[[...]]` targets. It does not handle code blocks, frontmatter, pipe syntax, raw file refs, or self-edges.
- Plan decisions: D1 (wiki-to-wiki only), D2 (pipe syntax splitting).

## Tasks

1. **Skip fenced code blocks** — Do not extract wikilinks from content inside `` ``` `` or `~~~` fences.
2. **Skip inline code** — Do not extract wikilinks from content inside backtick spans.
3. **Skip frontmatter** — Only parse content after the closing `---` of YAML frontmatter. A `related: [[other-page]]` in YAML should not produce edges.
4. **Handle pipe syntax (D2)** — Split `[[slug|Display Name]]` on `|`, take first part as target slug.
5. **Filter raw file refs (D1)** — Exclude targets ending in `.md` (these are raw file back-references, not semantic wiki links).
6. **Deduplicate** — Return unique slugs only (same target may appear multiple times in a page).
7. **Update signature** — Add `fromSlug string` parameter so self-edges (`fromSlug == target`) can be filtered. Alternatively, return all and let caller filter — decide based on simplicity.

## Acceptance Criteria

- [x] `ParseWikiLinks` skips wikilinks inside fenced code blocks (``` and ~~~)
- [x] `ParseWikiLinks` skips wikilinks inside inline code backticks
- [x] `ParseWikiLinks` skips wikilinks in YAML frontmatter
- [x] `[[slug|Display Name]]` returns `"slug"`, not `"slug|Display Name"`
- [x] Targets ending in `.md` are filtered out
- [x] Duplicate targets are deduplicated
- [x] All existing tests still pass
- [x] New test cases cover each hardening scenario
- [x] `mise run test` passes, `mise run lint` passes

## Dependencies

None — this is the foundation sub-plan.
