# ADR-009: DB-authoritative deduplication over source-side timestamp filtering

## Status
Accepted (supersedes incremental fetch in ADR-007's `source.Client` contract)

## Context
`FetchBookmarks` accepted a `lastSyncAt` timestamp and filtered bookmarks client-side using the source's `created` field. This broke when bookmarks were imported into Raindrop with historical creation dates — they were permanently invisible once the sync pointer advanced past them. A separate `batchSize` cap compounded the issue: with oldest-first pagination, already-processed bookmarks filled the batch before new ones were reached.

## Decision
Source clients return **full snapshots** — all bookmarks, no filtering. Deduplication is handled exclusively by the database via source ID and normalized URL checks in `processBookmark`. The `lastSyncAt` sync state and `batchSize` parameter are removed.

## Consequences
- `source.Client.FetchBookmarks()` takes no parameters — implementations paginate through all items
- Adding a new source only requires returning all bookmarks; no incremental-fetch logic needed
- The `sync_state` table is removed; the `bookmarks` table is the sole source of truth
- Each poll cycle re-fetches all bookmarks from the API — acceptable at personal-vault scale (~100 bookmarks, 4 API calls)
- For large collections (1000+), source clients may need pagination optimization (e.g. newest-first with early break when all items on a page are already in the DB)
