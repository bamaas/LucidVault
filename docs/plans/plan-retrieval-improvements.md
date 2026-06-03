# Plan (DRAFT): Further Agent Retrieval Improvements

> **⚠️ STATUS: DRAFT — NOT FINAL.** This is a captured backlog of ideas for later
> reference only. It has **not** been stress-tested, scoped, or sized. Before any
> of these are implemented it must go through `/grill-with-docs` (to challenge the
> ideas against the codebase, sharpen terminology, and create ADRs where needed)
> and a review pass. Effort/impact estimates below are rough first guesses, not
> commitments. Do not `/deliver` directly from this document.

## Context

Instruction-only retrieval improvements are largely done and shipped on
`feature/hybrid-ranking-query-expansion` (Query Expansion, Source Attribution with
clickable hyperlinks, Web Search guidance, empty-vault disclosure — all in
`internal/agentsmd/template.md`). The ideas below are the remaining levers, which
require **code** or **architecture** changes rather than prompt tuning.

Grounding — current retrieval tools (`internal/mcpserver/tools.go`):

- `HandleSearchIndex` — case-insensitive substring match on `index.md`
  (slug/title/tags); **unranked, unbounded**.
- `HandleGrepVault` — substring scan across files; **first 20 matches, no ranking,
  silent truncation, single matching line returned**.
- `HandleRelatedNotes` / `HandleExpandGraph` — wiki-link graph traversal.
- No semantic/embedding layer exists.

## Candidate Improvements (ranked by rough impact-to-effort)

### A. Quick code wins (low effort)

1. **Surface `grep_vault` truncation.** Add `Truncated bool` + `TotalMatches int`
   to results (`tools.go:139,150`). Today the agent cannot distinguish "20 = all"
   from "20 = partial" and stops early. ~10 lines.
2. **Return snippets, not single lines.** `grep_vault` returns only the matching
   line (`tools.go:163-167`). Returning a few surrounding lines lets the agent
   judge relevance without opening the whole file — fewer follow-up reads.
3. **Bound + rank `search_index`.** It is unranked and unbounded
   (`tools.go:73-90`); a common term floods the agent. Cap results and order
   title-match > tag-match > slug-match.

### B. Hybrid lexical ranking (medium effort) — the branch's namesake

Merge `search_index` + `grep_vault` into **one ranked list** with field-weighted
lexical scoring (title > tag > body), term frequency, and match position
(BM25-style). Replace the arbitrary 20-result cap with a relevance cut. Pure Go,
no new dependencies, fits KISS. Touches the tool contract → needs an ADR.
**This is the most likely "next feature."**

### C. Semantic search (large effort, ADR-level)

Everything today is lexical: "k8s" only finds that literal string (query expansion
mitigates but imperfectly). True conceptual queries need **embeddings** — generate
them in the pipeline (Ollama can embed), persist vectors, retrieve by cosine
similarity. Only *then* does "hybrid ranking" become genuinely hybrid
(lexical + semantic), as the original suggestion assumed. New dependency, new
pipeline step, new persistence. Largest change; requires its own design cycle/ADR.

### D. Index-quality lever (pipeline change)

4. **Richer `index.md`.** Currently slug/title/tags only. Add a one-line summary
   per page (the enrich step already produces a summary; thread it into
   `vault.UpdateIndex`) so the agent can triage from the index alone — fewer
   speculative page opens.

### E. Feedback loop (fits Hermes' design)

5. **Capture good retrieval patterns as a Hermes `vault-query` skill**
   (see `plan-agent-retrieval.md` step 14). Hermes self-improves; encoding
   "for X-type questions, search Y then expand Z" as a skill makes retrieval
   improve with use — complements the always-on AGENTS.md rules. Lives Hermes-side,
   outside this repo.

## Open Questions (resolve during grilling)

- Does hybrid lexical ranking (B) justify changing the public MCP tool contract,
  or should it be a new tool alongside the existing two?
- Is semantic search (C) worth the dependency/persistence cost at this vault scale,
  or is well-tuned lexical ranking + query expansion "good enough"?
- Should `index.md` enrichment (D) bloat the file for large vaults? Cap summary
  length? Paginate?
- Truncation/snippet changes (A) — confirm they don't regress existing
  `tools_test.go` expectations or context-budget assumptions.

## Explicitly NOT decided here

- Whether any of these ships, in what order, or in which release.
- Tool contracts, scoring weights, embedding model choice, storage format.
- These are inputs to `/grill-with-docs`, not conclusions.
