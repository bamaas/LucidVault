# ADR-014: Auto-tag notes via wiki copies

## Status

Accepted

## Context

Notes in `notes/` are indexed in `index.md` but never enriched. Notes without tags are hard to discover via search. The bookmark pipeline already follows a raw → wiki pattern where scraped content gets an enriched wiki copy. Notes lack this treatment — they appear in the index pointing directly to `notes/` paths, creating an inconsistency.

## Decision

All notes get a wiki copy in `wiki/`. The `index.md` always points to wiki pages, never to `notes/` directly. For notes without tags in frontmatter, the LLM generates 3-5 tags via a lightweight `SuggestTags` call. For notes with existing tags, the wiki copy uses those tags directly (no LLM call). The original note files are never mutated.

## Consequences

- Notes become discoverable via auto-generated tags in the wiki copy and index
- `index.md` consistently points to `wiki/` for both bookmarks and notes
- Notes without tags trigger an LLM call (lightweight, tag-only prompt) adding latency and API cost
- Notes with user-curated tags skip the LLM — respecting manual curation
- Original `notes/` files remain untouched; `wiki/` is the enriched layer
- The `notes` SQLite table gains a `wiki_path` column to track the wiki copy for cleanup
- Slug collisions between bookmark wiki pages and note wiki pages are theoretically possible but unlikely
