# Plan: Agent chat via Open WebUI + Open Terminal (replace Hermes)

Closes #112. Decision: `docs/adr/032-agent-chat-via-open-terminal.md`.

## Goal

Chat with the vault from OpenWebUI through a coding agent (Claude Code as the worked example) running in an Open Terminal container, instead of Hermes. The agent reads the vault directly (read-only) and writes only through LucidVault's MCP tools.

## Scope

Two tracks:

- **A. Repo (public):** rewrite the agent-chat guide and compose overlay. Agent-agnostic; no LucidVault code changes.
- **B. Maintainer's deployment (private, not in the repo):** apply the same setup on the home server. Executed from this plan, but host names, tokens and versions stay out of the repo.

Out of scope: LucidVault code; enrichment backend (ADR-031); multi-user Open Terminal.

## Requirements

### A. Repo

1. `docs/guides/agent-chat/agent-chat-setup.md`: replace every Hermes step with:
   - OpenWebUI pinned to a 0.13.x tag ≥ 0.13.4 (not `:main`), with an explicit "back up the `open-webui` volume before upgrading; migrations run on first start" step.
   - Open Terminal service from `ghcr.io/open-webui/open-terminal:<version>-agents` (pinned), `OPEN_TERMINAL_API_KEY` from `.env`, `OPEN_TERMINAL_ENABLE_BROWSER=false`, `OPEN_TERMINAL_ENABLE_DESKTOP=false`, a named volume for `/home/user`, the vault mounted read-only, no published port.
   - Agent sign-in: subscription login (`claude` login prompts, or a `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token`) or `ANTHROPIC_API_KEY`; Codex and other ACP agents noted as equivalent.
   - MCP wiring: register `http://lucidvault:8080/mcp` with the agent (for Claude Code: `claude mcp add --transport http lucidvault http://lucidvault:8080/mcp` run as the Open Terminal user, persisted in the home volume). `MCP_ALLOWED_HOST` must include `lucidvault`.
   - OpenWebUI side: Admin → Settings → Integrations → Open Terminal (URL `http://open-terminal:8000`, the API key), then pick `claude/<model>` in the model picker.
   - Agent instructions: point the agent at the vault's `AGENTS.md` (via the agent's own memory file, e.g. a `CLAUDE.md` in its working directory) so it follows LucidVault's retrieval and citation rules.
   - Rationale section updated: why Open Terminal, why read-only plus MCP writes (link ADR-032, ADR-019, ADR-023).
   - Operations: upgrading (tag bump of both images), approvals (default policy asks in chat), limits (agent chats can't be regenerated or edited), rollback (restore the volume backup and the previous tags).
2. `docs/guides/agent-chat/docker-compose.agent.yml`: replace the `hermes` service with `open-terminal` as above; pin `open-webui`; drop `OPENAI_API_BASE_URL`/`OPENAI_API_KEY` that pointed at Hermes.
3. Delete `docs/guides/agent-chat/hermes.config.example.yaml`.
4. `README.md`: update the full-stack summary (Hermes → Open Terminal agent).
5. ADR-023 and ADR-024: add a one-line note under Context that Hermes was the example agent at the time and ADR-032 replaced it (decisions unchanged).
6. `.env.example`: replace `HERMES_API_KEY` with `OPEN_TERMINAL_API_KEY` in the full-stack section.

### B. Deployment (server)

Each step backs up first and has a check. Stop on a failed check.

1. **Back up:** `docker-compose.yaml`, `.env` (mode 600), and the `open-webui` volume (tar of the volume via a throwaway container), plus `~/.hermes`.
2. **Stop Hermes:** `docker compose stop hermes`; comment out its service block (don't delete). Keep `~/.hermes` for a week.
3. **Upgrade OpenWebUI** to the pinned 0.13.x tag; `docker compose up -d open-webui`; check that migrations completed in the logs, login works, and old chats are visible.
4. **Add Open Terminal:** service as in A.1, image pinned, `CLAUDE_CODE_OAUTH_TOKEN` from `.env` (the existing token), `OPEN_TERMINAL_API_KEY` (new random value, `.env`), vault `./vault:/home/user/vault:ro` (or a path the guide fixes), on the `default` network only, no ports, memory limit around 1.5 GB.
5. **Sign-in check:** inside the container, `claude -p "say ok"` as the Open Terminal user returns `ok` using the token (not an API key). If the token isn't accepted there, stop and report.
6. **MCP:** register LucidVault MCP for that user; `claude mcp list` shows it connected.
7. **Connect OpenWebUI:** add the Open Terminal integration (URL plus key); `claude/<model>` appears in the picker.
8. **Acceptance chat** (done by the user in the browser, or scripted where possible):
   - Read: "What does my vault say about Prometheus native instrumentation?" → answer cites a wiki page.
   - Write: "Add a note titled 'open-terminal test' with body 'hello'" → tool approval appears, note created via MCP, visible in `vault/notes/`, and indexed on the next cycle.
   - Read-only proof: ask the agent to edit a vault file directly → it fails (read-only file system).
9. **Resources:** after the acceptance chat, `free -m` and `docker stats` — if available memory is under 500 MB, report that the VM needs more RAM.
10. **Cleanup after one week of use (separate, manual go):** remove the Hermes service, image and `~/.hermes`; revoke the Ollama API key.

## Edge cases

- The OpenWebUI upgrade fails or loses chats → restore the volume backup and the previous tag (`0.9.6` image digest recorded in step 1).
- The token isn't accepted by the CLI in Open Terminal → stop; options are an interactive `claude` login inside the container (persisted in the home volume) or an API key — user decides.
- Claude usage limit hit (shared with enrichment) → the chat shows the CLI's limit message; enrichment items wait in the inbox (ADR-031), no stub pages.
- The agent tries `rm` on the vault → read-only mount blocks it; MCP write tools are the only write path.
- Open Terminal restarts → the home volume keeps the agent's login, MCP config and conversation state.

## Acceptance criteria

- [ ] `grep -rni hermes docs/guides/ README.md .env.example` returns nothing (ADR mentions are historical notes only).
- [ ] The guide's compose overlay passes `docker compose config` and has no published port on `open-terminal`, `:ro` on its vault mount, pinned tags on `open-webui` and `open-terminal`.
- [ ] `mise run lint` passes.
- [ ] On the server: `claude/<model>` in OpenWebUI answers a vault question with a citation, creates a note via MCP after an approval, and cannot write the vault directly.
- [ ] Hermes stopped, backups recorded, rollback steps tested at least to "backup restorable" (tar lists the files).
