# Plan: Agent-Based Retrieval via Hermes

> Depends on: edges table (delivered in vault-hygiene, already implemented)

## Problem

Querying the vault from mobile is limited. Current setup (OpenWebUI + MCP tools) relies on the calling LLM to orchestrate multi-step retrieval via tool calls. This has three weaknesses:

1. **Keyword-bound retrieval** — `search_index` matches against slug, title, and tags. Misses conceptually related pages that use different terminology.
2. **No reactive exploration** — MCP tools return results, but the calling LLM decides the search strategy upfront. It cannot adapt based on what it finds mid-search (without chaining 6+ tool calls correctly).
3. **Model-dependent quality** — weak models (typical on mobile) make poor tool-calling decisions. Strong models work but cost more and take longer per query.

An AI agent with direct file access resolves all three: it reads files, follows hunches, greps with refined terms, and adapts its strategy with each file it reads.

---

## Solution: Hermes Agent + MCP Hybrid

### Architecture

```text
┌─────────────────────────────────────────────────────────┐
│                    USER DEVICES                          │
│  Phone (Telegram, WhatsApp, Signal)                     │
│  Desktop (CLI, Discord, Slack)                          │
└──────────────────────┬──────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────┐
│                 HERMES AGENT (daemon)                    │
│                                                         │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │   Gateway    │  │   Memory     │  │    Skills     │  │
│  │ Telegram     │  │ USER.md      │  │ Auto-created  │  │
│  │ WhatsApp     │  │ MEMORY.md    │  │ from usage    │  │
│  │ Signal       │  │ Cross-session│  │ Self-improving │  │
│  │ Discord      │  │ FTS5 search  │  │ Compounding   │  │
│  │ Slack        │  │              │  │               │  │
│  └─────────────┘  └──────────────┘  └───────────────┘  │
│                                                         │
│  Retrieval: DIRECT FILE ACCESS (read-only)              │
│  ┌────────────────────────────────────────────────────┐ │
│  │  grep wiki/ notes/ raw/   — full-text search       │ │
│  │  read any .md file        — direct content access  │ │
│  │  ls / tree                — browse vault structure  │ │
│  │  read index.md            — topic discovery        │ │
│  │  read AGENTS.md           — vault rules + tools    │ │
│  │  follow [[wikilinks]]     — manual graph traversal │ │
│  └────────────────────────────────────────────────────┘ │
│                                                         │
│  Mutations + Computed Data: MCP SERVER (tool calls)     │
│  ┌────────────────────────────────────────────────────┐ │
│  │  READ TOOLS                                        │ │
│  │  ├── search_index(query)      — structured search  │ │
│  │  ├── related_notes(slug)      — bidirectional      │ │
│  │  ├── expand_graph(seeds,hops) — multi-hop cluster  │ │
│  │  └── vault_overview()         — stats + context    │ │
│  │                                                    │ │
│  │  WRITE TOOLS                                       │ │
│  │  ├── add_bookmark(url,title,tags)                  │ │
│  │  ├── add_note(title,content,tags)                  │ │
│  │  ├── update_wiki(slug,section,content)             │ │
│  │  └── delete_page(slug)                             │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────┬──────────────────────────────────┘
                       │ MCP (Streamable HTTP)
                       ▼
┌─────────────────────────────────────────────────────────┐
│              LUCIDVAULT MCP SERVER                       │
│  Pure Go · SQLite (edges table) · Format enforcement    │
│                                                         │
│  Guarantees:                                            │
│  • YAML frontmatter format                              │
│  • Slug conventions                                     │
│  • Index.md consistency                                 │
│  • Edge table sync on every mutation                    │
│  • Path traversal protection                            │
│  • AGENTS.md generation (auto-updated)                  │
└──────────────────────┬──────────────────────────────────┘
                       │ filesystem
                       ▼
┌─────────────────────────────────────────────────────────┐
│                  OBSIDIAN VAULT                          │
│  wiki/      — LLM-enriched summaries (primary knowledge)│
│  raw/       — original scraped content (immutable)      │
│  notes/     — personal notes                            │
│  index.md   — master catalog                            │
│  soul.md    — user profile                              │
│  AGENTS.md  — auto-generated agent instructions         │
│  inbox/     — pending URLs (pipeline input)             │
└─────────────────────────────────────────────────────────┘
```

### Why Hermes (not OpenClaw, Khoj, or custom)

| Decision | Rationale |
|----------|-----------|
| Hermes over OpenClaw | Self-improving learning loop, persistent cross-session memory + user modeling, cleaner security record (0 CVEs vs 6). OpenClaw has bigger ecosystem but no cross-session learning. |
| Hermes over Khoj | Khoj is RAG (indexes then retrieves from embeddings). Hermes is agent (free file access, reactive exploration). Agent retrieval finds 30-50% more relevant content on complex queries. |
| Hermes over custom agent | Hermes already has 16+ messaging integrations, skill system, memory, MCP support. Building this from scratch would duplicate existing open-source work. |
| Agent reads files directly | Direct file access enables reactive exploration: read → discover → refine → read more. MCP tools constrain to predefined search strategies. |
| Agent writes through MCP only | Vault files have strict structure. One code path enforces invariants. Multiple clients share the same write API. |

