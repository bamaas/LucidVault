# ADR-004: index.md over Vector Search

## Status
Accepted

## Context
Need a way to find related notes for linking during enrichment. Options: vector embeddings (sqlite-vec), full-text search, or a simple index file.

## Decision
Use `index.md` — a flat list of all wiki pages with titles and tags, passed to the LLM as context.

## Consequences
- Zero additional infrastructure
- Works well for hundreds of pages (fits in LLM context)
- Human-readable, editable, debuggable
- Will need upgrade to vector search at ~100+ pages if linking quality drops
- No embedding API costs
