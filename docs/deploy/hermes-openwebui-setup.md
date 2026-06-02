# LucidVault + Hermes + OpenWebUI — Infrastructure Setup

A reproducible runbook for the "ask my vault from a chat UI" stack described in
`docs/plans/plan-agent-retrieval.md`.

This setup lets you ask natural-language questions about your Obsidian vault
through a web chat UI. The UI talks to an **agent** (Hermes) that reactively
explores the vault via the LucidVault **MCP** retrieval tools, backed by an
**LLM** on Ollama Cloud.

---

## 1. Architecture

```text
┌────────────────┐  http :3000           ┌──────────────────────────────────┐
│  Browser       │ ────────────────────► │  OpenWebUI            (container) │
└────────────────┘                        │  OPENAI_API_BASE_URL →           │
                                           │    host.docker.internal:8090/v1  │
                                           └───────────────┬──────────────────┘
                                                           │ OpenAI-compatible
                                                           │ (Bearer = HERMES_API_KEY)
                                                           ▼
                                  ┌─────────────────────────────────────────────┐
                                  │  Hermes API server  :8090   (host, systemd) │
                                  │  gateway platform → runs the AIAgent loop   │
                                  │  model: kimi-k2:1t-cloud  (Ollama Cloud)    │
                                  └───────────────┬─────────────────────────────┘
                                                  │ MCP (Streamable HTTP)
                                                  ▼
                                  ┌─────────────────────────────────────────────┐
                                  │  lucidvault-mcp  127.0.0.1:8080 /mcp        │
                                  │  (container) 13 retrieval + mutation tools  │
                                  └───────────────┬─────────────────────────────┘
                                                  │ filesystem (bind mount)
                                                  ▼
                                  ┌─────────────────────────────────────────────┐
                                  │  Obsidian vault  ./vault                    │
                                  │  written by the lucidvault pipeline cont.   │
                                  └─────────────────────────────────────────────┘
```

**Four moving parts:**

| Part | Where it runs | Role |
|---|---|---|
| `lucidvault` (pipeline) | Docker container | Scrapes/enriches URLs → writes the vault |
| `lucidvault-mcp` | Docker container | Serves vault retrieval/mutation tools over MCP (`:8080/mcp`) |
| **Hermes Agent** | **Host (native)** | OpenAI-compatible API on `:8090`; runs the agent loop + calls MCP |
| `open-webui` | Docker container | Chat UI on `:3000`; points at Hermes as its OpenAI backend |

Everything for the containers lives in this directory
(`/home/bas/apps/lucidvault`); Hermes lives in `~/.hermes`.

---

## 2. Prerequisites

- Docker + Docker Compose v2 (`docker compose version`)
- An **Ollama Cloud** API key — <https://ollama.com/settings>
- A checkout of the LucidVault repo (to build the image), e.g.
  `/home/bas/git/LucidVault/worktrees/<branch>`
- Linux host with systemd (for the Hermes user service). Tested on Ubuntu.

---

## 3. Directory layout

```text
/home/bas/apps/lucidvault/
├── docker-compose.yaml     # lucidvault + lucidvault-mcp + open-webui
├── .env                    # secrets/config for the containers (NOT committed)
├── SETUP.md                # this file
└── vault/                  # the Obsidian vault (bind-mounted into containers)

~/.hermes/                  # Hermes agent home
├── .env                    # Hermes secrets/config (OLLAMA + API_SERVER_*)
├── config.yaml             # model, mcp_servers, agent behaviour
└── HERMES.md / SOUL.md     # agent system prompt + user profile
```

---

## 4. Step 1 — Build the LucidVault image

The image is built once from the repo checkout and reused by both LucidVault
containers.

```bash
docker build -t lucidvault:branch \
  /home/bas/git/LucidVault/worktrees/feature-hermes-agent-openwebui-retrieval-setup
# pure-Go binary, no CGO → tiny image (~19 MB)
```

> If you tag the image differently, update `image:` in `docker-compose.yaml`.

---

## 5. Step 2 — Configure and start the containers

### 5.1 `/home/bas/apps/lucidvault/.env`

```dotenv
# --- LLM (Ollama Cloud) — used by the enrichment pipeline ---
OLLAMA_API_KEY=<your-ollama-cloud-key>
OLLAMA_MODEL=                 # blank → pipeline default (qwen3.5)
POLL_INTERVAL=5m

# --- Hermes API server bearer token ---
# MUST be identical to API_SERVER_KEY in ~/.hermes/.env (see Step 3).
# OpenWebUI sends this as its OpenAI API key.
HERMES_API_KEY=<shared-secret>
```

Generate a strong shared secret once:

```bash
openssl rand -hex 24      # use the output for BOTH HERMES_API_KEY and API_SERVER_KEY
```

### 5.2 `docker-compose.yaml` (already in this directory)

Three services off one image + the OpenWebUI image:

- `lucidvault` — default entrypoint → pipeline poll loop (writes the vault)
- `lucidvault-mcp` — `command: ["mcp","--http",":8080"]` → MCP server, published
  on `127.0.0.1:8080` (loopback only)
- `open-webui` — UI on `0.0.0.0:3000`, pointed at the host's `:8090`

> **Why two LucidVault containers, not one?** The binary's `main()`
> short-circuits on the `mcp` subcommand and otherwise runs the blocking poll
> loop — so one process is *either* the pipeline *or* the MCP server, never
> both. A container runs one foreground command, so the two roles are two
> `command:` overrides on the same image. Keeping them split also means the MCP
> endpoint OpenWebUI depends on stays up while the pipeline restarts/re-enriches.

OpenWebUI reaches the host-native Hermes via the `host.docker.internal`
extra-host mapping:

```yaml
  open-webui:
    image: ghcr.io/open-webui/open-webui:main
    ports: ["0.0.0.0:3000:8080"]
    extra_hosts: ["host.docker.internal:host-gateway"]
    environment:
      OPENAI_API_BASE_URL: "http://host.docker.internal:8090/v1"
      OPENAI_API_KEY: "${HERMES_API_KEY:?set HERMES_API_KEY in .env}"
      ENABLE_OLLAMA_API: "false"     # single backend; no direct Ollama in the UI
```

### 5.3 Start

```bash
cd /home/bas/apps/lucidvault
docker compose up -d
docker compose ps          # lucidvault, lucidvault-mcp, open-webui all Up
```

---

## 6. Step 3 — Install and configure Hermes (native on the host)

Hermes runs **on the host**, not in Docker, so it can read the vault directly
and reach the loopback MCP port.

### 6.1 Install

Installed via the official git method (this host runs **Hermes Agent v0.15.1**):

```bash
git clone git@github.com:NousResearch/hermes-agent.git
cd hermes-agent
./install.sh            # creates ~/.hermes, venv, and the `hermes` launcher on PATH
hermes postinstall      # bootstraps node/browser/ripgrep/ffmpeg deps
hermes --version
```

> Install method is recorded in `~/.hermes/.install_method` (`git`).

### 6.2 Select the model (Ollama Cloud)

```bash
hermes model            # interactive: choose Ollama Cloud → kimi-k2:1t-cloud
```

This writes `model.default: "kimi-k2:1t-cloud"` into `~/.hermes/config.yaml`.
Kimi K2 has a 256K context window and strong tool-use, which matters for
multi-step vault retrieval.

### 6.3 `~/.hermes/.env`

```dotenv
# LLM provider — Ollama Cloud (OpenAI-compatible endpoint)
OLLAMA_API_KEY=<your-ollama-cloud-key>
OLLAMA_CLOUD_API_KEY=<your-ollama-cloud-key>

# OpenAI-compatible API server (what OpenWebUI connects to)
API_SERVER_ENABLED=true
API_SERVER_HOST=0.0.0.0          # reachable from the OpenWebUI container
API_SERVER_PORT=8090
API_SERVER_KEY=<shared-secret>   # MUST equal HERMES_API_KEY in apps/.env
```

### 6.4 Wire the LucidVault MCP server into Hermes

In `~/.hermes/config.yaml`:

```yaml
mcp_servers:
  lucidvault:
    url: "http://127.0.0.1:8080/mcp"   # the lucidvault-mcp container
```

Verify the agent can see the tools:

```bash
hermes mcp test lucidvault
# ✓ Connected … ✓ Tools discovered: 13
# (search_index, read_wiki, grep_vault, related_notes, expand_graph,
#  vault_overview, get_soul, read_note, read_raw, add_bookmark,
#  add_note, update_wiki, delete_page)
```

### 6.5 Run the API server as a persistent service

The API server is a **gateway platform**, so starting the gateway starts
`:8090`. Install it as a systemd **user** service so it survives logout/reboot:

```bash
hermes gateway install      # answer "y" to: start now? + start on boot?
loginctl enable-linger "$USER"   # keep the user service running without a login session
hermes gateway status       # ● active (running)
```

Logs: `journalctl --user -u hermes-gateway -f`

---

## 7. Step 4 — Verify the full chain

```bash
KEY=$(grep '^API_SERVER_KEY=' ~/.hermes/.env | cut -d= -f2-)

# 1. API server is up and auth works (no key → 401)
curl -s http://127.0.0.1:8090/v1/models -H "Authorization: Bearer $KEY"
#   → {"data":[{"id":"hermes-agent",...}]}

# 2. End-to-end agent retrieval (runs the agent loop + MCP tools; ~30-60s)
curl -s http://127.0.0.1:8090/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"model":"hermes-agent","stream":false,
       "messages":[{"role":"user","content":"Give me a brief overview of my vault."}]}'
#   → answer with real page/edge counts and topics

# 3. OpenWebUI container can reach the host API server
docker exec open-webui sh -c \
  'curl -s "$OPENAI_API_BASE_URL/models" -H "Authorization: Bearer $OPENAI_API_KEY"'
#   → same hermes-agent model list
```

