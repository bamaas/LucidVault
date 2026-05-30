# Plan: Vault Hygiene

> Depended on by: [plan-agent-retrieval.md](plan-agent-retrieval.md) (uses edges table + MCP write tools)

## Problem

LucidVault's vault degrades silently over time:

1. **Static links** — `## Related` sections are frozen at enrichment time. Pages added later are never linked back.
2. **One-directional links** — A links to B, but B doesn't link back to A. Early pages are systematically under-connected.
3. **Broken links** — `[[wikilinks]]` pointing to deleted or never-created pages.
4. **Stale index** — index.md entries referencing files no longer on disk, or wiki files missing from index.
5. **Orphan pages** — wiki pages with zero inbound links, unreachable from navigation.
6. **No mutation tools** — MCP server can only create bookmarks/notes. Cannot edit, update, or delete existing pages.
7. **Tag drift** — frontmatter tags edited after enrichment, index.md out of sync.
8. **Raw↔wiki drift** — orphaned raw files when wiki is deleted, broken footer links when raw is deleted.

## Design Principles

- **LucidVault owns its own health** — no dependency on Obsidian plugins or external agents.
- **Prevention over detection** — auto-linking at enrichment stops most issues at source.
- **Auto-fix what's safe** — remove broken edges, sync index entries, delete orphaned raw files, fix broken footer links. Never auto-delete wiki files.
- **Wiki directory is source of truth** — index.md is a derived artifact kept in sync by hygiene cycle.
- **Minimal code** — no separate hygiene framework. Auto-fix is ~50 lines. Auto-linking is a natural pipeline extension.

## Design Decisions

Decisions made during plan review. Rationale preserved for future reference.

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Edges are wiki-to-wiki only. Filter `*.md` targets from `ParseWikiLinks`. | Raw file back-refs (e.g. `[[2024-01-15-foo.md]]`) are structural, not semantic. They pollute graph queries and trigger false broken edges. The link stays in page content — just not in the edges table. |
| D2 | `ParseWikiLinks` splits on `\|` and takes first part. | Obsidian supports `[[slug\|Display Name]]`. Without this, edge targets include display text and never resolve. |
| D3 | Auto-added back-links use format `[[slug]] — shared tags: x, y`. | Explains *why* the link exists without an LLM call. Helps agents prioritize which links to follow during retrieval. Deterministic, zero cost. |
| D4 | Cross-process write safety via SQLite exclusive transaction. | Poll process and MCP server are separate OS processes. SQLite's `BEGIN EXCLUSIVE` provides cross-process mutex. All wiki file mutations wrapped in exclusive tx. External edits (user in editor) caught by hygiene cycle. |
| D5 | Edge rebuild on first run (empty table) + `--rebuild-edges` flag. Not every startup. | Every-startup is wasteful at scale. Empty table detects fresh install / migration. Flag is escape hatch for corruption. Incremental sync + hygiene cycle handle normal drift. |
| D6 | Backlink cap ordering: tag overlap DESC → file mtime DESC → slug ASC. | Most relevant pages first. File mtime as recency proxy — cheap via `os.Stat`, avoids reading frontmatter for `date_saved`. Alphabetical slug as deterministic last resort. |
| D7 | `delete_page` uses `wiki_path` column to delete bookmarks. No slug column migration. | Bookmarks table has `wiki_path` but no `slug`. Construct path as `"wiki/" + slug + ".md"` and delete by that. Avoids schema change. Notes deleted via existing `store.DeleteNote()`. |
| D8 | Tag lookup for auto-linking uses index.md via `ParseIndexEntry`. Tag sync handled by hygiene cycle (D10). | `ParseIndexEntry` already exists. No schema change. Fast enough for <1000 pages. Hygiene cycle keeps index tags in sync with frontmatter. |
| D9 | `UpdateRelatedSection` creates `## Related` if absent. Insert before LucidVault footer pattern, or append at end. | Manually created pages and notes may lack `## Related`. Silently skipping defeats auto-linking. Footer detected by `---` followed by `*Source:` line — not any bare `---` (which could be a horizontal rule). |
| D10 | Bidirectional index sync in hygiene cycle: 3 directions in single pass. | Wiki directory is source of truth. Index.md is derived. One scan handles stale removal, missing addition, and tag/title sync. |
| D11 | Auto-linking excludes the new page itself from candidates. | New page shares all tags with itself — would always match. Filter `newSlug` from candidate list before sorting. |
| D12 | `delete_page` collects inbound edges before deleting them. | Must call `GetInboundEdges(slug)` before `DeleteEdgesInvolving(slug)` to build the "dangling reference" return list. |
| D13 | `FindBrokenEdges` accepts a file-exists checker function. | Store is database-only — can't check filesystem. Pass `v.FileExists` as `exists func(slug string) bool` parameter. Keeps Store testable. |
| D14 | Orphaned raw files (no matching wiki) → auto-delete. | Raw is a reproducible cache (re-scrape URL). Wiki is source of truth. Orphaned raw is dead weight with no discovery path. Safe to delete — not user-authored content. |
| D15 | Wiki with missing raw → rewrite `*Source:` footer to use original URL from frontmatter. | Deleted raw creates broken local link in wiki footer. Fix preserves provenance (original URL still in frontmatter `url:` field) without data loss. Don't delete wiki — it holds all enriched value. |

