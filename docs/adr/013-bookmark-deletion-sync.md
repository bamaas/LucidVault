# ADR-013: Bookmark Deletion Sync

**Status:** Superseded by [ADR-014](014-vault-inbox.md)

## Context

When a bookmark is deleted from the source (e.g. Raindrop.io), the corresponding wiki page, raw file, index entry, and DB record remained in the vault indefinitely. Since `FetchBookmarks` already returns a full snapshot every cycle (ADR-009), we can detect deletions by comparing the fetched set against DB records.

## Decision

Add a reconciliation step at the end of `processBookmarks()` that compares fetched source IDs against all DB bookmark records. For any DB record whose source ID is absent from the fetched set, remove its wiki file, raw file, index entry, and DB record.

## Consequences

- Bookmarks deleted from the source are automatically cleaned up from the vault
- Follows the same reconciliation pattern already used by `processNotes()`
- Requires `ListBookmarks()` on the store and `DeleteFile()` on the vault
- If the source API fails (returns error), no reconciliation runs — avoids false deletions
- If the source returns an empty set, reconciliation is skipped — an empty response is more likely an API glitch than the user deleting all bookmarks
- If file cleanup partially fails, the DB record is preserved so deletion is retried next cycle
