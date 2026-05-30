# 020 — Auto-Linking at Enrichment

**Status:** Accepted

**Context:** Wiki pages added later never get linked back from existing related pages, creating orphans and one-directional links. Detection-based fixes (hygiene cycle) can remove broken links but cannot create new semantic connections without an LLM call.

**Decision:** After writing a new wiki page, automatically add back-links to the top 3 existing pages that share 2+ tags. Links are inserted into the `## Related` section with format `[[slug]] — shared tags: x, y`.

**Consequences:**

- Prevention over detection — most orphans and one-directional links are avoided at source
- Deterministic, zero-cost (no LLM call) — uses tag overlap from index.md
- Capped at 3 back-links per enrichment to avoid spamming old pages
- Candidate ordering: tag overlap DESC → file mtime DESC → slug ASC (most relevant, most recent first)
- New page excluded from its own candidate list (would always match)
- Only touches `## Related` section — never modifies other content