---

## Solution

### Part 1: Edges Table

Add an `edges` table to the existing SQLite database. Populated from `[[wikilinks]]` in wiki pages. Foundation for bidirectional traversal, auto-fix, and retrieval features (used by agent-retrieval plan).

No new dependencies. Pure Go, same SQLite instance.

#### Schema

```sql
CREATE TABLE IF NOT EXISTS edges (
    from_slug TEXT NOT NULL,
    to_slug   TEXT NOT NULL,
    type      TEXT NOT NULL DEFAULT 'wikilink',
    created   TEXT NOT NULL,
    PRIMARY KEY (from_slug, to_slug, type)
);

CREATE INDEX idx_edges_to   ON edges(to_slug);
CREATE INDEX idx_edges_type ON edges(type);
```

**Edge scope (D1):** Only wiki-to-wiki links. Raw file references (`*.md` targets) are filtered out during parsing. Self-edges filtered out (`from_slug != to_slug`).

**Edge types (start with one, extend later):**

- `wikilink` — parsed from `[[slug]]` in wiki page content.
- Future: `shared_tag` (pages sharing 2+ tags), `co_sourced` (pages from same domain).

#### Edge Lifecycle

**Full rebuild** (first run when edges table is empty, or `--rebuild-edges` flag) **(D5)**:

1. Parse all `[[wikilinks]]` from each `wiki/**/*.md` file (recursive, includes `wiki/notes/`) using hardened `ParseWikiLinks()`.
2. `DELETE FROM edges WHERE type = 'wikilink'`.
3. `INSERT OR IGNORE` all parsed edges with `created = file mod time`.

Idempotent. <1 second on a 1,000-page vault.

**Incremental update** (after each enrichment in `processInboxItem` / `processNotes`):

1. `DELETE FROM edges WHERE from_slug = ? AND type = 'wikilink'`.
2. Parse `[[wikilinks]]` from newly written wiki content.
3. `INSERT OR IGNORE` new edges.

**Deletion** (when a page is removed):

1. `DELETE FROM edges WHERE from_slug = ? OR to_slug = ?`.

#### Store Methods

```go
type Edge struct {
    FromSlug string
    ToSlug   string
    Type     string
    Created  string
}

func (s *Store) RebuildEdges(edgeType string, edges []Edge) error
func (s *Store) UpsertEdgesFrom(fromSlug, edgeType string, edges []Edge) error
func (s *Store) DeleteEdgesInvolving(slug string) error
func (s *Store) DeleteEdge(fromSlug, toSlug string) error
func (s *Store) GetOutboundEdges(slug string) ([]Edge, error)
func (s *Store) GetInboundEdges(slug string) ([]Edge, error)
func (s *Store) EdgeCount() (int, error)
func (s *Store) FindOrphans() ([]string, error)
func (s *Store) FindBrokenEdges(exists func(slug string) bool) ([]Edge, error)  // D13
```

#### Cross-Process Write Safety (D4)

```go
func (s *Store) WithFileLock(fn func() error) error {
    tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
    if err != nil { return err }
    defer tx.Rollback()
    if err := fn(); err != nil { return err }
    return tx.Commit()
}
```

