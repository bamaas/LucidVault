# LucidVault — Ubiquitous Language

Domain glossary for LucidVault. Pure terminology — no implementation details.

## Core Concepts

**Vault**: An Obsidian-compatible directory of markdown files that forms the user's personal knowledge base. Contains wiki pages, raw pages, notes, and an index.

**Inbox**: The single entry point for all URLs entering the system. A directory of markdown files (`inbox/*.md`), each representing one URL to process. All sources (manual or automated) create inbox files — nothing bypasses the inbox.

**Feeder**: An optional external source that creates inbox files automatically. Raindrop.io is currently the only feeder. Feeders do not process URLs — they only populate the inbox.

## Content Types

**Raw Page**: The full scraped content of a URL, stored as markdown in the vault. Unprocessed — preserves the original source material.

**Wiki Page**: An LLM-enriched summary of a raw page or note, stored as markdown in the vault. Contains tags, key takeaways, and wiki-links. The primary reading format.

**Note**: A user-authored markdown file in the vault (not scraped from a URL). Notes can be auto-tagged and get a wiki page copy with tags.

**Inbox Item**: A markdown file in the inbox directory representing a URL to process. May contain optional YAML frontmatter (title, tags). Deleted after successful processing.

## Processing

**Enrichment**: The process of sending scraped content to an LLM and receiving a wiki-formatted summary with tags, key takeaways, and wiki-links.

**Scraping**: Fetching the content of a URL and converting it to markdown. Uses Jina Reader for web pages and Supadata for YouTube transcripts.

**Tag Suggestion**: The process of sending a note's content to an LLM to generate relevant tags. Applied to notes that lack tags.

## Organization

**Index**: A single markdown file (`index.md`) in the vault root that catalogs all processed pages. Append-only, idempotent by slug.

**Slug**: A URL-safe, filesystem-safe identifier derived from a page title. Used as the filename for raw and wiki pages.

## State

**Bookmark**: A record in the SQLite database representing a processed URL. Used for deduplication — if a URL's bookmark exists, it won't be reprocessed.

**Content Hash**: A hash of a note's content stored in the database. Used to detect changes — only notes with changed content are re-enriched.

## Retrieval

**Agent Web Search**: Web search performed by the AI agent using its *own* configured search capability. LucidVault does not perform, proxy, or provide web search — it only instructs the agent how to use whatever search tool the agent already has.

**Web Search Strategy**: The vault's instruction to the agent (carried in AGENTS.md) on whether and when to use Agent Web Search relative to the vault, and how to rank and attribute results. The curated vault is weighted above web results by default, though recency can override for time-sensitive questions. Configurable, and can be turned off entirely.

## Infrastructure

**LiveSync**: Obsidian LiveSync, a community plugin that synchronizes vaults across devices via CouchDB replication. Handled entirely at the infrastructure layer — zero application code changes.

**LiveSync Bridge**: A sidecar service that bidirectionally syncs between CouchDB (LiveSync format) and a local filesystem. Shares a persistent volume with LucidVault.
