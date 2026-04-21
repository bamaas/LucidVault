# ADR-007: Source interface over concrete clients

## Status
Accepted

## Context
The pipeline was coupled directly to the Raindrop API client. Types like `raindrop.Bookmark` and `*raindrop.Client` were used throughout `cmd/main.go`, the store, and the vault. Swapping Raindrop for another bookmark source (e.g. Pocket, Linkding, a browser export) would require changes across the entire codebase.

## Decision
Introduce `internal/source` with a generic `Bookmark` type and a `Client` interface. Concrete clients (e.g. `internal/raindrop`) implement this interface. The rest of the codebase depends only on `source.Bookmark` and `source.Client`.

All future external integrations must follow this pattern: define a shared interface in a dedicated package, implement it in a provider-specific package.

## Consequences
- Adding a new bookmark source only requires implementing `source.Client` and calling `source.Register` in an `init()` function
- `cmd/main.go` depends on the interface, not the provider — concrete clients are blank-imported for registration
- `source.NewClient(name, token)` factory selects the provider at runtime via `SOURCE_NAME` env var
- Store and vault use generic field names (`source_id`, `source_tags`) instead of provider-specific ones
- Concrete clients include a compile-time interface check (`var _ source.Client = (*Client)(nil)`)
- Existing Raindrop databases need a column rename (`raindrop_id` → `source_id`) if migrating in-place
