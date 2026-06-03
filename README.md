# LucidVault

## Transform saved links into connected insights

You save dozens of articles, blog posts, and links every week. Most of them disappear into a bookmark graveyard — never read, never searchable, never connected to anything. LucidVault fixes that.

LucidVault turns URLs into a structured, searchable knowledge base inside your Obsidian vault. Drop a URL into the inbox folder — or let Raindrop.io feed it automatically — and LucidVault scrapes the full content, summarizes it with an LLM, extracts key takeaways, and links it to your existing notes. Your personal notes live alongside enriched pages, and a `soul.md` file personalizes everything to your background and interests.

LucidVault can inject a retrieval strategy section into your `~/.claude/CLAUDE.md`, so Claude Code knows how to query your knowledge base efficiently, making it a daily companion for development work.

## Features

- **Inbox** — Drop a `.md` file with a URL into `inbox/` and it gets scraped, enriched, and added to your vault. Works standalone — no external service required
- **Raindrop.io integration** — Optionally connect Raindrop.io to auto-feed bookmarks into the inbox. Backfills all existing bookmarks on first run
- **Enrich** — LLM (Ollama Cloud, free) generates a wiki-style summary with key takeaways, tags, and wiki-links to related pages
- **Retrieve** — Built-in Claude Code integration with a tiered lookup strategy (index → wiki → raw) that keeps token usage low
- **MCP server** — Built-in MCP server exposes the vault as structured retrieval primitives for any AI client (Claude Code, Cursor, Windsurf, OpenClaw). Supports stdio and Streamable HTTP transports
- **Notes indexing** — Personal notes in `notes/` are automatically scanned and get a wiki copy in `wiki/` with tags. Notes without tags are auto-tagged via the LLM; notes with existing tags keep them as-is
- **Multi-device sync** — Access your vault from any device (phone, tablet, laptop) using [Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync). Self-hosted via CouchDB — zero LucidVault code changes needed

## Getting started

### Prerequisites

