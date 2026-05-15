# ADR-010: Reconcile Deleted Vault Files

## Status

Accepted

## Context

Once a bookmark is processed, its record in SQLite prevents re-processing on future poll cycles. If a user deletes or empties a wiki file from the vault, the bookmark is never re-generated because the DB still considers it processed.

## Decision

Before skipping a bookmark as "already processed", check whether the wiki file still exists on disk and has non-empty content. If the file is missing or empty, delete the stale DB record and re-process the bookmark from scratch.

Reconciliation is performed only on the source ID dedup path. The URL dedup path is not reconciled — once the source ID record is deleted, the URL check also passes because the row is gone.

## Consequences

- Deleting a wiki file from the vault automatically triggers re-processing on the next poll cycle
- Empty wiki files (whitespace-only) are treated the same as missing files
- **Known limitation**: If two different source IDs map to the same normalized URL and the first one's wiki file is deleted, only the original source ID's reconciliation will fire. The second source ID will still be blocked by URL dedup. This is a rare edge case.
- **Intentional deletion**: There is no way to permanently suppress a bookmark without it being re-processed. If needed in the future, an `excluded` flag could be added to the DB schema.
