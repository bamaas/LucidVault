# LucidVault

AI-powered personal knowledge base. Polls Raindrop.io, scrapes via Jina Reader, enriches via Ollama Cloud, writes to Obsidian vault.

## Project Structure

```text
cmd/main.go              — Entry point, poll loop, graceful shutdown
internal/source/         — Bookmark source interface and factory
internal/raindrop/       — Raindrop API client (implements source.Client)
internal/scraper/        — Jina Reader scraper
internal/enrich/         — Ollama Cloud enrichment
internal/store/          — SQLite state (modernc.org/sqlite, pure Go)
internal/vault/          — Vault file writer, slug/URL helpers
```

## Build & Run

```bash
mise run build:binary          # Build Go binary
mise run run:binary            # Run locally
mise run build:image           # Build Docker image
mise run run:container         # Run in Docker
mise run lint:go               # go vet
```

## Design Principles

- **KISS** — Linear pipeline, no frameworks
- **Separation of concerns** — Each package owns one thing
- **Accept interfaces, return structs**
- **Error wrapping** — Always `fmt.Errorf("context: %w", err)`
- **Pure Go, no CGO** — `modernc.org/sqlite`, not `mattn/go-sqlite3`

## Required Environment Variables

- `SOURCE_NAME` — Bookmark source to use (default: `raindrop`)
- `SOURCE_TOKEN` — Access token for the bookmark source (falls back to `RAINDROP_ACCESS_TOKEN`)
- `OLLAMA_API_KEY` — Ollama Cloud API key
- `VAULT_PATH` — Path to Obsidian vault

## Workflow

- **Commit after every feature or fix** — When you complete a new feature or bug fix, create a git commit immediately. Do not batch multiple features/fixes into a single commit.

## ADRs

Read `docs/adr/` before implementing — these capture architectural decisions and their reasoning.

Every architectural or design decision **must** have an ADR in `docs/adr/`. Keep them short: Status, Context (2-3 sentences), Decision (1 sentence), Consequences (bullet list). Number sequentially (`NNN-slug.md`).
