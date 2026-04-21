# LucidVault

You save dozens of articles, blog posts, and links every week. Most of them disappear into a bookmark graveyard — never read, never searchable, never connected to anything. LucidVault fixes that.

LucidVault turns saved bookmarks into a structured, searchable knowledge base inside your Obsidian vault. It scrapes the full content, summarizes it with an LLM, extracts key takeaways, and links it to your existing notes — automatically. Your personal notes live alongside enriched pages, and a `soul.md` file personalizes everything to your background and interests.

LucidVault generates a `CLAUDE.md` in your vault, so Claude Code can query your knowledge base efficiently, making it a daily companion for development work.

## Features

- **Capture** — Save a bookmark on your phone, it appears in your vault within minutes: scraped, summarized, tagged, and linked
- **Enrich** — LLM generates a wiki-style summary with key takeaways, tags, and wiki-links to related pages
- **Retrieve** — Built-in Claude Code integration with a tiered lookup strategy (index → wiki → raw) that keeps token usage low
- **Resilient** — Falls back to basic metadata when scraping fails (paywalled sites, blocked content)
- **Backfill** — Processes all your existing bookmarks on first run
- **Pluggable** — Swap bookmark sources without changing the pipeline (Raindrop today, anything tomorrow)

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
mkdir -p ~/obsidian-vault
```

### 3. (Optional) Create a soul.md

`soul.md` personalizes your entire LucidVault experience. It's used during enrichment (tailoring summaries to your interests) and during retrieval (Claude Code reads it to tailor answers to your background). Place it at the root of your vault:

```bash
cat > ~/obsidian-vault/soul.md << 'EOF'
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
  -e RAINDROP_ACCESS_TOKEN=<your-token> \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/obsidian-vault:/vault
  lucidvault:latest
```

That's it. LucidVault will poll Raindrop every 5 minutes. On first run, it backfills all your existing bookmarks.

### 5. Check it's working

```bash
docker logs -f lucidvault
```

You should see bookmarks being fetched, scraped, and enriched. Files appear in your vault under `raw/` (scraped content) and `wiki/` (enriched pages).

## Configuration

All configuration is via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `RAINDROP_ACCESS_TOKEN` | Yes | — | Raindrop test token |
| `OLLAMA_API_KEY` | Yes | — | Ollama Cloud API key |
| `VAULT_PATH` | Yes | — | Path to vault (use `/vault` with Docker) |
| `OLLAMA_MODEL` | No | `qwen3.5` | LLM model for enrichment |
| `POLL_INTERVAL` | No | `5m` | How often to check for new bookmarks |
| `BATCH_SIZE` | No | `10` | Max bookmarks per poll cycle |
| `ENRICH_DELAY_MS` | No | `500` | Delay between API calls (rate limiting) |
| `ENRICH_MAX_RETRIES` | No | `3` | Max retries on API failure |

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

LucidVault auto-generates a `CLAUDE.md` in your vault on first run. When you open the vault directory with Claude Code, it automatically follows an efficient retrieval strategy:

1. Read `index.md` to find relevant pages by title/tags
2. Read the matching `wiki/` page (enriched summary)
3. Only fall back to `raw/` if the wiki page lacks detail

This avoids reading large raw files unnecessarily and keeps token usage low.

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