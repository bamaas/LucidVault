# LucidVault

## Transform saved links into connected insights

You save dozens of articles, blog posts, and links every week. Most of them disappear into a bookmark graveyard — never read, never searchable, never connected to anything. LucidVault fixes that.

LucidVault turns saved bookmarks into a structured, searchable knowledge base inside your Obsidian vault. It scrapes the full content, summarizes it with an LLM, extracts key takeaways, and links it to your existing notes — automatically. Your personal notes live alongside enriched pages, and a `soul.md` file personalizes everything to your background and interests.

LucidVault can inject a retrieval strategy section into your `~/.claude/CLAUDE.md`, so Claude Code knows how to query your knowledge base efficiently, making it a daily companion for development work.

## Features

- **Capture** — Save a bookmark on your phone, it appears in your vault within minutes: scraped, summarized, tagged, and linked
- **YouTube transcripts** — YouTube URLs are automatically detected and their transcripts fetched via the [Supadata](https://supadata.ai) API, then enriched like any other page
- **Enrich** — LLM generates a wiki-style summary with key takeaways, tags, and wiki-links to related pages
- **Retrieve** — Built-in Claude Code integration with a tiered lookup strategy (index → wiki → raw) that keeps token usage low
- **Resilient** — Falls back to basic metadata when scraping fails (paywalled sites, blocked content)
- **Notes indexing** — Personal notes in `notes/` are automatically scanned and get a wiki copy in `wiki/` with tags. Notes without tags are auto-tagged via the LLM; notes with existing tags keep them as-is. The `index.md` points to the wiki copy for consistent retrieval
- **Deletion sync** — Bookmarks deleted from the source are automatically cleaned up: wiki page, raw file, index entry, and DB record are all removed
- **Re-enrich** — Changed your enrichment prompt or model? Run with `--re-enrich` to re-process all bookmarks using existing raw content
- **Backfill** — Processes all your existing bookmarks on first run

## Getting started

### Prerequisites

- Docker
- A [Raindrop.io](https://raindrop.io) account (free)
- An [Ollama Cloud](https://ollama.com) account

### 1. Get your API tokens

- **Raindrop**: Go to <https://app.raindrop.io/settings/integrations> → create a test token. These don't expire for personal use.
- **Ollama Cloud**: Go to <https://ollama.com/settings/keys> → create an API key.

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

```bash
docker run -d \
  --name lucidvault \
  --restart unless-stopped \
  -e SOURCE_TOKEN=<your-raindrop-token> \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest
```

That's it. LucidVault will poll Raindrop every 5 minutes. On first run, it backfills all your existing bookmarks.

## Optional: Claude Code integration

To let LucidVault inject a retrieval strategy into your Claude Code config, add the `CLAUDE.md` bind-mount:

```bash
touch ~/.claude/CLAUDE.md  # ensure the file exists before mounting

docker run -d \
  --name lucidvault \
  --restart unless-stopped \
  -e SOURCE_TOKEN=<your-raindrop-token> \
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
| `SOURCE_TOKEN` | Yes | — | Access token for the bookmark source (falls back to `RAINDROP_ACCESS_TOKEN`) |
| `OLLAMA_API_KEY` | Yes | — | Ollama Cloud API key |
| `VAULT_PATH` | Yes | `/vault` (Docker) | Path to vault |
| `SOURCE_NAME` | No | `raindrop` | Bookmark source to use |
| `OLLAMA_MODEL` | No | `qwen3.5` | LLM model for enrichment |
| `POLL_INTERVAL` | No | `5m` | How often to check for new bookmarks |
| `ENRICH_DELAY_MS` | No | `500` | Delay between API calls (rate limiting) |
| `ENRICH_MAX_RETRIES` | No | `3` | Max retries on API failure |
| `SUPADATA_API_KEY` | No | — | [Supadata](https://supadata.ai) API key for YouTube transcript extraction. When set, YouTube URLs are routed to Supadata instead of Jina. |
| `CLAUDE_MD_PATH` | No | `/CLAUDE.md` | Path to CLAUDE.md for Claude Code integration (override only if needed) |

### CLI flags

| Flag | Description |
|------|-------------|
| `--re-enrich` | Re-enrich all bookmarks returned by the source using existing raw content, then exit. Useful after changing the enrichment prompt or model. |

```bash
# Binary
lucidvault --re-enrich

# Docker
docker run --rm \
  -e SOURCE_TOKEN=<your-raindrop-token> \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest --re-enrich
```

## Vault structure

LucidVault creates and manages these directories inside your vault:

```text
vault/
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

## Tech stack

| Component | Choice |
|-----------|--------|
| Language | Go |
| Web scraping | Jina Reader, Supadata (YouTube) |
| LLM | Ollama Cloud |
| Storage | Obsidian vault (markdown) |
| State | SQLite (modernc.org/sqlite) |
| Deployment | Docker / static binary |
| Versioning | Commitizen (conventional commits, auto changelog) |
| Bookmark source | Raindrop.io |

## To do

_No pending items._
