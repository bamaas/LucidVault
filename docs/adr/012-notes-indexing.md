# ADR-012: Local notes indexing without LLM enrichment

## Status

Accepted

## Context

LucidVault processes bookmarks from external sources (Raindrop.io) through a scrape-enrich-index pipeline. Personal notes in `notes/` are ignored — they don't appear in `index.md` and are invisible to the enrichment prompt's context. The to-do asks for scanning notes and connecting them to the knowledge graph.

## Decision

Add a `processNotes` phase to the poll cycle that scans `notes/` for markdown files and indexes them in `index.md`. No LLM enrichment — notes are user-authored content that doesn't need summarization. Tags are extracted from YAML frontmatter only. Titles are derived from filenames. Change detection uses content hashing (SHA-256), not mtime.

## Consequences

- Notes appear in `index.md` alongside wiki entries as `[[notes/path]] — Title [tags]`, making them visible to the enrichment prompt for wiki-link suggestions
- Obsidian's graph view already renders wiki-links from note content — LucidVault doesn't need to extract or store them
- Zero additional config or API keys required
- `processNotes` runs independently of `processBookmarks` — a Raindrop API failure doesn't block notes indexing
- A `notes` table in SQLite tracks path and content hash for change detection and deletion reconciliation
- No inline hashtag extraction — frontmatter `tags:` is the single source
