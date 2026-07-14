# 026 — Always-On `search_wiki`: Discovery Tools Are Not Content-Read Tools

**Status:** Accepted (amends 023)

**Context:** ADR-023 gates seven duplicate content-read MCP tools behind `MCP_READ_TOOLS` (default `false`), leaving graph and write tools always registered. But the always-on graph tools (`related_notes`, `expand_graph`) require an exact wiki slug as input, and slugs are title/URL-derived (e.g. `apple-m7-ultra-komt-in-2028-en-ondersteunt-tot-15t`) — unguessable from a topic. MCP-only clients (no filesystem) therefore cannot discover a slug at all in the default deployment, making the always-on graph tools unusable in practice (issue #80).

**Decision:** Rename the gated `search_index` tool to `search_wiki` and register it always-on, establishing a third tool category — *discovery* — alongside graph and write: a discovery tool returns identifiers and metadata (slug, title, tags, type) needed to invoke other tools, never page content.

**Consequences:**

- The gated content-read set shrinks from seven to six tools (`get_soul`, `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `vault_overview`); `search_index` is removed rather than kept as a duplicate alias, so the AGENTS.md tool list never shows two near-identical search tools.
- MCP-only clients get a complete default loop — `search_wiki(topic)` → slug → `related_notes`/`expand_graph` → `read_wiki` (if reads enabled) — instead of dead-ending on slug guessing.
- The category line is defensible under ADR-023's rationale: index.md metadata (slug/title/tags) is the *map*, not the *territory*; filesystem-capable agents lose nothing by its exposure since native grep remains strictly more powerful for content.
- Any client that used `search_index` had `MCP_READ_TOOLS=true` set explicitly and self-discovers the renamed tool from the tool list; no compatibility alias is kept (project convention: no backwards-compatibility shims).
- Risk accepted: a salient always-on search tool may tempt filesystem-capable agents away from native grep for topic lookups; mitigated by the tool description pointing filesystem agents to direct reads, consistent with AGENTS.md guidance.