Then open **`http://<host-ip>:3000`**, create the OpenWebUI admin account on
first load, pick the **`hermes-agent`** model, and ask about your vault.

---

## 8. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `/v1/models` → `{"Invalid API key"}` after editing `.env` | The running gateway loaded the **old** key | `hermes gateway restart` (reloads `.env`) |
| `/v1/models` → `401` with a key | `HERMES_API_KEY` (apps/.env) ≠ `API_SERVER_KEY` (~/.hermes/.env) | Make them identical, restart gateway + `docker compose up -d` |
| OpenWebUI shows no models | Container can't reach host `:8090` | Ensure `extra_hosts: host.docker.internal:host-gateway` and `API_SERVER_HOST=0.0.0.0` |
| UI hangs on send | streaming broken | Confirm SSE: `curl -N … -d '{…,"stream":true}'` returns `data:` chunks + `[DONE]` |
| `hermes mcp test lucidvault` fails | MCP container down | `docker compose up -d lucidvault-mcp`; check `:8080` is published on loopback |
| Gateway dies after logout | linger not enabled | `loginctl enable-linger "$USER"` |
| `gateway install` appears to hang | it asks 2 interactive prompts | answer both (start now? / start on boot?) |
| `No user allowlists configured` warning | gateway messaging allowlist | harmless for API-server-only use; messaging channels (Telegram etc.) aren't configured |

---

## 9. Day-2 operations

```bash
# Containers
cd /home/bas/apps/lucidvault
docker compose ps | up -d | logs -f | restart | down

# Hermes API server
hermes gateway status | restart | stop
journalctl --user -u hermes-gateway -f

# Update Hermes
hermes update

# Rebuild LucidVault after code changes
docker build -t lucidvault:branch <repo-worktree> && docker compose up -d
```

---

## 10. Security notes

- **`:3000` (OpenWebUI) is bound to `0.0.0.0`** — anyone who can reach the host
  can load the UI. OpenWebUI has its own login, but if this VM is internet-facing
  put it behind a reverse proxy with TLS + auth, or bind to `127.0.0.1` / a
  tailnet and tunnel in.
- **`:8090` (Hermes API) is `0.0.0.0`** but protected by the bearer key. It must
  be `0.0.0.0` (not loopback) so the OpenWebUI container can reach it via the
  docker bridge. If you don't need that, you can instead publish OpenWebUI with
  `network_mode: host` and bind Hermes to loopback.
- **`:8080` (MCP) is loopback-only** (`127.0.0.1:8080`) — good; it has vault
  **mutation** tools (`add_note`, `update_wiki`, `delete_page`). Don't expose it.
- Keep `.env` files `chmod 600`; never commit them.
- The shared secret lives in two places — rotate both together and restart both
  the gateway and the compose stack.

---

## 11. Suggested improvements

These are optional hardening / quality ideas, not required for a working setup:

1. **Single source of truth for the shared key.** Today the secret is duplicated
   in `apps/.env` (`HERMES_API_KEY`) and `~/.hermes/.env` (`API_SERVER_KEY`).
   Consider a tiny `make`/script step that generates it once and writes both, to
   prevent the "stale key → 401" drift that bit us during setup.
2. **Mount the vault read-only into the MCP container if you disable mutations.**
   The plan favours "agent reads directly, writes via MCP." If you only want
   retrieval (no `add_note`/`update_wiki`/`delete_page` from chat), you remove a
   whole class of accidental writes.
3. **Reverse proxy (Caddy/Traefik) in front of `:3000`** for TLS + a single
   public entrypoint, especially before exposing this beyond localhost/tailnet.
4. **Pin the OpenWebUI image** to a digest instead of `:main` — `:main` moves and
   can break the UI on an unattended `docker compose pull`.
5. **Health/uptime checks.** Add a `healthcheck:` to the `lucidvault-mcp` service
   and a simple cron/systemd timer that curls `:8090/v1/models` and alerts on
   failure, so a dead gateway is noticed before a query is attempted.
6. **Boot ordering.** Containers (`restart: unless-stopped`) and the gateway
   (linger + enabled) both come back after reboot, but they start independently.
   The gateway tolerates a briefly-absent MCP server (it connects per request),
   so no hard dependency is needed — but verify after your next reboot.
7. **Backups.** `~/.hermes` holds learned skills, memory, and sessions; the
   `vault/` holds your knowledge base. Both are worth a periodic backup
   (`hermes backup` exists for the former).
8. **Consider exposing richer Hermes channels.** The same gateway can serve
   Telegram/Discord/etc. (per the plan) in addition to the OpenWebUI API — the
   API server and messaging platforms coexist in one gateway process.

---

_Last verified: 2026-06-02 — Hermes Agent v0.15.1, OpenWebUI `:main`,
lucidvault:branch (pure-Go, ~19 MB)._
```
