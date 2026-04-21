# ADR-006: modernc.org/sqlite over mattn/go-sqlite3

## Status
Accepted

## Context
Need a Go SQLite driver. Two main options: `mattn/go-sqlite3` (CGO, wraps C SQLite) or `modernc.org/sqlite` (pure Go transpilation of C SQLite).

## Decision
Use `modernc.org/sqlite` for pure Go builds.

## Consequences
- Enables `CGO_ENABLED=0` and `FROM scratch` images
- Driver name is `"sqlite"` (not `"sqlite3"`)
- WAL mode enabled via DSN pragma: `?_pragma=journal_mode(wal)`
- No C toolchain required in CI/CD
- Slightly larger binary due to transpiled C code (~5MB)
