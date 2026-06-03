# Plan: Agent Retrieval Instructions (Query Expansion, Source Attribution, Web Search)

## Problem

LucidVault generates `AGENTS.md` (vault root) from the static source
`internal/agentsmd/template.md` plus dynamic sections (MCP tools, vault stats).
Hermes loads the vault-root `AGENTS.md` into its system prompt at session start
(see `docs/plans/plan-agent-retrieval.md`), so the static template is the single
controllable place to steer Hermes' retrieval behaviour.

Three behaviours are currently unspecified, leaving retrieval quality to the
agent's defaults:

1. **No query expansion** — a query for "k8s networking" never broadens to
   "kubernetes networking", hurting recall.
2. **No source attribution** — answers don't say whether a fact came from the
   vault, the model's own knowledge, or the web.
3. **No web-search guidance** — when web search is available (configured
   Hermes-side), nothing tells the agent to prefer the vault first and cite URLs.

## Solution

Add three static instruction sections to `internal/agentsmd/template.md`. This is
an **instruction-only** change — no code logic, no new MCP tools, no Ollama calls,
no embedding store. The text is static, so it does not add cache churn to Hermes'
prompt prefix (only the existing dynamic stats sections do).

This is the correct layer per Hermes' own design: query expansion and attribution
must apply to **every** retrieval, and the vault-root `AGENTS.md` is always loaded
(unlike skills, which are lazy-loaded on demand).

### Section 1 — Query Expansion

Instruct the agent to broaden queries before searching:

- Synonyms & abbreviations (e.g. "k8s" → "kubernetes"), running each variant and
  merging results.
- Personalization via `soul.md` interests/expertise to disambiguate terms.
- Lateral terms — pull tags and `[[wikilinks]]` from the top hit and re-search.

### Section 2 — Source Attribution

Instruct the agent to always state the origin of each piece of an answer:

- **Vault** — cite the exact file path / wiki slug (e.g. `wiki/raft-consensus.md`).
- **Model knowledge** — say so explicitly ("from my own knowledge, not the vault").
- **Web search** — cite the source URL (and provider).
- Blended answers attribute each part separately.

### Section 3 — Web Search

Instruct the agent to search the vault and notes first, reach for web search only
when the vault doesn't cover the question, and always cite retrieved URLs (cross-
referencing Source Attribution). This guides *when* to web-search; it does not
grant the capability.

## Scope

### In scope

- Edits to `internal/agentsmd/template.md` (the embedded static template).
- TDD tests in `internal/agentsmd/agentsmd_test.go` asserting each new section
  renders through `agentsmd.Generate(...)`.

### Out of scope

- **Hybrid ranking** — merging/scoring `search_index` + `grep_vault` is a separate,
  larger lexical-scoring change requiring its own design/ADR.
- **Enabling Perplexity (or any) web search** — that is Hermes-host configuration
  (`hermes-web-search-plus` plugin + `PERPLEXITY_API_KEY`), outside this repo.
- **Code-side query expansion** — deliberately rejected in favour of the
  instruction route (cheaper, idiomatic, no Ollama latency).

## Acceptance Criteria

1. `agentsmd.Generate(...)` output contains a `## Query Expansion` section with the
   synonym/abbreviation, personalization, and lateral-term guidance.
2. Output contains a `## Source Attribution` section covering vault / model
   knowledge / web-search origins.
3. Output contains a `## Web Search` section instructing vault-first then cite URLs.
4. Existing static sections (Vault Access Rules, Retrieval Strategy, Content
   Guidelines) and dynamic sections (Available MCP Tools, Vault Statistics) still
   render — no regressions.
5. `mise run test` and `mise run lint` pass.

## Edge Cases

- **Empty tools / empty stats** — new static sections must still render (existing
  tests cover the empty-tools and zero-stats paths; new assertions should not
  depend on dynamic content).
- **First-match-wins context priority** — Hermes loads only one root context file
  (`.hermes.md` → `HERMES.md` → `AGENTS.md` → `CLAUDE.md` → `.cursorrules`). In the
  default deploy `claudemd.Upsert` writes to `/CLAUDE.md` (host root), not the vault
  root, so there is no collision. No code change needed; documented here as a
  deployment caveat.