- Docker
- An [Ollama Cloud](https://ollama.com) account (free)
- (Optional) A [Raindrop.io](https://raindrop.io) account (free) — for automatic bookmark syncing

### 1. Get your API tokens

- **Ollama Cloud**: Go to <https://ollama.com/settings/keys> → create an API key.
- **Raindrop** (optional): Go to <https://app.raindrop.io/settings/integrations> → create a test token. These don't expire for personal use.

### 2. Prepare your vault directory

Create a directory that will hold your knowledge base. If you already use Obsidian, point to your existing vault.

```bash
mkdir -p ~/lucid-vault
```

### 3. (Optional) Create a soul.md

`soul.md` personalizes your entire LucidVault experience. It's used during enrichment (tailoring summaries to your interests) and during retrieval (Claude Code reads it to tailor answers to your background). Place it at the root of your vault:

```bash
cat > ~/lucid-vault/soul.md << 'EOF'
# Soul

## Who I am
DevOps/platform engineer. Mostly Go and Kubernetes.

## What I care about
- Distributed systems, infrastructure patterns
- Developer experience and tooling
- AI/LLM applied to engineering workflows

## How to enrich
- Prefer practical takeaways over theory
- Infrastructure > frontend
- Flag contrarian or surprising claims explicitly

## How to respond
- Be direct, no fluff
- Link to related notes when answering
- Say "I don't have notes on this" rather than guessing
EOF
```

Edit this to reflect your background and interests. If you skip this step, everything still works — just without personalization.

### 4. Run the container

**Inbox-only mode** (no Raindrop):

```bash
docker run -d \
  --name lucidvault \
  --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest
```

**With Raindrop.io** (auto-feeds bookmarks into inbox):

```bash
docker run -d \
  --name lucidvault \
  --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -e RAINDROP_ACCESS_TOKEN=<your-raindrop-token> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest
```

LucidVault polls every 5 minutes. Drop `.md` files containing URLs into `inbox/` to process them. With Raindrop enabled, bookmarks are automatically synced to the inbox.

## Optional: Claude Code integration

To let LucidVault inject a retrieval strategy into your Claude Code config, add the `CLAUDE.md` bind-mount:

```bash
touch ~/.claude/CLAUDE.md  # ensure the file exists before mounting

docker run -d \
  --name lucidvault \
  --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  -v ~/.claude/CLAUDE.md:/CLAUDE.md \
  ghcr.io/bamaas/lucidvault:latest
```

### 5. Check it's working

```bash
docker logs -f lucidvault
```

You should see bookmarks being fetched, scraped, and enriched. Files appear in your vault under `raw/` (scraped content) and `wiki/` (enriched pages).

## Configuration

Environment variables configure the service. CLI flags control one-off operations.

### Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OLLAMA_API_KEY` | Yes | — | Ollama Cloud API key (free) |
| `VAULT_PATH` | Yes | `/vault` (Docker) | Path to vault |
| `RAINDROP_ACCESS_TOKEN` | No | — | Enables Raindrop.io as an inbox feeder. When set, bookmarks are synced to `inbox/` automatically. |
| `OLLAMA_MODEL` | No | `qwen3.5` | LLM model for enrichment |
| `POLL_INTERVAL` | No | `5m` | How often to check for new inbox items |
| `ENRICH_DELAY_MS` | No | `500` | Delay between API calls (rate limiting) |
| `ENRICH_MAX_RETRIES` | No | `3` | Max retries on API failure |
| `SUPADATA_API_KEY` | No | — | [Supadata](https://supadata.ai) API key for YouTube transcript extraction. When set, YouTube URLs are routed to Supadata instead of Jina. |
| `HYGIENE_INTERVAL` | No | `10` | Run vault hygiene (broken edge cleanup, index sync, raw/wiki consistency) every Nth poll cycle |
| `MCP_HTTP_ADDR` | No | — | Serve the MCP server over HTTP in-process with the pipeline (e.g. `:8080`). Empty disables it. See [Exposing MCP over HTTP](#exposing-mcp-over-http). |
| `MCP_ALLOWED_HOST` | No | `localhost,127.0.0.1` | Comma-separated Host-header allowlist (DNS-rebinding guard). `*` or empty disables the guard — needed in Kubernetes. |
| `CLAUDE_MD_PATH` | No | `/CLAUDE.md` | Path to CLAUDE.md for Claude Code integration (override only if needed) |

### CLI flags

| Flag | Description |
|------|-------------|
| `--re-enrich` | Re-enrich all bookmarks using existing raw content, then exit. Useful after changing the enrichment prompt or model. Does not re-scrape. |
| `--re-fetch` | Re-fetch all bookmarks from external sources (e.g. Raindrop.io) to inbox, bypassing dedup. Items flow through the full pipeline (scrape + enrich), then exit. Requires `RAINDROP_ACCESS_TOKEN`. |

```bash
# Re-enrich all bookmarks (uses existing raw content, does not re-scrape)
docker run --rm \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest --re-enrich

# Re-fetch all bookmarks from Raindrop (re-scrape + re-enrich)
docker run --rm \
  -e OLLAMA_API_KEY=<your-key> \
  -e RAINDROP_ACCESS_TOKEN=<your-token> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest --re-fetch
```

## Deployment options

### Local Docker (default)

The quickest way to run LucidVault — a single container with your vault mounted as a volume. See [Getting started](#getting-started) above.

### Docker Compose with LiveSync

For multi-device access via Obsidian LiveSync. Brings up LucidVault, [livesync-bridge](https://github.com/vrtmrz/livesync-bridge), and CouchDB with shared volumes.

1. Copy `.env.example` to `.env` and fill in your API keys and CouchDB credentials
2. Review `deploy/livesync-bridge/config.json` — CouchDB credentials must match `.env`
3. Start the stack:

```bash
docker compose up -d
```

4. Install the [Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync) plugin on each device
5. Configure the plugin to point at your CouchDB instance (the URL, database name, and credentials from your `.env`)
6. First sync: the bridge automatically detects existing vault files and syncs them to CouchDB

**Folder ownership:**

- `inbox/`, `notes/`, `soul.md` — yours to edit in Obsidian
- `wiki/`, `index.md` — generated by LucidVault, will be overwritten on re-enrichment (read-only in Obsidian)
- `raw/` — generated by LucidVault, synced but large; consider excluding on mobile

See `docs/design/017-couchdb-livesync.md` for the full design document.

## Vault structure

LucidVault creates and manages these directories inside your vault:

```text
vault/
├── inbox/        # Drop .md files with URLs here to process them
├── raw/          # Immutable scraped content (don't edit)
├── wiki/         # LLM-generated wiki pages (don't edit — overwritten on re-enrichment/note changes)
├── notes/        # Your personal notes (yours to write freely; wiki copies are auto-generated)
├── templates/    # Obsidian templates
├── index.md      # Master catalog of all wiki pages
├── soul.md       # Your profile for LLM personalization (optional, you create this)
└── .lucidvault.db  # SQLite state database
```

## Querying your vault with Claude Code

When `~/.claude/CLAUDE.md` is bind-mounted into the container, LucidVault upserts a retrieval strategy section into it at startup.

Claude Code is instructed to:

1. Read `soul.md` first to tailor responses to the user's background
2. Grep `index.md` for keywords — never read the full index
3. Read matching `wiki/` pages (enriched summaries)
4. Search `notes/` by keyword for personal context
5. Fall back to `raw/` only if wiki and notes lack detail (large files)
6. Offer to fetch a URL as a last resort, either one found in a vault page or provided by the user

It will never scan entire directories, and will not search the web unprompted.

### MCP server

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

#### Exposing MCP over HTTP

The standalone `lucidvault mcp` subcommand is a short-lived process. To serve MCP continuously for an always-on client (e.g. OpenWebUI), run the HTTP server **in-process** alongside the pipeline by setting `MCP_HTTP_ADDR`. Both share one SQLite connection pool — there is no second container and no move off SQLite, which keeps writes safe (a single process touching a single volume sidesteps SQLite's unreliable locking over networked filesystems).

Set `MCP_HTTP_ADDR=:8080` and the running daemon will additionally answer MCP requests on port 8080.

**Local Docker** — publish the port on loopback only and rely on the Host-header guard (DNS-rebinding defense). In `docker-compose.yml`, uncomment the loopback port publish:

```yaml
    ports:
      - "127.0.0.1:8080:8080"
```

Leave `MCP_ALLOWED_HOST` at its default (`localhost,127.0.0.1`). Never bind `0.0.0.0`.

**Kubernetes** — run LucidVault as a single Deployment (RWO PVC) and expose MCP via a **ClusterIP Service** (never an Ingress). Restrict access with a NetworkPolicy allowing ingress only from the client pod (e.g. OpenWebUI). Because the request `Host` is then the Service DNS name, either add that name to `MCP_ALLOWED_HOST` or disable the guard with `MCP_ALLOWED_HOST=*` and rely on the NetworkPolicy. (No k8s manifests ship in this repo yet — this is guidance only.)

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

## Development workflow

Feature development is driven by Claude Code commands and documented in the repo for future reference.

1. **Grill** — Run `/grill-with-docs` with your feature idea. This stress-tests the idea against the codebase, sharpens domain terminology in `CONTEXT.md`, creates ADRs for trade-offs, and produces a detailed plan in `docs/plans/`.
2. **Decompose (large features)** — Run `/decompose docs/plans/plan-<feature>.md` to break the plan into numbered, context-window-sized sub-plans in `docs/plans/plan-<feature>/`.
3. **Deliver** — Run `/deliver docs/plans/plan-<feature>.md`. This automates the full cycle:
   - **Single mode** (no sub-plans): implement (TDD) → test quality → review → PR
   - **Multi mode** (with sub-plans): loops implement → test → review per sub-plan, single PR at end
4. **Reference later** — Plans, sub-plans, `CONTEXT.md`, and ADRs are committed to the repo. Use them to understand why a feature was built, what edge cases were considered, and what trade-offs were made.

For small fixes or changes that don't need a plan, work directly and commit with [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).

## To do

_No pending items._
