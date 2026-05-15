# Claude instructions for LucidVault

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

- **Commit after every feature or fix** — When you complete a new feature or bug fix, present a summary of the changes for review. On approval, create a git commit immediately. Do not batch multiple features/fixes into a single commit.
- **Conventional Commits** — All commit messages must follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#specification) specification (e.g. `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`).

## ADRs

Read `docs/adr/` before implementing — these capture architectural decisions and their reasoning.

Every architectural or design decision **must** have an ADR in `docs/adr/`. Keep them short: Status, Context (2-3 sentences), Decision (1 sentence), Consequences (bullet list). Number sequentially (`NNN-slug.md`).

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

### 6. Autonomous Bug Fixing
- When given a bug report: just fix it. Don't ask for hand-holding
- Point at logs, errors, failing tests — then resolve them
- Zero context switching required from the user
- Go fix failing CI tests without being told how

## Task Management

1. **Plan First**: Write plan to `tasks/todo.md` with checkable items
2. **Verify Plan**: Check in before starting implementation
3. **Track Progress**: Mark items complete as you go
4. **Explain Changes**: High-level summary at each step
5. **Document Results**: Add review section to `tasks/todo.md`
6. **Capture Lessons**: Update `tasks/lessons.md` after corrections

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.