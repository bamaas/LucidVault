# 018 — Edges Table for Wiki Graph

**Status:** Accepted

**Context:** LucidVault needs bidirectional link traversal (inbound/outbound), orphan detection, and broken link detection. Obsidian's native graph is not accessible programmatically, and external graph databases add operational complexity.

**Decision:** Store wiki-to-wiki edges in an `edges` table in the existing SQLite database, populated from `[[wikilinks]]` parsed from wiki page content.

**Consequences:**

- Zero new dependencies — reuses existing pure-Go SQLite instance
- Enables orphan detection (`FindOrphans`), broken edge detection (`FindBrokenEdges`), and bidirectional traversal
- Only wiki-to-wiki links stored (raw file back-refs filtered out — structural, not semantic)
- Self-edges filtered at insert time + SQL safety net
- Full rebuild available on empty table or `--rebuild-edges` flag; incremental sync after each enrichment
- Table is a derived cache — can be rebuilt from wiki files at any time
