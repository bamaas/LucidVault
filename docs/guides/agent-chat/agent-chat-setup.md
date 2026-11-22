# Full stack - talk to your vault from a browser or Claude Code

The full LucidVault setup: the pipeline, multi-device sync, and **two ways for AI
agents to work with your vault**, sharing one narrow write path.

- **Browser chat** - ask a question in [OpenWebUI](https://github.com/open-webui/open-webui)
  and a coding agent's CLI, run inside an [Open Terminal](https://github.com/open-webui/open-terminal)
  container, explores the vault and answers with citations, or files a bookmark /
  writes a note / edits a page. **Claude Code is the worked example below; Codex or
  any other [ACP](https://agentclientprotocol.com/)-compatible agent is wired up the
  same way** - swap the sign-in step and the CLI name.
- **Claude Code on your machine** - reads the LiveSync-synced local vault directly
  and writes back through the same remote MCP server, so edits from your editor are
  deterministic and propagate to every device.

Both run with the vault **read-only** and can only mutate it through LucidVault's
MCP tools - a capable agent without the keys to your host or files.

A **worked example**, not a supported product - OpenWebUI and Open Terminal are
third-party. File here: `docker-compose.agent.yml` (the full-stack compose
overlay). Decision record: `docs/adr/032-agent-chat-via-open-terminal.md`.

---

## Architecture

```text
  ─── Access surface A:  browser chat ──────────────────────────────

        Browser
           │  http
           ▼
        OpenWebUI  :3000
           │  Terminal Server connection   (Bearer = OPEN_TERMINAL_API_KEY)
           ▼
        Open Terminal  :8000   (container)
           │      launches the agent CLI (e.g. `claude --input-format
           │      stream-json --output-format stream-json`), inheriting the
           │      container's env (login token/key, MCP registration)
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

        Claude Code   (your machine, independent of the container above)
           • reads   ── the local synced vault above   (filesystem, read-only)
           • writes  ── straight to  MCP lucidvault:8080/mcp   (never via OpenWebUI)
```

| Part | Runs as | Role |
|---|---|---|
| `lucidvault` | container | pipeline + **in-process MCP** on `:8080` |
| `open-terminal` | container | runs the agent CLI, exposes it to OpenWebUI on `:8000` |
| `open-webui` | container | chat UI `:3000`, connects to Open Terminal as a "Terminal Server" |
| `couchdb` + `livesync-bridge` | containers | multi-device sync spine |

---

## Setup

**Prerequisites:** Docker + Compose v2, the repo checked out (root
`docker-compose.yml`, `.env`, `vault/`), and an LLM API key for enrichment (this
example uses Ollama Cloud) plus a way to sign the agent in to Claude (subscription
login or an API key - see step 3). Images are pulled automatically. This overlay
adds CouchDB + livesync-bridge (sync) and Open Terminal + OpenWebUI (agents) on top
of the lucidvault container.

**0. Pin and back up OpenWebUI before any upgrade.** `docker-compose.agent.yml`
pins `ghcr.io/open-webui/open-webui:v0.13.4` (not `:main`) - each release can run
database migrations on first start. Before pulling a newer tag, back up the
`open-webui` volume:

```bash
docker run --rm -v open-webui:/data -v "$PWD":/backup alpine \
  tar czf /backup/open-webui-backup-$(date +%F).tar.gz -C /data .
```

Then `docker compose pull open-webui && docker compose up -d open-webui` and check
the logs for a clean migration before trusting it with real chats.

**1. Generate the Open Terminal API key** and set it in the repo-root `.env`
(this is the bearer secret between OpenWebUI and Open Terminal, not an LLM key):

```dotenv
OPEN_TERMINAL_API_KEY=<generate with: openssl rand -hex 24>
COUCHDB_PASSWORD=<couchdb-pass>    # must match deploy/livesync-bridge/config.json
```

**2. Image tag.** `docker-compose.agent.yml` pins
`ghcr.io/open-webui/open-terminal:0.17.21-agents` (confirmed against a live
deployment on Nov 22, 2026), following the project's `<version>-agents` tag
convention. Pin an exact tag rather than tracking a floating one - tags move
faster than this guide. If you bump it, check the
[package page](https://github.com/open-webui/open-terminal/pkgs/container/open-terminal)
for the current `<version>-agents` release (`docker manifest inspect
ghcr.io/open-webui/open-terminal:<tag>` also confirms a tag exists) and update
both `docker-compose.agent.yml` and this note.

**3. Sign the agent in.** Open Terminal launches the agent CLI inside the
container and the CLI inherits the container's environment, so either works:

- **Subscription login** - run `claude setup-token` on any machine where you're
  already logged in to get a long-lived token, then set it in `.env`:

  ```dotenv
  CLAUDE_CODE_OAUTH_TOKEN=<token from: claude setup-token>
  ```

- **API key** - set `ANTHROPIC_API_KEY=<key>` in `.env` instead.

  Set only one of the two in `.env` (leave the other blank) - `docker-compose.agent.yml`
  passes both through as empty-string defaults. For Codex or another ACP agent,
  substitute that agent's own login/token mechanism here; the rest of this guide
  is unaffected.

**4. Mount the vault read-only and point the agent at it.** The compose file
mounts `./vault:/home/user/vault:ro` - Open Terminal's `HOME` is `/home/user`,
persisted in the `open_terminal_home` named volume (also where the agent's login,
MCP registration, and session state live). If you need a different host path,
edit the left side of that mount; keep the right side and the `:ro` as-is.

**5. Agent instructions: point the agent at the vault's `AGENTS.md`.** LucidVault
generates `vault/AGENTS.md` with the retrieval and citation rules every agent
should follow (ADR-023, ADR-024). Give the containerized agent a `CLAUDE.md` that
states the read-only boundary up front and imports `AGENTS.md`, in a dedicated
start directory inside the persisted home volume:

```bash
docker compose up -d open-terminal   # creates the home volume if needed
docker compose exec -u user open-terminal mkdir -p /home/user/workspace
docker compose exec -u user open-terminal tee /home/user/workspace/CLAUDE.md >/dev/null <<'EOF'
Vault at /home/user/vault is read-only. Follow /home/user/vault/AGENTS.md for
retrieval and citations. Write only via the lucidvault MCP tools. Never modify
files under /home/user/vault.

@/home/user/vault/AGENTS.md
EOF
```

`OPEN_TERMINAL_START_DIR=/home/user/workspace` in `docker-compose.agent.yml` makes
this the agent's working directory, so Claude Code loads that `CLAUDE.md` -
stating the read-only boundary explicitly, then through its `@`-import pulling
in the vault's `AGENTS.md` - every session. (Codex/other ACP agents: use the
equivalent memory-file mechanism in the same directory.)

**6. Register the MCP server** for the Open Terminal user (persisted in
`~/.claude.json` inside the home volume, so this is a one-time step per volume):

```bash
docker compose exec -u user open-terminal \
  claude mcp add --scope user --transport http lucidvault http://lucidvault:8080/mcp
docker compose exec -u user open-terminal claude mcp list
                                          # → lucidvault: ... - ✔ Connected
```

`MCP_ALLOWED_HOST` on the `lucidvault` service already includes `lucidvault` (the
compose service name Open Terminal connects to) - `docker-compose.agent.yml` sets
this for you.

**7. Bring the rest of the stack up:**

```bash
docker compose -f docker-compose.yml \
  -f docs/guides/agent-chat/docker-compose.agent.yml up -d
```

OpenWebUI picks up the Open Terminal connection declaratively from
`TERMINAL_SERVER_CONNECTIONS` in the compose file - no manual wiring needed. If you
ever need to do it by hand (or just want to confirm it took): **Admin → Settings →
Integrations → Open Terminal**, URL `http://open-terminal:8000`, the API key from
`.env`.

OpenWebUI only reads `TERMINAL_SERVER_CONNECTIONS` and initializes terminal
servers at startup, so the overlay's `depends_on: [open-terminal]` on
`open-webui` matters - without it, OpenWebUI can start before Open Terminal is
ready to answer. If `docker compose logs open-webui` shows `Initialized 0
terminal server(s)`, Open Terminal wasn't up in time: `docker compose restart
open-webui` once it is.

**8. Verify tool approvals are on.** `ENABLE_TOOL_PERMISSIONS=True` and
`ENABLE_TOOL_PERMISSIONS_AUTO=True` are set in the compose file (both are the
verified env var names and both are applied); confirm under OpenWebUI's Admin
settings for your version - the default mode lets reads through and holds
writes/commands for your approval in chat.

**9. Chat.** In OpenWebUI, start a **new chat**, select the Open Terminal
connection as the model source, then pick `claude-code/<model>` from the picker
(the model list is populated once the terminal is selected - Open Terminal
listens on `:8000` and serves this from its own `/agents` endpoint, bearer-authed
with `OPEN_TERMINAL_API_KEY`; it lists the `claude-code` agent together with
whatever models the CLI currently reports, e.g. `claude-sonnet-5-5`,
`claude-opus-5-5`, `claude-haiku-5-5` - that list comes from the CLI and changes
over time, so don't treat any particular model name here as authoritative). Ask
it a question about your vault, approve the first write it proposes, and confirm
an attempt to edit a vault file directly fails (`Read-only file system`).

### Multi-device sync (LiveSync)

[couchdb](https://hub.docker.com/_/couchdb) + [livesync-bridge](https://github.com/vrtmrz/livesync-bridge) come up with the stack and sync the whole vault. To
use it:

- Match CouchDB credentials between `.env` and `deploy/livesync-bridge/config.json`.
- Install the [Obsidian LiveSync](https://github.com/vrtmrz/obsidian-livesync) plugin
  on each device and point it at CouchDB (`:5984`, the DB name + credentials).
- **Folder ownership:** you edit `inbox/`, `notes/`, `soul.md`; LucidVault owns
  `wiki/`, `index.md` (overwritten on re-enrichment); `raw/` is large - exclude on
  mobile. Full design: `docs/design/017-couchdb-livesync.md`.

This is what makes surface B work: your machine's synced copy is what your own
local Claude Code reads - independent of the Open Terminal container above.

---

## Verification

Checks run against a live deployment (Nov 22, 2026):

- **Sign-in** - `docker compose exec -u user open-terminal claude -p "Reply with
  exactly: ok"` returns `ok`.
- **MCP connected** - `docker compose exec -u user open-terminal claude mcp list`
  shows `lucidvault` as `✔ Connected`.
- **Read-only enforced** - `docker compose exec -u user open-terminal touch
  /home/user/vault/x` fails (`Read-only file system`).
- **End to end** - from OpenWebUI, select the Open Terminal connection, then pick
  the `claude-code` model from the picker and send a message.

---

## Rationale - why it's built this way

**The problem.** A coding agent is general and code-executing. Pointed at a
knowledge base it's useful - it can grep the index, read wiki pages, walk the
wiki-link graph, and cite sources - but a general agent with shell access near
your vault is a risk. See `docs/adr/032-agent-chat-via-open-terminal.md` for why
this landed on Open Terminal over the alternatives (keeping an always-on agent
daemon, a bespoke OpenAI-compatible proxy, a read-write mount).

**The containment (kernel-enforced, not prompt-enforced):**

- **Containerized.** Open Terminal's only host-adjacent mounts are its own home
  (`open_terminal_home`, rw) and the vault (`:ro`). It cannot write anywhere else
  on the host - not `$HOME`, not system paths.
- **Vault read-only.** The agent reads vault files directly (native-first
  retrieval, ADR-023); a write is refused with `Read-only file system` even from
  its own shell.
- **Writes only through MCP.** The single path that mutates the vault is the MCP
  tool set (`add_note`, `update_wiki`, `edit_page`, `delete_page`, `add_bookmark`):
  structured, auditable, deterministic. Retrieval is open; mutation is a narrow gate.
- **MCP isn't world-exposed.** Loopback (or a trusted-LAN IP) plus a
  `MCP_ALLOWED_HOST` allowlist. It carries mutation tools - treat it as privileged.
- **Chat connection is bearer-authed.** OpenWebUI authenticates to Open Terminal
  with `OPEN_TERMINAL_API_KEY`; Open Terminal is otherwise unreachable from outside
  the compose network (no published port).
- **Tool approvals in chat.** `ENABLE_TOOL_PERMISSIONS=True` and
  `ENABLE_TOOL_PERMISSIONS_AUTO=True` hold writes and shell commands for your
  approval; reads run free.
- **Browser/desktop endpoints disabled.** `OPEN_TERMINAL_ENABLE_BROWSER=false`
  and `OPEN_TERMINAL_ENABLE_DESKTOP=false` turn off Open Terminal's own
  browser-automation and desktop HTTP/WebSocket endpoints - this deployment only
  needs the terminal, so there's no reason to expose the other two.
- **Recommended: `no-new-privileges`.** The image grants the `user` account
  passwordless `sudo`; `security_opt: [no-new-privileges:true]` on
  `open-terminal` blocks that escalation path without affecting the agent CLI or
  MCP registration.
- **Recommended: a memory ceiling.** `mem_limit: 1536m` on `open-terminal` caps
  the container; idle usage measured ~316 MiB on a live deployment, so this
  leaves headroom without being unbounded.

Residual blast radius: the agent can run code in its own container and write its
own home volume (login, MCP config, session state) - by design. Not the host, not
the vault. Optional further hardening: an egress allowlist via
`OPEN_TERMINAL_ALLOWED_DOMAINS` (needs `cap_add: [NET_ADMIN]`, commented out in
`docker-compose.agent.yml`).

**One vault, two front ends.** Because writes funnel through MCP into the vault,
and the vault syncs over CouchDB/LiveSync, the same core serves both the browser
chat *and* an editor agent (Claude Code) on your laptop reading the synced copy
directly and writing back through the same MCP endpoint. They're independent and
converge only at MCP + the vault.

To let a LAN workstation reach MCP for writes, publish it on the host's LAN IP and
add that IP to `MCP_ALLOWED_HOST` - this exposes **mutation** tools to that LAN, so
only on a trusted network; otherwise keep MCP on loopback and tunnel
(`ssh -L 8080:127.0.0.1:8080 host`).

---

## Operations & troubleshooting

```bash
alias dc='docker compose -f docker-compose.yml -f docs/guides/agent-chat/docker-compose.agent.yml'
dc ps                                  # status
dc logs -f open-terminal               # follow agent/CLI logs
dc restart open-terminal               # restart the agent container
dc pull open-terminal open-webui && dc up -d open-terminal open-webui
                                        # upgrade both - bump the pinned tags
                                        # in docker-compose.agent.yml first,
                                        # and back up the open-webui volume
                                        # (step 0) before an OpenWebUI bump
```

**Rollback.** If an upgrade goes wrong: restore the `open-webui` volume backup
from step 0, revert both image tags in `docker-compose.agent.yml` to the previous
pinned versions, and `dc up -d`.

**Limits to know about:**

- Agent chats in OpenWebUI cannot be regenerated or edited (an Open WebUI
  limitation, not LucidVault's) - start a new chat instead.
- Agent models only appear in saved chats, and only after you've selected the
  Open Terminal connection as the chat's model source - a fresh chat with no
  terminal selected won't show `claude-code/<model>` in the picker.
- As of the versions pinned here, image attachments to agent chats are known to
  be partially broken, and the model picker can silently fall back to a
  non-agent model in some flows - check `open-webui/open-webui` issues for the
  current state before relying on either.
- LucidVault's MCP `delete_page` removes wiki pages but not a note's source file
  in `notes/` - an agent that called `add_note` cannot fully undo it through MCP
  alone; the leftover file in `notes/` needs manual cleanup.

| Symptom | Fix |
|---|---|
| OpenWebUI shows no `claude-code/*` models | Did you select the Open Terminal connection first? Is `TERMINAL_SERVER_CONNECTIONS` set and does `OPEN_TERMINAL_API_KEY` match on both services? |
| `claude mcp list` shows lucidvault disconnected | URL uses the compose service name (`lucidvault`, not `127.0.0.1`), and `lucidvault` is in `MCP_ALLOWED_HOST` |
| Agent: "vault doesn't exist" / can't find AGENTS.md | Check `OPEN_TERMINAL_START_DIR` and that `/home/user/workspace/CLAUDE.md` exists and imports `/home/user/vault/AGENTS.md` |
| OpenWebUI logs `Initialized 0 terminal server(s)` | Open Terminal wasn't ready when OpenWebUI started - `docker compose restart open-webui` |
| Agent can't write the vault | Working as intended - writes go through MCP |
| No tool-approval prompt in chat | `ENABLE_TOOL_PERMISSIONS` not actually on - check Admin settings for your OpenWebUI version |
| Sign-in token rejected inside the container | Re-run `claude setup-token` and update `.env`, or switch to `ANTHROPIC_API_KEY` |
