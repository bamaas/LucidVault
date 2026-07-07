# Full stack - talk to your vault from a browser or Claude Code

The full LucidVault setup: the pipeline, multi-device sync, and **two ways for AI
agents to work with your vault**, sharing one narrow write path.

- **Browser chat** - ask a question in [OpenWebUI](https://github.com/open-webui/open-webui)
  and an agent ([Hermes](https://github.com/NousResearch/hermes-agent)) explores the
  vault and answers with citations, or files a bookmark / writes a note / edits a page.
- **Claude Code on your machine** - reads the LiveSync-synced local vault directly
  and writes back through the same remote MCP server, so edits from your editor are
  deterministic and propagate to every device.

Both run with the vault **read-only** and can only mutate it through LucidVault's
MCP tools - a capable agent without the keys to your host or files.

A **worked example**, not a supported product - Hermes and OpenWebUI are
third-party. Files here: `docker-compose.agent.yml` (the full-stack compose overlay)
and `hermes.config.example.yaml` (the Hermes wiring keys).

---

## Architecture

```text
  ─── Access surface A:  browser chat ──────────────────────────────

        Browser
           │  http
           ▼
        OpenWebUI  :3000
           │  OpenAI-compatible API   (Bearer = HERMES_API_KEY)
           ▼
        Hermes  :8090   (container)
           │      reads the vault through a READ-ONLY mount
           │      all writes go through the MCP tools
           │
           ▼   writes
        ┌───────────────────────────────┐
        │   MCP   lucidvault:8080 /mcp   │
        │   graph + mutation tools       │
        └───────────────┬───────────────┘
                        │  writes land here
                        ▼
                  vault on disk
                        │
                        ▼
        livesync-bridge ──► CouchDB :5984 ──┬──► Obsidian devices
                                            │
                                            └──► your machine:  ~/…/vault
                                                        ▲
  ─── Access surface B:  editor agent ──────────────────┘

        Claude Code   (your machine)
           • reads   ── the local synced vault above   (filesystem, read-only)
           • writes  ── straight to  MCP lucidvault:8080/mcp   (never via Hermes)
```

| Part | Runs as | Role |
|---|---|---|
| `lucidvault` | container | pipeline + **in-process MCP** on `:8080` |
| `hermes` | container | OpenAI-compatible API `:8090` + agent loop + MCP client |
| `open-webui` | container | chat UI `:3000`, points at Hermes as its backend |
| `couchdb` + `livesync-bridge` | containers | multi-device sync spine |

---

## Setup

**Prerequisites:** Docker + Compose v2, the repo checked out (root
`docker-compose.yml`, `.env`, `vault/`), and an LLM API key for the agent (this
example uses Ollama Cloud). Images are pulled automatically. This overlay adds
CouchDB + livesync-bridge (sync) and Hermes + OpenWebUI (agents) on top of the
lucidvault container.

**1. Configure Hermes** (`~/.hermes`, bind-mounted to `/opt/data`).

```bash
docker run --rm -it -v ~/.hermes:/opt/data nousresearch/hermes-agent:latest model
```

Point it at the MCP server in `~/.hermes/config.yaml` (see
[`hermes.config.example.yaml`](./hermes.config.example.yaml)):

```yaml
mcp_servers:
  lucidvault:
    url: "http://lucidvault:8080/mcp"   # compose service name, NOT 127.0.0.1
```

Set the API server + LLM keys in `~/.hermes/.env`:

```dotenv
OLLAMA_API_KEY=<your-ollama-cloud-key>
API_SERVER_ENABLED=true
API_SERVER_HOST=0.0.0.0        # reachable by open-webui on the compose network
API_SERVER_PORT=8090
API_SERVER_KEY=<shared-secret> # generate with: openssl rand -hex 24
```

**2. Set the repo-root `.env`** - the shared secret must equal `API_SERVER_KEY`,
and CouchDB needs a password:

```dotenv
HERMES_API_KEY=<shared-secret>     # identical to API_SERVER_KEY above
COUCHDB_PASSWORD=<couchdb-pass>    # must match deploy/livesync-bridge/config.json
# HERMES_HOME=~/.hermes   HERMES_UID/HERMES_GID default 1000 (set to id -u / id -g)
```

**3. Mount the vault read-only at the path the agent expects.** The agent finds the
vault by the absolute path in its `HERMES.md`/`AGENTS.md`. Fresh setup: mount at
`/vault` and tell the agent so. Migrating a host-native Hermes: mount at its
existing host path (e.g. `./vault:/home/you/apps/lucidvault/vault:ro`). Either way,
`:ro`. Edit the mount in `docker-compose.agent.yml` accordingly.

**4. Bring it up:**

```bash
docker compose -f docker-compose.yml \
  -f docs/guides/agent-chat/docker-compose.agent.yml up -d
```

**5. Verify** (Hermes' API is bridge-only - check via the real path, open-webui):

```bash
KEY=$(grep -m1 ^API_SERVER_KEY= ~/.hermes/.env | cut -d= -f2-)

docker exec open-webui curl -sf http://hermes:8090/v1/models \
  -H "Authorization: Bearer $KEY"                     # → lists hermes-agent
docker exec hermes sh -c 'cd /opt/data && hermes mcp test lucidvault'
                                                       # → Connected, 7 tools
docker exec hermes sh -c 'touch <vault-mount>/__t 2>&1 || echo READ-ONLY-OK'
                                                       # → Read-only file system
```

Then open `http://<host>:3000`, create the admin account, pick **`hermes-agent`**,
and ask about your vault.

### Multi-device sync (LiveSync)

[couchdb](https://hub.docker.com/_/couchdb) + [livesync-bridge](https://github.com/vrtmrz/livesync-bridge) come up with the stack and sync the whole vault. To
use it:

- Match CouchDB credentials between `.env` and `deploy/livesync-bridge/config.json`.
- Install the [Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync) plugin
  on each device and point it at CouchDB (`:5984`, the DB name + credentials).
- **Folder ownership:** you edit `inbox/`, `notes/`, `soul.md`; LucidVault owns
  `wiki/`, `index.md` (overwritten on re-enrichment); `raw/` is large - exclude on
  mobile. Full design: `docs/design/017-couchdb-livesync.md`.

This is what makes surface B work: your machine's synced copy is what Claude Code
reads.

---

## Rationale - why it's built this way

**The problem.** Hermes is a general, code-executing agent. Pointed at a knowledge
base it's useful - it can grep the index, read wiki pages, walk the wiki-link graph,
and cite sources - but a general agent with shell access near your vault is a risk.

**The containment (kernel-enforced, not prompt-enforced):**

- **Containerized.** Hermes' only host mounts are its own home (`~/.hermes`, rw) and
  the vault (`:ro`). It cannot write anywhere else on the host - not `$HOME`, not
  system paths.
- **Vault read-only.** The agent reads vault files directly (native-first retrieval,
  ADR-023); a write is refused with `Read-only file system` even from its own shell.
- **Writes only through MCP.** The single path that mutates the vault is the MCP
  tool set (`add_note`, `update_wiki`, `edit_page`, `delete_page`, `add_bookmark`):
  structured, auditable, deterministic. Retrieval is open; mutation is a narrow gate.
- **MCP isn't world-exposed.** Loopback (or a trusted-LAN IP) plus a
  `MCP_ALLOWED_HOST` allowlist. It carries mutation tools - treat it as privileged.
- **Chat API is bearer-authed.** OpenWebUI authenticates to Hermes with the shared
  secret; Hermes is otherwise unreachable from outside the compose network.

Residual blast radius: the agent can run code in its own container and write
`~/.hermes` (skills, memory, sessions) - by design. Not the host, not the vault.

**One vault, two front ends.** Because writes funnel through MCP into the vault, and
the vault syncs over CouchDB/LiveSync, the same core serves both the browser chat
*and* an editor agent (Claude Code) on your laptop reading the synced copy directly
and writing back through the same MCP endpoint. They're independent and converge
only at MCP + the vault.

To let a LAN workstation reach MCP for writes, publish it on the host's LAN IP and
add that IP to `MCP_ALLOWED_HOST` - this exposes **mutation** tools to that LAN, so
only on a trusted network; otherwise keep MCP on loopback and tunnel
(`ssh -L 8080:127.0.0.1:8080 host`).

---

## Operations & troubleshooting

```bash
alias dc='docker compose -f docker-compose.yml -f docs/guides/agent-chat/docker-compose.agent.yml'
dc ps                          # status
dc logs -f hermes              # follow agent logs
dc restart hermes              # restart the agent
dc pull hermes && dc up -d hermes   # upgrade Hermes
```

Back up `~/.hermes` (agent state) and the vault. Rotate the shared secret in both
`.env` files together. **Never run a host-native Hermes gateway and the container
against the same `~/.hermes` at once** - they share SQLite state; disable the old
one with `systemctl --user disable --now hermes-gateway`.

| Symptom | Fix |
|---|---|
| `/v1/models` → 401 | `HERMES_API_KEY` must equal `API_SERVER_KEY`; restart both |
| OpenWebUI shows no models | same compose network? `API_SERVER_HOST=0.0.0.0`? |
| Agent: "vault doesn't exist" | RO mount path ≠ path in `HERMES.md`/`AGENTS.md` - fix the mount |
| `hermes mcp test` fails | URL uses the service name + it's in `MCP_ALLOWED_HOST` |
| Agent can't write the vault | working as intended - writes go through MCP |
