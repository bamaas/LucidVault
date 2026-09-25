# 028 — Emit a Host-Facing Vault Path in the CLAUDE.md Pointer (amends ADR-025)

## Status

Accepted (amends ADR-025)

## Context

ADR-025 reduced the injected `CLAUDE.md` block to a pointer carrying "the absolute host path of the vault" — the one fact `AGENTS.md` cannot self-supply. The implementation passes `cfg.vaultPath` (`VAULT_PATH`) straight through, and that assumption does not hold: `VAULT_PATH` is the path as seen by the *writing process*, while `CLAUDE.md` is read by an agent on the *host*. Every shipped configuration sets `VAULT_PATH=/vault` (`Dockerfile`, `docker-compose.yml`, the `docker run` example in `docs/guides/simple-setup.md`), so the block written into the host's `~/.claude/CLAUDE.md` names a container path that does not exist on the host. Deriving the host path from container-local state was evaluated and rejected: the final image is `scratch` and never sets `HOME`, and Go's `os.UserHomeDir` consults `$HOME` and nothing else, so `$HOME`-prefix stripping is a no-op in exactly the deployments that have the problem. `/proc/self/mountinfo` recovers the source path on native Linux bind mounts but only a suffix on Docker Desktop for macOS, so it cannot be relied on either.

## Decision

Take the path the block advertises from a dedicated `CLAUDE_MD_VAULT_PATH` env var describing the vault as the *reader* of `CLAUDE.md` sees it, falling back to `VAULT_PATH` when unset, and append one sentence telling the agent what to do when that directory is absent.

## Consequences

- The emitted path is configured by whoever owns the bind mount, which is the only party that knows both sides of it. No inference from container-local state, so no environment where the derivation silently degrades.
- Symmetric with the existing `CLAUDE_MD_PATH`, which already exists to describe the host side of this same mount. No new concept, one new variable.
- `docker-compose.yml`, `.env.example`, `docs/guides/simple-setup.md` and `README.md` must set or document it in the same change, otherwise the default stays `/vault` and nothing observable improves.
- The two variables can drift from the actual `-v host:container` flag and nothing validates them. Accepted: a wrong path that the user can see and correct beats a right-looking path derived from state that is structurally unavailable.
- The absent-vault sentence is worded to the tool surface that is actually always registered (`search_wiki` for discovery, `related_notes` / `expand_graph` for traversal) and does not promise MCP content reads, which ADR-023 gates off by default. It instructs the agent to report the vault as unavailable rather than guess — the failure this ADR exists to prevent is an agent confidently reading a directory that is not there.
- Stays inside ADR-025's pointer constraint: path plus "read `AGENTS.md` and follow it" plus one fallback sentence. No retrieval strategy, no file legend, no web-search guidance.
- Does not help a machine that already carries a block: see ADR-027 — a stock legacy block is shape-matched and upgraded, so the corrected path does reach existing installs.