### Why Agent Retrieval is Better Than MCP Tool Retrieval

The core difference: **an agent decides what to read next based on what it already found.**

MCP tool retrieval is like searching a library catalog — you get what matches your keywords. Agent retrieval is like browsing the shelves — you find things you didn't know to search for.

**Retrieval strategy comparison:**

```text
MCP TOOL RETRIEVAL (fixed strategy)
═══════════════════════════════════

User: "What do I know about service mesh alternatives to Istio?"

    ┌──────────────┐
    │ search_index  │──→ "service mesh" → finds: istio-overview, linkerd-setup
    │ (keyword)     │──→ "istio alternatives" → finds: nothing
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │  read_wiki    │──→ reads istio-overview, linkerd-setup
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │  synthesize   │──→ answer from 2 pages
    └──────────────┘

    Result: PARTIAL — missed Cilium (tagged "ebpf", not "service mesh")
    Tool calls: 4  |  Pages found: 2


AGENT RETRIEVAL (reactive exploration)
══════════════════════════════════════

User: "What do I know about service mesh alternatives to Istio?"

    ┌──────────────┐
    │ read index.md │──→ scans full list, spots 5 candidates by title+tags
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │ grep wiki/    │──→ "service mesh" → 8 file matches
    │ (full text)   │──→ "istio" → cross-references, finds comparison page
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │ read top 4    │──→ reads pages, notices [[cilium-networking]] link
    │ wiki pages    │    "hmm, Cilium is mentioned here, let me check"
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │ follow link   │──→ reads cilium-networking (found via exploration!)
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │ grep notes/   │──→ checks for personal notes on the topic
    └──────┬───────┘
           ▼
    ┌──────────────┐
    │  synthesize   │──→ answer from 6 pages, including ones keyword search missed
    └──────────────┘

    Result: COMPREHENSIVE — found Cilium through link-following
    Pages found: 6  |  Adapted strategy mid-search
```

**When the gap is biggest:**

- Vague queries: "that article about the German guy's talk" — agent greps patterns, scans dates, reads speculatively
- Multi-topic synthesis: "compare X vs Y" — agent reads both clusters naturally
- Exploratory: "what's interesting about networking?" — agent can browse and judge

**When the gap is smallest:**

- Precise factual: "what is Raft?" — keyword search finds it immediately

### AGENTS.md — Auto-Generated Agent Instructions

LucidVault generates and maintains `AGENTS.md` in the vault root. This file instructs any agent (Hermes, Claude Code, etc.) on how to interact with the vault efficiently.

**Design: static template + dynamic sections.**

- **Static sections** (hand-authored, version-controlled in `internal/agentsmd/template.md`):
  - Vault Access Rules (read/write boundaries)
  - Retrieval Strategy (search patterns, fallback order)
  - Content Guidelines (raw/ size warnings, wiki preference)
- **Dynamic sections** (generated at runtime):
  - Available MCP Tools — introspected from registered tools with descriptions and parameters
  - Vault Stats — page counts per section, edge count, tag frequency, last pipeline run

**Generation behavior:**

- Written to `{vaultPath}/AGENTS.md` on startup and each poll cycle
- Idempotent — only writes if content changed (avoids unnecessary file churn for LiveSync)
- Package: `internal/agentsmd/`

**Why LucidVault generates it (not hand-maintained):**

- Tool surface stays in sync automatically as MCP tools are added/removed
- Vault stats give agents instant orientation without needing `vault_overview` call
- Single source of truth — no drift between what MCP server offers and what docs claim

---

## Retrieval Flow Diagrams

### Flow 1: Simple Factual Query

```text
User (Telegram): "What is Raft consensus?"

Hermes:
  1. grep wiki/ "raft"           → finds wiki/raft-consensus.md
  2. read wiki/raft-consensus.md → has answer
  3. reply to user with answer + citation

Total: 2 reads, <5 seconds
```

### Flow 2: Exploratory Query

```text
User (WhatsApp): "What do I know about Kubernetes networking?"

Hermes:
  1. read index.md                        → spots 4 candidates
  2. grep wiki/ "kubernetes.*network"     → 6 matches
  3. read wiki/kubernetes-networking.md   → core page
  4. read wiki/kubernetes-cni.md          → linked from core page
  5. expand_graph(["kubernetes-networking"], hops=2)  → MCP
     → returns: cilium-networking, calico-setup, service-mesh-comparison
  6. read wiki/cilium-networking.md       → relevant, include
  7. read wiki/service-mesh-comparison.md → relevant, include
  8. grep notes/ "kubernetes"             → finds personal note
  9. synthesize answer from 5 wiki pages + 1 note

Total: 7 reads + 1 MCP call, ~10 seconds
```

### Flow 3: Save from Mobile

