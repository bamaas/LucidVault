# Simple setup - single container

The quickest way to run LucidVault: one container with your vault mounted as a
volume. The pipeline scrapes and enriches URLs you drop in the inbox and writes the
results to your vault. Best for a single machine with no multi-device sync or chat
UI - for those, see the [full-stack runbook](./agent-chat/agent-chat-setup.md).

## Prerequisites

- Docker
- An [Ollama Cloud](https://ollama.com) account (free)
- (Optional) A [Raindrop.io](https://raindrop.io) account (free) - automatic
  bookmark syncing

## 1. Get your API tokens

- **Ollama Cloud:** <https://ollama.com/settings/keys> → create an API key.
- **Raindrop** (optional): <https://app.raindrop.io/settings/integrations> → create
  a test token (doesn't expire for personal use).

## 2. Prepare your vault directory

Create a directory for your knowledge base - or point at an existing Obsidian vault.

```bash
mkdir -p ~/lucid-vault
```

## 3. (Optional) Create a soul.md

`soul.md` personalizes LucidVault - it tailors enrichment summaries to your
interests and helps an agent tailor answers to your background. Place it at the
vault root:

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

Skip it and everything still works - just without personalization.

## 4. Run the container

**Inbox-only** (no Raindrop):

```bash
docker run -d --name lucidvault --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  ghcr.io/bamaas/lucidvault:latest
```

**With Raindrop.io** (auto-feeds bookmarks into the inbox) - add the token:

```bash
  -e RAINDROP_ACCESS_TOKEN=<your-raindrop-token> \
```

LucidVault polls every 5 minutes. Drop `.md` files containing URLs into `inbox/` to
process them; with Raindrop enabled, bookmarks sync there automatically.

### Or with Docker Compose

The repo root ships a single-container [`docker-compose.yml`](../../docker-compose.yml)
that runs exactly this. From the repo root:

```bash
cp .env.example .env      # then set OLLAMA_API_KEY
docker compose up -d
```

It bind-mounts `./vault`. This same file is the base the
[full-stack runbook](./agent-chat/agent-chat-setup.md) layers on with a compose
overlay, so moving up later is just an extra `-f` flag.

## 5. (Optional) Point Claude Code at your vault

LucidVault can upsert a pointer into your `~/.claude/CLAUDE.md` so Claude Code reads
the vault's `AGENTS.md` (which carries the retrieval strategy). Bind-mount it:

```bash
touch ~/.claude/CLAUDE.md    # ensure it exists before mounting

docker run -d --name lucidvault --restart unless-stopped \
  -e OLLAMA_API_KEY=<your-key> \
  -v ~/lucid-vault:/vault \
  -v ~/.claude/CLAUDE.md:/CLAUDE.md \
  ghcr.io/bamaas/lucidvault:latest
```

Claude Code then reads the vault files directly. To let it also *write* (add
bookmarks/notes, edit pages) deterministically over MCP - and to sync the vault
across devices - move up to the [full-stack runbook](./agent-chat/agent-chat-setup.md).

## 6. Check it's working

```bash
docker logs -f lucidvault
```

You should see items being fetched, scraped, and enriched. Files land in your vault
under `raw/` (scraped content) and `wiki/` (enriched pages).

---

See the main [README](../../README.md#configuration) for the full environment
variable and CLI-flag reference.
