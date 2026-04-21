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

- **Commit after every feature or fix** — When you complete a new feature or bug fix, present a summary of the changes for review. On approval, create a git commit immediately. Do not batch multiple features/fixes into a single commit.
- **Conventional Commits** — All commit messages must follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#specification) specification (e.g. `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`).

## ADRs

Read `docs/adr/` before implementing — these capture architectural decisions and their reasoning.

Every architectural or design decision **must** have an ADR in `docs/adr/`. Keep them short: Status, Context (2-3 sentences), Decision (1 sentence), Consequences (bullet list). Number sequentially (`NNN-slug.md`).

<!-- dgc-policy-v11 -->
# Dual-Graph Context Policy

This project uses a local dual-graph MCP server for efficient context retrieval.

## MANDATORY: Always follow this order

1. **Call `graph_continue` first** — before any file exploration, grep, or code reading.

2. **If `graph_continue` returns `needs_project=true`**: call `graph_scan` with the
   current project directory (`pwd`). Do NOT ask the user.

3. **If `graph_continue` returns `skip=true`**: project has fewer than 5 files.
   Do NOT do broad or recursive exploration. Read only specific files if their names
   are mentioned, or ask the user what to work on.

4. **Read `recommended_files`** using `graph_read` — **one call per file**.
   - `graph_read` accepts a single `file` parameter (string). Call it separately for each
     recommended file. Do NOT pass an array or batch multiple files into one call.
   - `recommended_files` may contain `file::symbol` entries (e.g. `src/auth.ts::handleLogin`).
     Pass them verbatim to `graph_read(file: "src/auth.ts::handleLogin")` — it reads only
     that symbol's lines, not the full file.
   - Example: if `recommended_files` is `["src/auth.ts::handleLogin", "src/db.ts"]`,
     call `graph_read(file: "src/auth.ts::handleLogin")` and `graph_read(file: "src/db.ts")`
     as two separate calls (they can be parallel).

5. **Check `confidence` and obey the caps strictly:**
   - `confidence=high` -> Stop. Do NOT grep or explore further.
   - `confidence=medium` -> If recommended files are insufficient, call `fallback_rg`
     at most `max_supplementary_greps` time(s) with specific terms, then `graph_read`
     at most `max_supplementary_files` additional file(s). Then stop.
   - `confidence=low` -> Call `fallback_rg` at most `max_supplementary_greps` time(s),
     then `graph_read` at most `max_supplementary_files` file(s). Then stop.

## Token Usage

A `token-counter` MCP is available for tracking live token usage.

- To check how many tokens a large file or text will cost **before** reading it:
  `count_tokens({text: "<content>"})`
- To log actual usage after a task completes (if the user asks):
  `log_usage({input_tokens: <est>, output_tokens: <est>, description: "<task>"})`
- To show the user their running session cost:
  `get_session_stats()`

Live dashboard URL is printed at startup next to "Token usage".

## Rules

- Do NOT use `rg`, `grep`, or bash file exploration before calling `graph_continue`.
- Do NOT do broad/recursive exploration at any confidence level.
- `max_supplementary_greps` and `max_supplementary_files` are hard caps - never exceed them.
- Do NOT dump full chat history.
- Do NOT call `graph_retrieve` more than once per turn.
- After edits, call `graph_register_edit` with the changed files. Use `file::symbol` notation (e.g. `src/auth.ts::handleLogin`) when the edit targets a specific function, class, or hook.

## Context Store

Whenever you make a decision, identify a task, note a next step, fact, or blocker during a conversation, call `graph_add_memory`.

**To add an entry:**
```
graph_add_memory(type="decision|task|next|fact|blocker", content="one sentence max 15 words", tags=["topic"], files=["relevant/file.ts"])
```

**Do NOT write context-store.json directly** — always use `graph_add_memory`. It applies pruning and keeps the store healthy.

**Rules:**
- Only log things worth remembering across sessions (not every minor detail)
- `content` must be under 15 words
- `files` lists the files this decision/task relates to (can be empty)
- Log immediately when the item arises — not at session end

## Session End

When the user signals they are done (e.g. "bye", "done", "wrap up", "end session"), proactively update `CONTEXT.md` in the project root with:
- **Current Task**: one sentence on what was being worked on
- **Key Decisions**: bullet list, max 3 items
- **Next Steps**: bullet list, max 3 items

Keep `CONTEXT.md` under 20 lines total. Do NOT summarize the full conversation — only what's needed to resume next session.
