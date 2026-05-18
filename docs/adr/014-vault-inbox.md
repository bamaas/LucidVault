# ADR-014: Vault Inbox as Single Processing Path

**Status:** Accepted (supersedes ADR-013, impacts ADR-009)

## Context

All bookmarks flow through Raindrop.io via the `source.Client` interface — LucidVault cannot function without a Raindrop account. Users should be able to drop a URL into a folder and have it processed without any external service. Additionally, the two-way deletion sync (ADR-013) adds complexity and fragility: if the Raindrop API returns empty results, it could wipe vault content.

## Decision

Introduce an `inbox/` folder in the vault as the single entry point for all bookmark processing. Sources (e.g., Raindrop) become optional feeders that create inbox files for new URLs. The inbox processor always processes whatever is in the folder — no deduplication. Dedup only happens at the source→inbox boundary (Raindrop checks the DB before creating inbox files). Remove deletion sync entirely — once content is in the vault, it stays unless the user deletes it manually. Remove the `source.Client` registry/factory pattern; Raindrop is called directly.

## Consequences

- Users can process URLs by dropping files in `inbox/` — no external service required
- Raindrop becomes optional, auto-enabled when `RAINDROP_ACCESS_TOKEN` is set
- `SOURCE_NAME` and `SOURCE_TOKEN` env vars are removed
- Reprocessing a URL is as simple as dropping it in inbox again
- The `internal/source/` package and its registry pattern are removed
- ADR-013 (deletion sync) is fully superseded — no reconciliation logic
- ADR-009 (DB-authoritative dedup) is partially impacted: DB is still used for Raindrop→inbox dedup, but inbox processing itself has no dedup
- Raw filenames change from `yyyy-mm-dd-<slug>.md` to `<slug>.md` for clean overwrites on reprocessing
