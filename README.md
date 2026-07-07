# LucidVault

## Transform notes and bookmarks into connected insights

You save dozens of articles, blog posts, and links every week. Most of them disappear into a bookmark graveyard - never read, never searchable, never connected to anything. LucidVault fixes that.

LucidVault turns URLs into a structured, searchable knowledge base inside your Obsidian vault. Drop a URL into the inbox folder - or let Raindrop.io feed it automatically - and LucidVault scrapes the full content, summarizes it with an LLM, extracts key takeaways, and links it to your existing notes. Your personal notes live alongside enriched pages, and a `soul.md` file personalizes everything to your background and interests.

LucidVault can inject a retrieval strategy section into your `~/.claude/CLAUDE.md`, so Claude Code knows how to query your knowledge base efficiently, making it a daily companion for development work.

## Features

- **Inbox** - Drop a `.md` file with a URL into `inbox/` and it gets scraped, enriched, and added to your vault. Works standalone - no external service required
- **Raindrop.io integration** - Optionally connect Raindrop.io to auto-feed bookmarks into the inbox. Backfills all existing bookmarks on first run
- **Enrich** - LLM (Ollama Cloud, free) generates a wiki-style summary with key takeaways, tags, and wiki-links to related pages
- **Retrieve** - Built-in Claude Code integration with a tiered lookup strategy (index → wiki → raw) that keeps token usage low
- **MCP server** - Built-in MCP server exposes the vault as structured retrieval primitives for any AI client (Claude Code, Cursor, Windsurf, OpenClaw). Supports stdio and Streamable HTTP transports
- **Notes indexing** - Personal notes in `notes/` are automatically scanned and get a wiki copy in `wiki/` with tags. Notes without tags are auto-tagged via the LLM; notes with existing tags keep them as-is
- **Multi-device sync** - Access your vault from any device (phone, tablet, laptop) using [Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync). Self-hosted via CouchDB - zero LucidVault code changes needed

## Getting started

