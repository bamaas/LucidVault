# Plan — Agent Web Search Strategy

## Goal

Let LucidVault tell an AI agent *how and when* to use the agent's **own** web
search relative to the curated vault — without LucidVault providing, proxying, or
wrapping any web search service. The instruction is provider-agnostic (works with
whatever search tool the agent is configured with), configurable via an
environment variable, and can be turned off entirely.

This replaces the original "add a Tavily-backed `search_web` MCP tool" idea, which
was rejected during grilling (see Background).

## Background & key decisions

Captured during `/grill-with-docs`. Full reasoning in the ADRs.

- **No `search_web` tool in LucidVault** (ADR-024). The consuming agent (Hermes)
  already has a native `web_search`; wrapping a provider would duplicate it
  (violating native-first / ADR-023) and couple LucidVault to a fast-churning
  search-provider market (Tavily/Exa/Brave/Firecrawl/fastCRW/Scavio/…). Web search
  is the *agent's* answer-time tool; LucidVault owns acquisition + the vault
  interface, not the agent's toolkit.
- **LucidVault instructs strategy, not provider.** `AGENTS.md` already carries a
  provider-agnostic `## Web Search` section (#71) and source-attribution rules
  requiring the original source URL (#74). This feature makes the strategy
  *configurable* and adds a trust-vs-recency rule.
- **Deployment is native-first** (`MCP_READ_TOOLS=false`, in-process MCP on
  `127.0.0.1:8080`): Hermes reads vault files directly and uses MCP only for graph
  + writes. The strategy must therefore govern "native vault reads + the agent's
  own web search," naming no MCP tool.
- **Companion change** (ADR-025, **separate commit**): reduce the injected
  `CLAUDE.md` section to a pointer ("vault at `<abs path>`; read its `AGENTS.md`
  and follow it"), eliminating the static retrieval-strategy duplicate that would
  otherwise drift.

## Scope

### In scope
- New env var `AGENT_WEB_SEARCH_STRATEGY`: `off` | `fallback` | `time-sensitive` |
  `immediately`. Default `fallback`.
- `AGENTS.md` generator emits the mode-appropriate `## Web Search` section, plus
  the trust-vs-recency synthesis rule; `off` omits **all** web-search instructions.
- Provider-agnostic wording — the generated `AGENTS.md` names no search vendor.
- Docs: README env table, CLAUDE.md env section, `.env.example`.
- Companion (separate commit): CLAUDE.md injection → pointer (ADR-025).

### Out of scope
- Any `search_web` MCP tool, Tavily/other client, or new Go dependency.
- Changing the scraper/acquisition layer (Jina Reader stays — ADR-005). The
  "evaluate fastCRW / topic-driven source discovery" idea is parked as a separate
  future feature; no evidence Jina is underperforming.
- Code-level enforcement of the strategy (it is advisory prose, per ADR-015).

## Strategy modes (generated `## Web Search` wording)

All modes share the synthesis + citation rules; only the **first directive**
changes. No provider is ever named.

- **`off`** — no `## Web Search` section is emitted, and the "Web search" bullet is
  omitted from `## Source Attribution`. AGENTS.md contains zero web-search guidance.
- **`fallback`** (default) — "Reach for your own web search only when the vault does
  not cover the question." (preserves current #71 behavior.)
- **`time-sensitive`** — "Use your own web search when the question is
  time-sensitive (latest/current/versions/news/prices/dates beyond your saved
  content), or when the vault does not cover it."
- **`immediately`** — "For any substantive or factual question, use your own web
  search and the vault in parallel." (Most aggressive; opt-in. Documented
  trade-off: spends a web call per substantive query and partially tensions the
  template's 'you are an agent, use judgment' framing.)

Shared rules appended in every non-`off` mode:
- Weight the curated vault (wiki pages) **above** web results by default (higher
  trust). **But** when the question is time-sensitive and a web result is newer than
  the matching wiki page, lead with the web result and flag the vault page as
  possibly outdated. (trust-vs-recency override)
- Use your own configured web search — LucidVault does not provide one.
- Cite every source (see Source Attribution): vault → its original source URL;
  web → `[title](url)`, naming the provider.

## Implementation notes (for `/deliver`)

Mirror the existing `readTools` threading (ADR-023) — an env-derived value passed
into the generator so output always matches configuration.

- `cmd/main.go` (`loadConfig`): read `AGENT_WEB_SEARCH_STRATEGY`; validate against
  the four values; unknown/empty → `fallback` (+ `slog.Warn` on unknown non-empty).
  Add to the `config` struct; pass into `generateAgentsMD`.
- `internal/agentsmd/agentsmd.go`: `Generate` gains a strategy parameter (a small
  typed enum). It assembles the `## Web Search` section (mode-specific) and the
  Source-Attribution web bullet, omitting both when `off`.
- `internal/agentsmd/template.md`: move the static `## Web Search` section and the
  "Web search" attribution bullet **out** of the static template into generator
  logic so they become conditional/configurable (the rest of the template is
  unchanged).
- **Companion commit** — `internal/claudemd/claudemd.go`: shrink `sectionTemplate`
  to the pointer form (keep markers, `Upsert`, `CLAUDE_MD_PATH`, the abs-path
  `Sprintf`).
- Docs: `README.md` (env table + adjust the CLAUDE.md-integration row),
  `CLAUDE.md` (env section), `.env.example` (add `AGENT_WEB_SEARCH_STRATEGY`).

## Edge cases

- Unknown / empty `AGENT_WEB_SEARCH_STRATEGY` → `fallback` (+ warn on unknown).
- `off` must remove **every** web-search instruction (section *and* attribution
  bullet) — verified by asserting the substring "web search" is absent.
- Generated `AGENTS.md` must name **no** provider in any mode (assert no
  "tavily"/vendor strings).
- `AGENTS.md` is regenerated each poll cycle; `WriteIfChanged` rewrites it the
  cycle after the env var changes, and is a no-op otherwise.
- CLAUDE.md: if the host file is absent (no bind-mount), the upsert is silently
  skipped (unchanged from ADR-008); the pointer still interpolates the abs vault
  path.

## ADRs

- **ADR-024** — Web Search Belongs to the Agent, Not LucidVault (new).
- **ADR-025** — Reduce CLAUDE.md Injection to a Pointer (supersedes ADR-008).
- Relates to: ADR-015 (MCP retrieval primitives / AGENTS.md), ADR-023
  (native-first read-tool gating), ADR-005 (Jina scraping — unchanged).

## Acceptance criteria

1. `AGENT_WEB_SEARCH_STRATEGY` parses `off|fallback|time-sensitive|immediately`;
   default `fallback`; unknown non-empty value falls back to `fallback` with a
   logged warning.
2. Generated `AGENTS.md` contains the correct first-directive wording per mode and
   the trust-vs-recency rule in all non-`off` modes.
3. `off` produces an `AGENTS.md` with no `## Web Search` section and no web-search
   attribution bullet (no "web search" guidance anywhere).
4. Generated `AGENTS.md` names no search provider/vendor in any mode.
5. No `search_web` tool, no new Go dependency; `RegisteredTools` / the MCP tool
   surface are unchanged.
6. Companion commit: the injected `CLAUDE.md` section is the pointer form (abs vault
   path + "read AGENTS.md and follow it"), with no retrieval steps or file legend;
   markers preserved; idempotent re-run.
7. `mise run test` and `mise run lint` pass; new behavior covered by tests
   (`agentsmd` per-mode output, `cmd` env parsing, `claudemd` pointer content).
8. Docs updated: README, CLAUDE.md, `.env.example`.
