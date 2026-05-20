# 015 — MCP Server with Retrieval Primitives

**Status:** Accepted

## Context

LucidVault's retrieval strategy is encoded as prose instructions in `CLAUDE.md`, which only works for Claude Code. Other AI clients (Cursor, Windsurf, OpenClaw, mobile agents) cannot access the vault's knowledge hierarchy. We need a portable interface that preserves the retrieval philosophy (index → wiki → notes → raw) without collapsing into opaque RAG.

## Decision

Add a thin MCP server embedded in the existing Go binary (`lucidvault mcp` subcommand) that exposes 7 read-only retrieval primitives. Supports dual transport: stdio (default, for Claude Code/Cursor) and Streamable HTTP (`--http :8080`, for OpenClaw/mobile/remote agents). The retrieval hierarchy is encoded structurally through separate tools with descriptive names and guidance — agents orchestrate retrieval, the vault does not reason for them. Use `github.com/mark3labs/mcp-go` as the SDK.

## Consequences

- New dependency: `github.com/mark3labs/mcp-go`
- New package: `internal/mcpserver/` (server setup, tool handlers, parsing helpers)
- New subcommand: `lucidvault mcp` (stdio) or `lucidvault mcp --http :8080` (Streamable HTTP)
- Read-only access — MCP server never writes to the vault
- Tools: `get_soul`, `search_index`, `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `related_notes`
- No FTS5 or vector search initially — uses grep/string matching on existing files
- Forward wiki-links only for `related_notes` (no backlink graph yet)
- Streamable HTTP enables remote access from mobile/OpenClaw without separate service