You need Docker and a free [Ollama Cloud](https://ollama.com) API key. Then pick the
setup that fits - both build on the same container.

### Simple - single container

The pipeline scrapes and enriches URLs you drop in the inbox and writes your vault.
Best for a single machine, no sync or chat UI.

```bash
docker run -d --name lucidvault --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest
```

Full walkthrough - tokens, `soul.md`, Raindrop, Claude Code pointer:
**[Simple setup runbook →](docs/guides/simple-setup.md)**

### Full stack - Claude Code integration + browser chat + multi-device sync

Query, extend, and curate your knowledge base from **Claude Code** as you work, chat
with it in a browser, and read it on every device. The agents can only change the
vault through safe, auditable tools.

**[Full-stack runbook →](docs/guides/agent-chat/agent-chat-setup.md)**

## Configuration

Environment variables configure the service. CLI flags control one-off operations.

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OLLAMA_API_KEY` | Yes | - | Ollama Cloud API key (free) |
| `VAULT_PATH` | Yes | `/vault` (Docker) | Path to vault |
| `RAINDROP_ACCESS_TOKEN` | No | - | Enables Raindrop.io as an inbox feeder. When set, bookmarks are synced to `inbox/` automatically. |
| `OLLAMA_MODEL` | No | `qwen3.5` | LLM model for enrichment |
| `POLL_INTERVAL` | No | `5m` | How often to check for new inbox items |
| `ENRICH_DELAY_MS` | No | `500` | Delay between API calls (rate limiting) |
| `ENRICH_MAX_RETRIES` | No | `3` | Max retries on API failure |
| `SUPADATA_API_KEY` | No | - | [Supadata](https://supadata.ai) API key for YouTube transcript extraction. When set, YouTube URLs are routed to Supadata instead of Jina. |
| `HYGIENE_INTERVAL` | No | `10` | Run vault hygiene (broken edge cleanup, index sync, raw/wiki consistency) every Nth poll cycle |
| `MCP_HTTP_ADDR` | No | - | Serve the MCP server over HTTP in-process with the pipeline (e.g. `:8080`). Empty disables it. See [Exposing MCP over HTTP](#exposing-mcp-over-http). |
| `MCP_ALLOWED_HOST` | No | `localhost,127.0.0.1` | Comma-separated Host-header allowlist (DNS-rebinding guard). `*` or empty disables the guard - needed in Kubernetes. |
| `MCP_READ_TOOLS` | No | `false` | Expose the duplicate MCP content-read tools (`read_wiki`, `search_index`, `grep_vault`, `read_note`, `read_raw`, `vault_overview`, `get_soul`). Off by default so filesystem-capable agents read the vault directly; enable for clients that reach the vault only over MCP (no filesystem access). Graph and write tools are always available. |
| `AGENT_WEB_SEARCH_STRATEGY` | No | `fallback` | How the generated `AGENTS.md` tells an agent to use its **own** web search relative to the vault: `off` (no web-search guidance), `fallback` (only when the vault lacks coverage), `time-sensitive` (also for latest/current/news/price/date questions), `immediately` (web + vault in parallel for any substantive question). LucidVault never provides a web search; the prose names no provider. Unknown values fall back to `fallback`. |
| `CLAUDE_MD_PATH` | No | `/CLAUDE.md` | Path to CLAUDE.md for Claude Code integration (override only if needed) |

### CLI flags

| Flag | Description |
|------|-------------|
| `--re-enrich` | Re-enrich all bookmarks using existing raw content, then exit. Useful after changing the enrichment prompt or model. Does not re-scrape. |
| `--re-fetch` | Re-fetch all bookmarks from external sources (e.g. Raindrop.io) to inbox, bypassing dedup. Items flow through the full pipeline (scrape + enrich), then exit. Requires `RAINDROP_ACCESS_TOKEN`. |

## Vault structure

LucidVault creates and manages these directories inside your vault:

```text
vault/
├── inbox/        # Drop .md files with URLs here to process them
├── raw/          # Immutable scraped content (don't edit)
├── wiki/         # LLM-generated wiki pages (don't edit - overwritten on re-enrichment/note changes)
├── notes/        # Your personal notes (yours to write freely; wiki copies are auto-generated)
├── templates/    # Obsidian templates
├── index.md      # Master catalog of all wiki pages
├── soul.md       # Your profile for LLM personalization (optional, you create this)
└── .lucidvault.db  # SQLite state database
```

## MCP server

LucidVault includes a built-in MCP (Model Context Protocol) server that exposes the vault as structured retrieval primitives, inbox write tools, and vault mutation tools. Any MCP-compatible AI client (Claude Code, Cursor, Windsurf, OpenClaw) can query your knowledge base, submit bookmarks or notes, and manage vault content.

**Start the server:**

```bash
# Stdio transport (for Claude Code, Cursor)
lucidvault mcp

# Streamable HTTP transport (for remote clients, mobile)
lucidvault mcp --http :8080
```

**Available tools:**

| Tool | Description |
|------|-------------|
| `get_soul` | Read user profile (soul.md) |
| `search_index` | Search index for topics, titles, and tags |
| `read_wiki` | Read a curated wiki page |
| `grep_vault` | Search for exact terms (scoped to wiki/notes/raw) |
| `read_note` | Read a personal note |
| `read_raw` | Read original source content (fallback) |
| `related_notes` | Get bidirectional related pages (outbound, inbound, both) |
| `vault_overview` | Get vault stats: page counts, edge count, top tags, metadata |
| `expand_graph` | Expand seed slugs by traversing edges up to N hops |
| `add_bookmark` | Add a URL to the inbox for pipeline processing |
| `add_note` | Create a personal note in the knowledge base |
| `update_wiki` | Update a section of a wiki page (preserves other sections) |
| `edit_page` | Replace the whole body of a wiki page (preserves frontmatter, re-syncs edges) |
| `delete_page` | Delete a page and all artifacts (returns dangling refs) |

**Claude Code configuration** (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "lucidvault": {
      "command": "lucidvault",
      "args": ["mcp"],
      "env": {
        "VAULT_PATH": "/path/to/your/vault"
      }
    }
  }
}
```

### Exposing MCP over HTTP

The standalone `lucidvault mcp` subcommand is a short-lived process. To serve MCP continuously for an always-on client (e.g. OpenWebUI), run the HTTP server **in-process** alongside the pipeline by setting `MCP_HTTP_ADDR`. Both share one SQLite connection pool - there is no second container and no move off SQLite, which keeps writes safe.

Set `MCP_HTTP_ADDR=:8080` and the running daemon will additionally answer MCP requests on port 8080.

**Local Docker** - set `MCP_HTTP_ADDR=:8080` and publish the port on loopback only,
relying on the Host-header guard (DNS-rebinding defense). Add to the `lucidvault`
service in `docker-compose.yml`:

```yaml
    ports:
      - "127.0.0.1:8080:8080"
```

Leave `MCP_ALLOWED_HOST` at its default (`localhost,127.0.0.1`). Never bind
`0.0.0.0`. The [full-stack runbook](docs/guides/agent-chat/agent-chat-setup.md) wires
this up for you.

**Kubernetes** - run LucidVault as a single Deployment (RWO PVC) and expose MCP via a **ClusterIP Service** (never an Ingress). Restrict access with a NetworkPolicy allowing ingress only from the client pod (e.g. OpenWebUI). Because the request `Host` is then the Service DNS name, either add that name to `MCP_ALLOWED_HOST` or disable the guard with `MCP_ALLOWED_HOST=*` and rely on the NetworkPolicy. (No k8s manifests ship in this repo yet - this is guidance only.)

## Tech stack

| Component | Choice |
|-----------|--------|
| Language | Go |
| Web scraping | Jina Reader, Supadata (YouTube) |
| LLM | Ollama Cloud |
| Storage | Obsidian vault (markdown) |
| State | SQLite (modernc.org/sqlite) |
| Deployment | Docker / Docker Compose / static binary |
| Versioning | Commitizen (conventional commits, auto changelog) |
| Bookmark source | Inbox folder (+ optional Raindrop.io) |

## Contributing

Development workflow, plan/ADR conventions, and commit rules live in
[CONTRIBUTING.md](CONTRIBUTING.md).
