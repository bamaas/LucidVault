# Plan — Portable CLAUDE.md pointer + divergence guard

**ADRs:** [027](../adr/027-claudemd-divergence-guard.md) (divergence guard), [028](../adr/028-host-facing-vault-path-for-claudemd.md) (host-facing path). Amends [025](../adr/025-claudemd-pointer-supersedes-008.md).

**Component:** `internal/claudemd` + its caller in `cmd/main.go`, plus the deployment files that feed it.

## Problem

`claudemd.Upsert` writes a marker-delimited block into the host's `~/.claude/CLAUDE.md`. Two defects, both verified against the tree:

1. **The advertised path is the writing process's path, not the reader's.** Every shipped configuration sets `VAULT_PATH=/vault` (`Dockerfile:17`, `docker-compose.yml:14`, the `docker run` example at `docs/guides/simple-setup.md:102-108`), so the block on the host says `/vault` — a path that does not exist there. When the vault is absent altogether (dev container), the block still instructs the agent to read it, with no alternative offered.
2. **Regeneration destroys hand-authored content.** `markerRe.ReplaceAllString` (`internal/claudemd/claudemd.go:15`) replaces everything between the markers on every pipeline start. Content a user wrote inside the markers is discarded with no warning and no backup. This is live: a real `~/.claude/CLAUDE.md` was found carrying hand-written guidance inside the marker region.

Defect 2 is the urgent one — it is currently silent data loss.

## Goals

- The emitted path is correct for the agent that reads the file, on macOS, on a headless Linux VM, and when the writer runs in a container.
- A block the user has edited is never overwritten; divergence is reported, not swallowed.
- A block the generator wrote still upgrades itself, including the stock blocks already carrying the wrong path.
- The block stays a pointer per ADR-025: path, "read `AGENTS.md` and follow it", one absent-vault sentence. Nothing else.

## Non-goals

- The `MCP_READ_TOOLS` / `search_wiki` content-read gap. Separate issue; this plan must not widen the MCP surface or argue against ADR-023's default.
- Deriving the host path from container-local state ($HOME stripping, `/proc/self/mountinfo`). Evaluated and rejected in ADR-028.
- Any change to content outside the markers, or to `AGENTS.md`.

## Scope

### 1. Host-facing vault path (`cmd/main.go`, `internal/claudemd`)

- Add a `claudeMDVaultPath` field to `config`, read from `CLAUDE_MD_VAULT_PATH` in `loadConfig`, defaulting to the empty string.
- At the `Upsert` call site (`cmd/main.go:80-91`), pass `cfg.claudeMDVaultPath` when non-empty, otherwise `cfg.vaultPath`.
- When the fallback is used and a block is actually written, log at warn level that the emitted path is the pipeline's own `VAULT_PATH` and may not resolve for the reader, naming both the emitted path and `CLAUDE_MD_VAULT_PATH`. A wrong path must be diagnosable from the logs.
- The path is emitted verbatim. No `~` expansion, no normalisation, no trailing-slash rewriting.

### 2. Absent-vault fallback sentence (`internal/claudemd`)

Extend `sectionTemplate` with exactly one sentence covering the vault-absent case. It must be true of the default deployment:

- It may reference the `lucidvault` MCP server as *possibly configured*, and may name only always-on tools — `search_wiki` (discovery, metadata only), `related_notes` / `expand_graph` (traversal).
- It must **not** promise MCP content reads: `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `vault_overview` and `get_soul` are gated behind `MCP_READ_TOOLS`, default `false`.
- It must tell the agent to report the vault as unavailable rather than guess or fabricate.
- It must not restate retrieval strategy, the file legend, or web-search guidance (ADR-025).

### 3. Divergence guard (`internal/claudemd`)

- Compute a checksum over the generated body (the text between the markers, excluding the checksum comment itself) and emit it as an HTML comment inside the block.
- On upsert, when a block already exists:
  - **Checksum present and matches the current body** → generator-owned. Rewrite.
  - **Checksum present and does not match** → user-edited. Skip the write, return a signal the caller can log as a warning naming the file.
  - **No checksum (legacy block)** → match the body against the pre-checksum ADR-025 template shape with any vault path. Exact shape match → generator-written, rewrite (this is how existing installs pick up the corrected path). Anything else → treat as diverged, skip and warn.
- Skipping is not an error. The caller distinguishes "wrote it", "skipped, diverged" and "failed"; only the last is an error.
- Content outside the markers is still never touched (ADR-008).

### 4. Deployment + docs

Without these the default output is unchanged and the fix reaches nobody:

- `docker-compose.yml` — pass `CLAUDE_MD_VAULT_PATH=${CLAUDE_MD_VAULT_PATH:-}` through the `environment` block.
- `.env.example` — document it next to the other optional vars: what it is for, why it differs from `VAULT_PATH`, and an example value.
- `docs/guides/simple-setup.md` — the `docker run` snippet (section 5) must set `-e CLAUDE_MD_VAULT_PATH=~/lucid-vault` alongside the existing mounts, with one line saying why the container path is not what the host agent needs.
- `README.md` — add the `CLAUDE_MD_VAULT_PATH` row to the env table, next to `CLAUDE_MD_PATH`.
- `CLAUDE.md` — add `CLAUDE_MD_VAULT_PATH` to the Environment Variables section.

## Acceptance criteria

1. `Upsert` on a file whose marker block was written by a previous `Upsert` rewrites that block and leaves exactly one marker pair.
2. `Upsert` on a file whose marker block has been edited by hand leaves the file byte-identical and reports the skip to the caller.
3. `Upsert` on a file whose marker block is the stock pre-checksum template (any vault path) upgrades it to the new form.
4. `Upsert` on a file whose marker block is a pre-checksum block with *added* user prose leaves it byte-identical and reports the skip.
5. `Upsert` on a file with no marker block appends one, preserving all pre-existing content.
6. Repeated `Upsert` calls are idempotent: one marker pair, and the file stops changing after the first run.
7. The emitted block contains the path passed in, `AGENTS.md`, an instruction to follow it, and the absent-vault sentence.
8. The emitted block does not contain `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `vault_overview`, `get_soul`, `Retrieval Strategy`, `Grep index.md`, or `Vault Structure`.
9. `CLAUDE_MD_VAULT_PATH`, when set, is the path that appears in the block; when unset, `VAULT_PATH` appears instead.
10. Content outside the markers is unchanged in every case above.

## Edge cases

- Target file does not exist → unchanged behaviour (caller stats it first and skips).
- Target file exists but is empty → block is appended, no leading blank lines.
- Unterminated markers (start present, end missing) → the regex does not match, so the block is appended rather than the rest of the file being swallowed. Assert this, it is the destructive failure mode.
- Two marker pairs in one file → only the first is replaced; the count of marker pairs must not grow.
- `CLAUDE_MD_VAULT_PATH` set to whitespace → treated as unset.
- Vault path containing regex metacharacters or `%` → must survive templating and shape-matching intact.
- File is read-only → returns a wrapped error, pipeline continues (best-effort call site).

## Verification

`mise run test`, `mise run lint`. New behaviour covered by unit tests in `internal/claudemd` and a config test in `cmd` for the env-var precedence.
