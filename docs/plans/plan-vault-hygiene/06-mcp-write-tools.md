# Sub-Plan 06: MCP Write Tools

## Goal

Add `update_wiki` and `delete_page` MCP tools so external clients can mutate vault content with full cleanup.

## Context

- **Files:** `internal/mcpserver/tools.go` (handlers), `internal/mcpserver/server.go` (tool registration), `internal/store/sqlite.go`
- Plan decisions: D4 (WithFileLock), D7 (delete by wiki_path), D12 (collect inbound edges before deletion).
- Existing MCP tools: `get_soul`, `search_index`, `read_wiki`, `grep_vault`, `read_note`, `read_raw`, `related_notes`, `add_bookmark`, `add_note`.

### update_wiki(slug, section, content)

- Parse existing wiki page, find `## {section}` heading.
- Replace content from after heading to next `##` heading (or EOF).
- Must handle: section at EOF, empty sections, nested `###` inside `##`, code blocks containing `##`.
- Preserve frontmatter and all other sections.
- Update `last_updated` in frontmatter.
- If `## Related` section changed → sync edges.
- Wrap in `WithFileLock`.

### delete_page(slug) (D7, D12)

- Collect inbound edges via `store.GetInboundEdges(slug)` **before any deletion** (D12).
- Delete `wiki/{slug}.md` from disk.
- Delete `raw/{slug}.md` if it exists.
- Remove from index.md via `vault.RemoveFromIndex(slug)`.
- Remove all edges via `store.DeleteEdgesInvolving(slug)`.
- Clean up DB record:
  - If `strings.HasPrefix(slug, "notes/")` → `store.DeleteNote(notePath)` (need to resolve note path).
  - Otherwise → `store.DeleteBookmarkByWikiPath("wiki/" + slug + ".md")` (D7).
- Return list of pages that still reference the deleted page (from pre-collected inbound edges).

## Tasks

1. **Implement `HandleUpdateWiki(v *vault.Vault, db *store.Store, slug, section, content string) error`**:
   - Read existing wiki page.
   - Parse sections (by `##` at line start, outside fenced code blocks).
   - Find target section, replace content.
   - Update `last_updated` frontmatter.
   - Write file back.
   - If section is "Related", sync edges via `UpsertEdgesFrom`.
   - Wrap in `WithFileLock`.

2. **Implement `HandleDeletePage(v *vault.Vault, db *store.Store, slug string) (*DeleteResult, error)`** (D7, D12):
   - Collect inbound edges first.
   - Delete wiki file, raw file (if exists), index entry, all edges, DB record.
   - Return `DeleteResult{Slug, DanglingRefs []string}`.

3. **Register `update_wiki` tool in MCP server** — add to `server.go` tool list with schema (slug, section, content params).

4. **Register `delete_page` tool in MCP server** — add to `server.go` tool list with schema (slug param). Response includes dangling refs.

5. **Add integration tests** — test full delete flow (wiki + raw + index + edges + DB record removed). Test update_wiki section replacement.

## Acceptance Criteria

- [ ] `update_wiki` replaces correct section content, preserves rest of page
- [ ] `update_wiki` handles section at EOF, empty sections, code blocks with `##`
- [ ] `update_wiki` updates `last_updated` frontmatter
- [ ] `update_wiki` syncs edges when Related section changes
- [ ] `delete_page` removes wiki file, raw file, index entry, edges, DB record
- [ ] `delete_page` returns list of pages with dangling references
- [ ] `delete_page` handles both bookmark and note slugs (D7)
- [ ] Inbound edges collected before deletion (D12)
- [ ] All write operations wrapped in `WithFileLock` (D4)
- [ ] Tools registered and callable via MCP protocol
- [ ] `mise run test` passes, `mise run lint` passes

## Dependencies

- **02** — Store methods (WithFileLock, edge CRUD, DeleteBookmarkByWikiPath)
- **03** — Edge rebuild (UpsertEdgesFrom pattern)
- **01** — ParseWikiLinks (for edge sync after update_wiki)
