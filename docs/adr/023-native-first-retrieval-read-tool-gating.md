# 023 — Native-First Retrieval: Gate Duplicate MCP Read Tools Behind a Flag

**Status:** Accepted

**Context:** Filesystem-capable agents (e.g. the Hermes daemon, which mounts the vault) over-rely on the MCP content-read tools (`get_soul`, `search_index`, `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `vault_overview`) that merely duplicate direct file access. This funnels reactive exploration through narrower, capped tool contracts (e.g. `grep_vault` is substring-only and ≤20 results) and undercuts the reason an agent was chosen over RAG: free file access (see `docs/plans/plan-agent-retrieval.md`). AGENTS.md already frames direct reads as primary, but a registered tool stays salient and gets used regardless of instructions.

**Decision:** Gate the seven duplicate content-read MCP tools behind `MCP_READ_TOOLS` (default `false`); graph tools (`related_notes`, `expand_graph`) and all write tools (`add_bookmark`, `add_note`, `update_wiki`, `delete_page`) remain always registered. Filesystem-capable agents read the vault directly; clients reachable only over MCP (no filesystem) set `MCP_READ_TOOLS=true`.

**Consequences:**

- Default deployment exposes only graph + write tools, enforcing (not just suggesting) native-first retrieval for filesystem-capable agents like Hermes
- Clients without filesystem access (e.g. OpenWebUI over HTTP) restore the read tools with `MCP_READ_TOOLS=true` — no client is permanently broken
- `RegisteredTools(readTools)` and both the in-process and standalone servers honour the same flag, so the auto-generated `AGENTS.md` tool list always matches the live tool surface
- Reversible per deployment — the flag enables A/B comparison of native-first vs MCP-read retrieval quality (citation rate, recall, wasted reads)
- Graph queries (inbound backlinks, multi-hop) stay on MCP because they require the SQLite edge index, which a single file read cannot reconstruct
- Source attribution is unaffected: native reads expose each wiki page's `source:` frontmatter and `*Source: [title](url)*` footer, which AGENTS.md already requires agents to cite
