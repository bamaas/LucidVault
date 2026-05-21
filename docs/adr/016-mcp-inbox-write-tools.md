# 016 — MCP Write Tools for Inbox and Notes

**Status:** Accepted

## Context

The MCP server (ADR-015) exposes 7 read-only retrieval primitives. AI agents can search and read the vault but cannot contribute back. Users already add bookmarks by dropping `.md` files in `inbox/` (ADR-014) and notes in `notes/`. Agents should have the same capability.

## Decision

Add two write tools to the MCP server: `add_bookmark` (creates `inbox/<slug>.md`) and `add_note` (creates `notes/<slug>.md`). Both produce files in the exact format the existing poll loops expect — no new processing paths.

## Consequences

- Agents can submit URLs for the full scrape → enrich → index pipeline via `add_bookmark`
- Agents can create personal notes via `add_note`, picked up by the notes scanner for auto-tagging
- No deduplication in `add_bookmark` — consistent with ADR-014 (reprocessing is intentional)
- `add_note` overwrites existing notes with the same slug — same as manual editing
- Write scope is limited to `inbox/` and `notes/` directories only — no arbitrary vault writes
- ADR-015's "read-only" constraint is relaxed to "read + controlled writes to inbox/notes"
