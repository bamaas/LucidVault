# LucidVault

You save dozens of articles, blog posts, and links every week. Most of them disappear into a bookmark graveyard — never read, never searchable, never connected to anything. LucidVault fixes that.

LucidVault turns saved bookmarks into a structured, searchable knowledge base inside your Obsidian vault. It scrapes the full content, summarizes it with an LLM, extracts key takeaways, and links it to your existing notes — automatically. Your personal notes live alongside enriched pages, and a `soul.md` file personalizes everything to your background and interests.

LucidVault can inject a retrieval strategy section into your `~/.claude/CLAUDE.md`, so Claude Code knows how to query your knowledge base efficiently, making it a daily companion for development work.

## Features

- **Capture** — Save a bookmark on your phone, it appears in your vault within minutes: scraped, summarized, tagged, and linked
- **Enrich** — LLM generates a wiki-style summary with key takeaways, tags, and wiki-links to related pages
- **Retrieve** — Built-in Claude Code integration with a tiered lookup strategy (index → wiki → raw) that keeps token usage low
- **Resilient** — Falls back to basic metadata when scraping fails (paywalled sites, blocked content)
- **Backfill** — Processes all your existing bookmarks on first run

## Getting started

### Prerequisites

- Docker
- A [Raindrop.io](https://raindrop.io) account (free)
- An [Ollama Cloud](https://ollama.com) account

### 1. Get your API tokens

- **Raindrop**: Go to https://app.raindrop.io/settings/integrations → create a test token. These don't expire for personal use.
- **Ollama Cloud**: Go to https://ollama.com/settings/keys → create an API key.

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
  lucidvault:latest
```

That's it. LucidVault will poll Raindrop every 5 minutes. On first run, it backfills all your existing bookmarks.

**Optional: Claude Code integration**

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
  lucidvault:latest
```

### 5. Check it's working

```bash
docker logs -f lucidvault
```

You should see bookmarks being fetched, scraped, and enriched. Files appear in your vault under `raw/` (scraped content) and `wiki/` (enriched pages).

## Configuration

All configuration is via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SOURCE_TOKEN` | Yes | — | Access token for the bookmark source (falls back to `RAINDROP_ACCESS_TOKEN`) |
| `OLLAMA_API_KEY` | Yes | — | Ollama Cloud API key |
| `VAULT_PATH` | Yes | `/vault` (Docker) | Path to vault |
| `SOURCE_NAME` | No | `raindrop` | Bookmark source to use |
| `OLLAMA_MODEL` | No | `qwen3.5` | LLM model for enrichment |
| `POLL_INTERVAL` | No | `5m` | How often to check for new bookmarks |
| `BATCH_SIZE` | No | `10` | Max bookmarks per poll cycle |
| `ENRICH_DELAY_MS` | No | `500` | Delay between API calls (rate limiting) |
| `ENRICH_MAX_RETRIES` | No | `3` | Max retries on API failure |
| `CLAUDE_MD_PATH` | No | `/CLAUDE.md` | Path to CLAUDE.md for Claude Code integration (override only if needed) |

## Vault structure

LucidVault creates and manages these directories inside your vault:

```text
vault/
├── raw/          # Immutable scraped content (don't edit)
├── wiki/         # LLM-generated wiki pages (don't edit — overwritten on re-enrichment)
├── notes/        # Your personal notes (yours to write freely)
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
5. Fall back to `raw/` only as a last resort (large files)

It will never scan entire directories, and will not search the web unprompted.

## Tech stack

| Component | Choice |
|-----------|--------|
| Language | Go |
| Web scraping | Jina Reader |
| LLM | Ollama Cloud |
| Storage | Obsidian vault (markdown) |
| State | SQLite (modernc.org/sqlite) |
| Deployment | Docker / static binary |
| Bookmark source | Raindrop.io |

## To do

### YouTube support
- [ ] Extract a `Scraper` interface (`internal/scraper`) so multiple scraping strategies can coexist
- [ ] Add a YouTube transcript scraper that picks up `youtube.com`/`youtu.be` URLs
- [ ] Route to the correct scraper based on URL pattern (YouTube → transcript, everything else → Jina)
- [ ] Feed transcript through the existing enrichment pipeline for summarization

### Personal notes indexing
- [ ] Scan `notes/` for new/changed markdown files and add them to `index.md`
- [ ] Extract tags and wiki-links from personal notes so they connect to the knowledge graph