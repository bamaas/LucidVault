# ADR-008: Upsert LucidVault section into host CLAUDE.md at startup

## Status
Accepted

## Context
Claude Code reads `~/.claude/CLAUDE.md` for project-agnostic instructions. LucidVault needs to teach Claude Code how to query the vault (grep index first, read wiki pages, fall back to raw). This must be idempotent, upgradeable across versions, and work in a `scratch`-based Docker image with no shell.

## Decision
Add a `upsertClaudeMD` function in `cmd/main.go` that reads, upserts (via regex marker replacement), and writes the CLAUDE.md file using pure Go. Run it once at container startup before the poll loop. The section is delimited by `<!-- lucidvault:start -->` / `<!-- lucidvault:end -->` markers. The file path defaults to `/CLAUDE.md` (overridable via `CLAUDE_MD_PATH` env var). If the file does not exist at that path (i.e. no bind-mount), the upsert is silently skipped.

## Consequences
- Works in `scratch` images — no shell or filesystem utilities needed
- Host bind-mounts `~/.claude/CLAUDE.md` to `/CLAUDE.md` — only the single file, not the entire `~/.claude` directory
- Content outside the markers is never modified
- Running multiple versions overwrites the section with the latest template
- If `/CLAUDE.md` does not exist (no bind-mount), the upsert is silently skipped — no env var needed
- `CLAUDE_MD_PATH` can override the default path for non-standard setups