```text
User (Telegram): "Save this article: https://example.com/good-article"

Hermes:
  1. add_bookmark(url, title, tags)  → MCP
  2. Reply: "Saved to inbox. Pipeline will scrape and enrich it next cycle."

Total: 1 MCP call, <2 seconds
```

---

## Edge Cases

### Hermes writes files directly despite rules

**Risk:** AGENTS.md says "never write directly" but Hermes has fs write capability.
**Handling:** Mount vault read-only in Hermes workspace config if possible. If not, rely on instructions (Hermes is instruction-following).

### MCP connection loss

**Risk:** If MCP server is down, Hermes can still read but cannot write.
**Handling:** Hermes reports "LucidVault MCP unavailable, read-only mode" to user. Queue write intentions in memory for retry.

### Hermes reads stale file content

**Risk:** Pipeline enriches a page while Hermes is mid-query. Hermes has old content in context.
**Handling:** Acceptable at this scale. Vault changes are infrequent (poll-cycle driven). If accuracy matters, Hermes re-reads before answering.

### Large file in raw/

**Risk:** Raw files can be very large (full article scrapes). Hermes reads one and burns context.
**Handling:** AGENTS.md instructs: "Prefer wiki/ pages. Only read raw/ when wiki summary lacks detail. Read first 200 lines of raw files, not full content."

### Hermes skill creates bad retrieval pattern

**Risk:** Auto-created skill encodes a poor strategy that gets reused.
**Handling:** Hermes' Curator system includes rubric-based self-improvement. Bad skills get low scores and are refined or deprecated. User can also manually prune skills.

### Multiple agents querying simultaneously

**Risk:** Hermes + Claude Code + OpenWebUI all query via MCP at once.
**Handling:** MCP server is stateless per request. SQLite handles concurrent reads. Writes serialize via SQLite locking. No issue at expected concurrency (<5 simultaneous queries).

### AGENTS.md conflicts with LiveSync

**Risk:** LucidVault regenerates AGENTS.md while LiveSync is syncing.
**Handling:** Only write when content changes (content-hash comparison). Writes go through `WithFileLock` for cross-process safety. LiveSync conflict resolution handles rare races (last-write-wins is fine for generated files).

---

## Implementation Order

### Phase 1: MCP Read Tool Enhancements (in LucidVault)

1. Implement `ExpandGraph` with recursive CTE in `store.Store`.
2. Add `expand_graph` MCP tool.
3. Modify `related_notes` to use bidirectional edge lookups from SQLite (pass `*store.Store` to handler).
4. Add `vault_overview` MCP tool.

### Phase 2: AGENTS.md Generation (in LucidVault)

5. Create `internal/agentsmd/` package.
6. Write static template (`internal/agentsmd/template.md`) with vault access rules and retrieval strategy.
7. Implement dynamic section generation: introspect registered MCP tools, query vault stats.
8. Generate `AGENTS.md` on startup and each poll cycle (idempotent, content-hash check).

### Phase 3: Hermes Agent Setup (outside LucidVault codebase)

9. Install Hermes Agent on vault host.
10. Configure workspace pointing at vault path (read-only mount if possible).
11. Connect LucidVault MCP server as MCP endpoint in Hermes config.
12. Hermes reads `AGENTS.md` from vault root for instructions.
13. Connect Telegram + WhatsApp channels via Hermes gateway.
14. Write initial `vault-query` skill as a starting point for Hermes to build on.

### Phase 4: Validation

15. Test retrieval quality: run 20 representative queries, compare agent vs MCP-only results.
16. Test mutation safety: verify all writes go through MCP, no direct file modifications.
17. Test edge cases: MCP down, large files, concurrent queries.
18. Let Hermes build skills over 2 weeks of real usage, review quality.

---

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Retrieval agent | Hermes Agent | Self-improving, persistent memory, MCP support, 16+ chat integrations |
| Agent file access | Read: direct, Write: MCP only | Best retrieval quality + vault structure safety |
| Vector/semantic search | Deferred | Graph + keyword + agent exploration covers 80%+ of queries. Add embeddings later if retrieval gaps emerge. |
| LucidVault code changes | MCP tools + AGENTS.md generation | Agent is external. LucidVault stays minimal — pipeline + MCP + agent instructions. |
| AGENTS.md ownership | LucidVault generates it | Tool surface and vault stats stay in sync automatically. No manual maintenance drift. |
| AGENTS.md structure | Static template + dynamic sections | Rules rarely change (hand-authored). Tool list + stats change with code (auto-generated). |

## Research Sources

- **Hermes Agent** (github.com/NousResearch/hermes-agent) — self-improving AI agent, persistent memory, 16+ messaging integrations, MCP support, skill learning loop.
- **OpenClaw** (github.com/openclaw/openclaw) — evaluated as alternative. Bigger ecosystem (345K stars) but no cross-session learning.
- **Khoj** (github.com/khoj-ai/khoj) — evaluated. RAG-based (index + embed), not agent-based. Less flexible retrieval.
- **claude-obsidian** (github.com/AgriciDaniel/claude-obsidian) — hot cache pattern (inspired vault_overview), query modes (quick/standard/deep).