All wiki file mutations (WriteWiki, UpdateRelatedSection, update_wiki, delete_page) wrapped in `WithFileLock`. SQLite's exclusive lock serializes across poll process and MCP server process. External edits (user in editor) are caught by the hygiene cycle.

### Part 2: Auto-Linking at Enrichment (Prevention)

After writing a new wiki page, update existing related pages to link back. This prevents most hygiene issues at source.

**Added to `processInboxItem` / `processNotes`, after wiki write:**

```go
// After writing wiki page for newSlug:
// 1. Parse tags from newly written page
// 2. Find existing pages with 2+ shared tags (from index.md via ParseIndexEntry) (D8)
// 3. Exclude newSlug from candidates (D11)
// 4. Sort candidates: tag overlap DESC → file mtime DESC → slug ASC (D6)
// 5. For top 3 candidates, add [[newSlug]] to their ## Related section (D3)
// 6. Sync edges for each modified page
```

**Rules:**

- Only add links to `## Related` section, never touch other sections.
- Skip if link already exists in the page.
- Max 3 back-links per enrichment (don't spam old pages) **(D6)**.
- Use `vault.UpdateRelatedSection(slug, newLinks)` — new method that preserves existing links and appends.
- If `## Related` section doesn't exist, create it before the `---` footer. If no footer, append at end **(D9)**.

**Back-link format (D3):**

```markdown
## Related

- [[service-mesh-comparison]] — alternative networking approaches
- [[cilium-ebpf-networking]] — shared tags: kubernetes, networking
```

**What this prevents:**

- Static links → existing pages get back-links when new related content arrives.
- One-directional links → back-linking is built into the write path.
- Orphans → new pages immediately get linked from existing related pages.

### Part 3: Auto-Fix on Poll Cycle

Lightweight, deterministic fixes that run periodically. No LLM needed.

```go
func runHygiene(db *store.Store, v *vault.Vault) {
    // 1. Broken edges: remove edges where target file doesn't exist (D13)
    broken := db.FindBrokenEdges(func(slug string) bool {
        return v.FileExists("wiki/" + slug + ".md")
    })
    for _, edge := range broken {
        db.DeleteEdge(edge.FromSlug, edge.ToSlug)
        slog.Info("hygiene: removed broken edge", "from", edge.FromSlug, "to", edge.ToSlug)
    }

    // 2. Bidirectional index sync (D10) — 3 directions in single pass
    syncIndex(v)

    // 3. Raw↔wiki consistency (D14, D15)
    cleanRawWikiOrphans(v)

    // 4. Log orphan pages (warning only)
    orphans := db.FindOrphans()
    for _, slug := range orphans {
        slog.Warn("hygiene: orphan page", "slug", slug)
    }
}
```

**Bidirectional index sync (D10):**

```go
func syncIndex(v *vault.Vault) {
    // Build set of what's in index now
    indexEntries := parseAllIndexEntries(v)  // map[slug]IndexEntry

    // Scan wiki dir — source of truth
    wikiFiles := scanWikiDir(v)
    diskSlugs := map[string]bool{}

    for _, file := range wikiFiles {
        slug := deriveSlug(file)
        diskSlugs[slug] = true
        title, tags := parseFrontmatter(file)

        existing, inIndex := indexEntries[slug]
        if !inIndex {
            // Direction 2: file exists, not in index → add
            v.UpdateIndex(slug, title, tags)
            slog.Info("hygiene: added missing index entry", "slug", slug)
        } else if tagsChanged(existing.Tags, tags) || existing.Title != title {
            // Direction 3: metadata drifted → update
            v.RemoveFromIndex(slug)
            v.UpdateIndex(slug, title, tags)
            slog.Info("hygiene: synced index entry", "slug", slug)
        }
    }

    // Direction 1: index entry exists, file gone → remove
    for slug := range indexEntries {
        if !diskSlugs[slug] {
            v.RemoveFromIndex(slug)
            slog.Info("hygiene: removed stale index entry", "slug", slug)
        }
    }
}
```

**Raw↔wiki consistency (D14, D15):**

```go
func cleanRawWikiOrphans(v *vault.Vault) {
    // D14: Orphaned raw files — raw exists, no matching wiki → delete raw
    rawFiles := scanRawDir(v)
    for _, file := range rawFiles {
        slug := deriveSlug(file)
        if !v.FileExists("wiki/" + slug + ".md") {
            os.Remove(file)
            slog.Info("hygiene: deleted orphaned raw file", "slug", slug)
        }
    }

    // D15: Wiki with missing raw → rewrite footer to use original URL
    wikiFiles := scanWikiDir(v)
    for _, file := range wikiFiles {
        slug := deriveSlug(file)
        rawPath := "raw/" + slug + ".md"
        if !v.FileExists(rawPath) && hasRawFooterLink(file, rawPath) {
            url := parseFrontmatterURL(file)
            if url != "" {
                rewriteFooterLink(file, rawPath, url)
                slog.Info("hygiene: fixed broken raw link in footer", "slug", slug)
            }
        }
    }
}
```

**Trigger:** Every Nth poll cycle, configurable via `HYGIENE_INTERVAL` env var (default: every 10th cycle).

**What it fixes automatically:**

- Broken edges → removed from edges table (page content unchanged, dead `[[link]]` stays as text).
- Stale index entries → removed from index.md.
- Missing index entries → added to index.md from wiki file frontmatter.
- Tag/title drift → index.md updated to match frontmatter (source of truth).
- Orphaned raw files → deleted when no matching wiki page exists **(D14)**.
- Broken raw footer links → rewritten to use original URL from frontmatter **(D15)**.

**What it does NOT fix (logs only):**

- Orphan pages → logged as warning. May be re-connected by auto-linking when related content arrives. Destructive action (deletion) requires human or agent.

### Part 4: MCP Write Tools

Required so external clients (Hermes, Claude Code, OpenWebUI) can fix issues that auto-fix doesn't handle, and perform general vault mutations.

All MCP write operations wrapped in `store.WithFileLock()` for cross-process safety **(D4)**.

**`update_wiki(slug, section, content)`**

- Parses existing page, finds `## {section}` heading, replaces content up to next `##` heading.
- Preserves frontmatter and all other sections.
- Updates edge table if `## Related` section changed.
- Updates `last_updated` in frontmatter.

**`delete_page(slug)` (D7, D12)**

- Collects inbound edges via `store.GetInboundEdges(slug)` **before any deletion** **(D12)**.
- Deletes `wiki/{slug}.md` from disk.
- Removes from index.md via `vault.RemoveFromIndex()`.
- Removes all edges via `store.DeleteEdgesInvolving()`.
- Removes from DB:
  - If `strings.HasPrefix(slug, "notes/")` → `store.DeleteNote()`
  - Otherwise → `store.DeleteBookmarkByWikiPath("wiki/" + slug + ".md")`
- Returns list of pages that still link to the deleted page (from pre-collected inbound edges).

---

## Edge Cases

### ParseWikiLinks hardening

`ParseWikiLinks()` requires several fixes before edges table can be reliable:

1. **Code blocks** — Skip content inside fenced code blocks (`` ``` `` and `~~~`) and inline code backticks.
2. **Frontmatter** — Only parse content after closing `---`. `related: [[other-page]]` in YAML should not create edges.
3. **Display text (D2)** — Split `[[slug|Display Name]]` on `|`, take first part as target.
4. **Raw file refs (D1)** — Filter out targets ending in `.md` (raw file back-references, not semantic links).
5. **Self-edges** — Filter `from_slug == to_slug` in `UpsertEdgesFrom`, plus `WHERE from_slug != to_slug` as SQL safety net.
6. **Duplicates** — Same page can link to same target multiple times. Use `INSERT OR IGNORE` to handle gracefully.

### UpdateRelatedSection edge cases (D9)

Three cases for where to insert/append:

1. `## Related` exists → append new links to it.
2. No `## Related`, but LucidVault footer exists → insert `## Related` section before footer.
3. No `## Related`, no footer → append `## Related` at end of file.

Footer detection: match `---` line followed by `*Source:` on the next line. Do NOT match any bare `---` — that could be a horizontal rule mid-content. Pages without the LucidVault footer pattern (manually created pages) fall through to case 3.

`syncIndex` must handle pages without frontmatter title — fall back to `notes.TitleFromFilename()` or slug-derived title.

### `update_wiki` section parsing

Must handle: section at end of file (no next heading), empty sections, nested headings (`###` inside `##`), code blocks containing `##`. Parse by `## ` at line start, only outside fenced code blocks.

### Slug renames / page moves

`UpsertEdgesFrom` handles source side. Stale `to_slug` references caught by `FindBrokenEdges()` auto-fix. Full rebuild available via `--rebuild-edges` flag.

### Edges table out of sync with disk

User manually edits/deletes files. Hygiene cycle catches stale references via `FindBrokenEdges()`. Full rebuild available via `--rebuild-edges` flag. First startup auto-rebuilds when edges table is empty **(D5)**.

### Concurrent writes (D4)

Poll process and MCP server are separate OS processes sharing the same vault directory and SQLite database. All file mutations wrapped in `store.WithFileLock()` which uses SQLite's `BEGIN EXCLUSIVE` as a cross-process mutex. Lock is database-wide (not per-file), acceptable at current write frequency.

External edits (user in editor) are not protected by the lock. Hygiene cycle detects and syncs drift on next run.

### LLM-generated broken links

Enrichment asks the LLM to link to existing wiki pages, but it can hallucinate slugs. These become broken edges immediately. `FindBrokenEdges()` catches them in the next hygiene cycle.

### Auto-linking creates too many back-links

Capped at 3 per enrichment **(D6)**. Candidates sorted by tag overlap DESC → file mtime DESC → slug ASC. Skip if link already exists. Only triggers on tag overlap (2+ shared tags), not arbitrary similarity.

### Auto-linking self-match (D11)

New page always shares all tags with itself. After adding to index (step before auto-linking), `ParseIndexEntry` scan would return it as a candidate. Must explicitly filter `newSlug` from candidate list.

### Wiki directory scan must be recursive

Note wiki copies live at `wiki/notes/foo.md`. All scans (`scanWikiDir`, edge rebuild, `syncIndex`) must use recursive glob `wiki/**/*.md` or `filepath.WalkDir`, not `wiki/*.md`.

### Empty vault / fresh install

All checks return empty results. Edges table is empty (triggers auto-rebuild on first run). Auto-fix is a no-op. Auto-linking has no existing pages to update.

---

## Implementation Order

### Phase 1: Edges Table + ParseWikiLinks Hardening

1. Harden `ParseWikiLinks()`: code block skipping, frontmatter exclusion, pipe syntax splitting, `*.md` filtering, self-edge filtering.
2. Add `edges` table to SQLite migration in `store.Store`.
3. Implement `RebuildEdges`, `UpsertEdgesFrom`, `DeleteEdgesInvolving`, `DeleteEdge` (with `INSERT OR IGNORE`).
4. Implement `GetOutboundEdges`, `GetInboundEdges`, `EdgeCount`, `FindOrphans`, `FindBrokenEdges`.
5. Add `WithFileLock()` for cross-process write safety.
6. Add conditional rebuild on startup (empty table) + `--rebuild-edges` flag.
7. Add incremental update in `processInboxItem` and `processNotes` after wiki write.

### Phase 2: Auto-Linking at Enrichment

8. Implement `vault.UpdateRelatedSection(slug, newLinks)` — append links to `## Related`, create section if absent.
9. Add back-linking step after wiki write: find pages with 2+ shared tags from index.md, sort by overlap/recency, update top 3's `## Related` with `[[slug]] — shared tags: x, y` format.
10. Sync edges for each modified page.

### Phase 3: Auto-Fix (Hygiene Cycle)

11. Implement `syncIndex(v)` — bidirectional 3-direction index sync (stale removal, missing addition, tag/title sync).
12. Implement `cleanRawWikiOrphans(v)` — delete orphaned raw files (D14), fix broken raw footer links (D15).
13. Add `runHygiene()` to poll loop with configurable `HYGIENE_INTERVAL`.

### Phase 4: MCP Write Tools

14. Add `DeleteBookmarkByWikiPath(wikiPath)` to store.
15. Implement `update_wiki(slug, section, content)` with section parsing.
16. Implement `delete_page(slug)` with full cleanup (disk + index + edges + bookmarks/notes table).
17. Register new tools in MCP server.

---

## Research Sources

- **claude-obsidian** (github.com/AgriciDaniel/claude-obsidian) — wiki-lint 10-point health check, DragonScale semantic tiling, boundary-first gap detection.
- **wiki-lint skill** (ar9av/obsidian-wiki) — 13 lint checks, consolidate mode with 7 auto-fix actions, provenance drift detection.
