# Claude instructions for LucidVault

AI-powered personal knowledge base. Scrapes URLs, enriches via Ollama Cloud, writes to Obsidian vault. URLs enter through an inbox folder — either manually or via optional Raindrop.io integration.

## Pipeline

```text
Poll loop (configurable interval)
  │
  ├─ syncRaindropToInbox (optional, when RAINDROP_ACCESS_TOKEN is set):
  │    raindrop.FetchBookmarks()          → []Bookmark
  │    raindrop.SyncToInbox()             → create inbox/*.md for new URLs (dedup via DB)
  │
  ├─ processInbox:
  │    inbox.Scan(vaultPath)              → []Item (reads inbox/*.md)
  │    scraper.Scrape(url)                → markdown content (Jina Reader; YouTube via Supadata)
  │    enrich.Enrich(content)             → wiki-style summary (Ollama Cloud)
  │    vault.WriteRaw() + vault.WriteWiki() + vault.UpdateIndex()
  │    store.UpsertBookmark()             → mark processed in SQLite
  │    inbox.Delete()                     → remove processed inbox file
  │
  └─ processNotes:
       notes.Scan(vaultPath)              → detect new/changed notes
       enrich.SuggestTags(content)        → auto-tag notes without tags (Ollama Cloud)
       vault.WriteWiki() + vault.UpdateIndex()  → wiki copy with tags
       store.UpsertNote()                 → track content hashes + wiki path
```

## Project Structure

```text
cmd/main.go              — Entry point, poll loop, graceful shutdown
internal/claudemd/       — CLAUDE.md upsert logic for Claude Code integration
internal/inbox/          — Inbox scanner and file management
internal/raindrop/       — Raindrop API client and inbox sync (optional feeder)
internal/scraper/        — Scraper (Jina Reader + Supadata YouTube transcripts)
internal/enrich/         — Ollama Cloud enrichment
internal/notes/          — Notes scanner, frontmatter parser
internal/mcpserver/      — MCP server (retrieval, inbox write, vault mutation tools for AI agents)
internal/store/          — SQLite state (modernc.org/sqlite, pure Go)
internal/vault/          — Vault file writer, slug/URL helpers
CONTEXT.md                — Domain glossary (ubiquitous language, no implementation details)
docs/plans/               — Feature plans and sub-plans (permanent reference)
docs/adr/                 — Architecture Decision Records
deploy/livesync-bridge/   — LiveSync bridge Dockerfile and config
docker-compose.yml        — Full stack: LucidVault + LiveSync bridge + CouchDB
.env.example              — Environment variable template for Docker Compose
```

## Key Interfaces

- **`inbox.Scan`** — `Scan(vaultPath) ([]Item, error)`. Reads `inbox/*.md` files, parses optional YAML frontmatter (title, tags) and URL.
- **`inbox.Delete`** — `Delete(path) error`. Removes processed inbox file.
- **`raindrop.Client`** — `FetchBookmarks(ctx) ([]Bookmark, error)`. Fetches all bookmarks from Raindrop API.
- **`raindrop.SyncToInbox`** — `SyncToInbox(bookmarks, db, vaultPath) (int, error)`. Creates inbox files for new URLs (dedup via DB).
- **`scraper.Scraper`** — `Scrape(ctx, url) (*Result, error)`. Uses Jina Reader; delegates YouTube URLs to `YouTubeClient` (Supadata).
- **`enrich.Client`** — `Enrich(ctx, *EnrichInput) (string, error)`. Calls Ollama Cloud with retry logic. Returns wiki-formatted markdown. `SuggestTags(ctx, *TagInput) ([]string, error)` generates tags for notes.
- **`vault.Vault`** — `WriteRaw()`, `WriteWiki()`, `UpdateIndex()`, `RemoveFromIndex()`. Manages file layout and `index.md`.
- **`store.Store`** — SQLite-backed deduplication. Tracks processed bookmarks (by normalized URL) and note content hashes.

## Build, Test & Lint

```bash
mise run build:binary          # Build Go binary
mise run build:image           # Build Docker image
mise run run:binary            # Run locally
mise run run:container         # Run in Docker
mise run test                  # go test ./...
mise run lint                  # All linters (go, dockerfile, actions, vuln, yaml, markdown)
mise run lint:go               # golangci-lint only
mise run lint:commits          # Check commit message (used by commit-msg hook)
```

## Design Principles

- **KISS** — Linear pipeline, no frameworks
- **Separation of concerns** — Each package owns one thing
- **Accept interfaces, return structs**
- **Error wrapping** — Always `fmt.Errorf("context: %w", err)`
- **Pure Go, no CGO** — `modernc.org/sqlite`, not `mattn/go-sqlite3`
- **Simplicity first** — Make every change as simple as possible, impact minimal code
- **Minimal impact** — Changes should only touch what's necessary

## Common Gotchas

- **Pure-Go SQLite** — Uses `modernc.org/sqlite` (no CGO). Don't import `mattn/go-sqlite3`. Build tags for CGO will break the build.
- **Jina Reader rate limits** — Scraper makes HTTP calls to `r.jina.ai`. Respect rate limits; the poll interval naturally throttles.
- **Ollama Cloud retries** — `enrich.Client` has built-in retry with configurable `maxRetries` and `delayMs`. Don't add external retry wrappers.
- **Inbox is the single entry point** — All URLs flow through `inbox/`. Raindrop is an optional feeder that creates inbox files. No other entry path exists.
- **Vault index.md** — `vault.UpdateIndex()` appends to `index.md`. Idempotent by slug.

## Environment Variables

