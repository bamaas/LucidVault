# Sub-Plan 04: Auto-Linking at Enrichment

## Goal

After writing a new wiki page, automatically add back-links from related existing pages — preventing orphans and one-directional links at the source.

## Context

- **Files:** `internal/vault/writer.go` (new `UpdateRelatedSection`), `cmd/main.go` (`processInboxItem`, `processNotes`), `internal/mcpserver/parse.go` (`ParseIndexEntry`)
- Plan decisions: D3 (back-link format), D6 (cap ordering), D8 (tag lookup via index.md), D9 (UpdateRelatedSection edge cases), D11 (exclude self from candidates).

### ParseIndexEntry (existing)

`ParseIndexEntry(line string) *IndexEntry` — already parses index.md lines. Returns `IndexEntry{Slug, Title, Tags}`.

### Back-link format (D3)

```markdown
- [[cilium-ebpf-networking]] — shared tags: kubernetes, networking
```

### Candidate ordering (D6)

1. Tag overlap count DESC (most shared tags first)
2. File mtime DESC (most recent first — via `os.Stat`)
3. Slug ASC (deterministic tiebreaker)

## Tasks

1. **Implement `vault.UpdateRelatedSection(filePath string, newLinks []string) error`** (D9):
   - If `## Related` exists → append new links (skip if link already present in section).
   - If no `## Related` but LucidVault footer exists → insert `## Related` before footer.
   - If neither → append `## Related` at end of file.
   - Footer detection: `---` line followed by `*Source:` on next line (not any bare `---`).

2. **Implement `vault.FindRelatedByTags(newSlug string, newTags []string) ([]BacklinkCandidate, error)`**:
   - Read index.md, parse each line with `ParseIndexEntry`.
   - Find pages with 2+ shared tags with `newTags`.
   - Exclude `newSlug` from candidates (D11).
   - Sort by tag overlap DESC → file mtime DESC → slug ASC (D6).
   - Return top 3 candidates.

3. **Add auto-linking step to `processInboxItem`** — after writing wiki + syncing edges:
   - Get tags from newly written page (from enriched content frontmatter).
   - Call `FindRelatedByTags(slug, tags)`.
   - For each candidate: call `UpdateRelatedSection` with `[[newSlug]] — shared tags: x, y`.
   - Sync edges for each modified candidate page.

4. **Add auto-linking step to `processNotes`** — same pattern.

5. **Wrap file mutations in `store.WithFileLock`** — `UpdateRelatedSection` modifies wiki files, needs cross-process safety (D4).

## Acceptance Criteria

- [ ] `UpdateRelatedSection` appends links to existing `## Related` section
- [ ] `UpdateRelatedSection` creates `## Related` before footer when section absent
- [ ] `UpdateRelatedSection` appends at end when no section and no footer
- [ ] Footer detection matches `---` + `*Source:` pattern only, not arbitrary `---`
- [ ] Existing links in `## Related` are not duplicated
- [ ] `FindRelatedByTags` excludes the new page itself (D11)
- [ ] Candidates sorted correctly: tag overlap DESC → mtime DESC → slug ASC
- [ ] Max 3 back-links per enrichment
- [ ] Edges synced for each modified candidate page
- [ ] `mise run test` passes, `mise run lint` passes

## Dependencies

- **01** — ParseWikiLinks hardening
- **02** — Edges table and store methods (WithFileLock, UpsertEdgesFrom)
- **03** — Edge incremental sync (pattern established)
