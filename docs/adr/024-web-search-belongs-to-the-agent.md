# 024 — Web Search Belongs to the Agent, Not LucidVault

**Status:** Accepted

**Context:** LucidVault's retrieval is closed-world (vault-only); users want current or missing information from the web. The obvious move — a Tavily-backed `search_web` MCP tool gated on an API key, mirroring the Raindrop/Supadata feature pattern — has two fatal problems for this codebase. First, the primary consuming agent (the Hermes daemon) already has a native `web_search` capability, so a LucidVault web tool would duplicate it and reintroduce the exact tool-duplication that ADR-023 was just written to remove ("a registered tool stays salient and gets used regardless of instructions"). Second, web search is a fast-churning commodity market (Tavily, Exa, Brave, Firecrawl, fastCRW, Scavio, …); whichever provider LucidVault wrapped, swapping it would mean a Go change + release + redeploy — the wrong layer for something this volatile, and against the project's KISS / separation-of-concerns principles.

**Decision:** LucidVault does not provide, proxy, or wrap web search. Instead, the generated `AGENTS.md` instructs the agent how to use its *own* web search, via a provider-agnostic, configurable **Web Search Strategy**.

**Consequences:**

- No new dependency, no API key, no `search_web` tool, no Go HTTP client — the search provider lives in the agent and is swapped in agent config with zero LucidVault change.
- Honors native-first (ADR-023): no duplicate web tool competes with Hermes's `web_search`.
- `AGENTS.md` gains a configurable `## Web Search` section selected by `AGENT_WEB_SEARCH_STRATEGY` (`off` | `fallback` | `time-sensitive` | `immediately`; default `fallback`). The generated prose names no provider.
- Synthesis rule encoded as prose: the curated vault (wiki pages) is weighted **above** web results by default, but **recency overrides** for time-sensitive questions (a fresher web result beats a stale wiki page, and the staleness must be flagged). Every source is cited per the existing Source Attribution rules (vault → original source URL; web → `[title](url)` naming the provider).
- The strategy is advisory prose, not code-enforced — consistent with ADR-015 ("the vault does not reason for the agent").
- `off` omits **all** web-search instructions from `AGENTS.md` (both the `## Web Search` section and the web-search bullet under Source Attribution).
- Portability cost: an MCP-only client with no web search of its own gets none from LucidVault. Revisit a gated, provider-abstracted tool only if such a client actually appears.