- `OLLAMA_API_KEY` — (required) Ollama Cloud API key
- `VAULT_PATH` — (required) Path to Obsidian vault
- `RAINDROP_ACCESS_TOKEN` — (optional) Enables Raindrop.io as an inbox feeder
- `SUPADATA_API_KEY` — (optional) Supadata API key for YouTube transcript extraction
- `HYGIENE_INTERVAL` — (optional, default: 10) Run vault hygiene every Nth poll cycle

## Deployment

Two deployment options:

1. **Local Docker** — Single container, vault as volume mount. Default for users who don't need multi-device sync.
2. **Docker Compose with LiveSync** — LucidVault + livesync-bridge sidecar + CouchDB. Enables multi-device vault access via Obsidian LiveSync. Zero LucidVault code changes — sync is entirely infrastructure.

See `docs/design/017-couchdb-livesync.md` for the full design document and `docs/adr/017-couchdb-livesync-obsidian-sync.md` for the architectural decision record.

## Plans

Feature plans live in `docs/plans/` and serve as permanent reference documentation.

```text
docs/plans/
  plan-vault-hygiene.md              — parent plan (requirements, goals, scope)
  plan-vault-hygiene/                — sub-plans (created by /decompose)
    01-scan-orphaned-files.md
    02-cleanup-stale-index.md
    03-add-hygiene-command.md
  plan-agent-retrieval.md            — plan without sub-plans (single mode)
```

- **Parent plans** describe *what* to build: requirements, goals, scope, edge cases.
- **Sub-plans** describe *how* to execute: ordered, self-contained chunks sized for one context window. Created by `/decompose`, committed alongside the parent plan.
- **Reference use** — Plans and sub-plans are committed to the repo. When debugging or extending a feature later, read the relevant plan to understand requirements, execution order, edge cases, and acceptance criteria.

### Feature workflow

1. `/grill-with-docs` — stress-test the idea, sharpen terms in `CONTEXT.md`, create ADRs, produce plan in `docs/plans/`
2. `/decompose docs/plans/plan-feature.md` — (large features only) break plan into sub-plans
3. `/deliver docs/plans/plan-feature.md` — implement + test + review + PR (loops per sub-plan if decomposed)

## Workflow

- **Commit after every feature or fix** — When you complete a new feature or bug fix, present a summary of the changes for review. On approval, create a git commit immediately. Do not batch multiple features/fixes into a single commit.
- **Conventional Commits** — All commit messages must follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#specification) specification (e.g. `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`).
- **Update docs on feature changes** — When adding features, changing env vars, or modifying the pipeline, update `README.md` (features, config table, tech stack, to-do) and `CLAUDE.md` (project structure, env vars) before committing.
- **Test-Driven Development for features** — All feature requests must follow TDD:
  1. Pull latest changes from main (`git fetch origin main && git merge origin/main`) before starting
  2. Write failing tests first that define the expected behavior (via subagent, spec-only — no implementation knowledge)
  3. Implement the minimum code to make the tests pass
  4. Refactor if needed while keeping tests green

## ADRs

Read `docs/adr/` before implementing — these capture architectural decisions and their reasoning.

ADRs are created during the planning phase (`/grill-with-docs`), not during implementation. If an implementing agent discovers a missing architectural decision, it should stop and flag it to the user rather than deciding on the fly.

Format: Status, Context (2-3 sentences), Decision (1 sentence), Consequences (bullet list). Number sequentially (`NNN-slug.md`).

## Workflow Orchestration

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately — don't keep pushing
- Use plan mode for verification steps, not just building
- Write detailed specs upfront to reduce ambiguity

### 2. Subagent Strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- For complex problems, throw more compute at it via subagents
- One task per subagent for focused execution

### 3. Self-Improvement Loop

- After ANY correction from the user: update `tasks/lessons.md` with the pattern
- Write rules for yourself that prevent the same mistake
- Ruthlessly iterate on these lessons until mistake rate drops
- Review lessons at session start for relevant project

### 4. Verification Before Done

- Never mark a task complete without proving it works
- Diff behavior between main and your changes when relevant
- Ask yourself: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- If a fix feels hacky: "Knowing everything I know now, implement the elegant solution"
- Skip this for simple, obvious fixes — don't over-engineer
- Challenge your own work before presenting it

### 6. Design Validation

- For features, use `/grill-with-docs` to stress-test the idea against the codebase, sharpen terminology in `CONTEXT.md`, create ADRs for trade-offs, and produce a plan in `docs/plans/`
- For quick questions or non-codebase brainstorming, `/grill-me` is still appropriate
- Don't auto-trigger — only when the plan involves trade-offs worth exploring

### 7. Review Before Merge

- For non-trivial changes, spawn a subagent to review the diff before creating a PR
- Before creating a PR, always merge the latest `origin/main` into the feature branch (`git fetch origin main && git merge origin/main`) to ensure the PR includes all recent changes
- After merging main, re-run linters and tests (`mise run lint` and `mise run test`) — the merge can introduce new violations
- Create the PR with auto-merge enabled (`--auto --squash --delete-branch`) so it merges when CI passes and the branch is deleted
- Skip the review agent for docs-only or config-only changes

### 8. Autonomous Bug Fixing

- When given a bug report: just fix it. Don't ask for hand-holding
- Point at logs, errors, failing tests — then resolve them
- Zero context switching required from the user
- Go fix failing CI tests without being told how

## Task Management

1. **Plan First**: Write plan to `tasks/todo.md` with checkable items (created on-demand, gitignored)
2. **Verify Plan**: Check in before starting implementation
3. **Track Progress**: Mark items complete as you go
4. **Explain Changes**: High-level summary at each step
5. **Document Results**: Add review section to `tasks/todo.md`
6. **Capture Lessons**: Update `tasks/lessons.md` after corrections (created on-demand, gitignored)
