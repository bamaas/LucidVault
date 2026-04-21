# ADR-002: Pure Go, No CGO

## Status
Accepted

## Context
SQLite requires a C library binding. Two Go options: `mattn/go-sqlite3` (CGO) or `modernc.org/sqlite` (pure Go).

## Decision
Use `modernc.org/sqlite` with `CGO_ENABLED=0` for fully static binaries.

## Consequences
- Static binary, `FROM scratch` Docker image (~10MB)
- No C compiler needed in build pipeline
- Slightly slower SQLite operations (~10-20%) vs CGO — irrelevant for our write volume
- Driver name is `"sqlite"` not `"sqlite3"`
