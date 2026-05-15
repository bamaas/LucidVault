# Notes Processing — Implementation Plan

Ref: ADR-012

## Tasks

### 1. Database schema
- [x] Add `notes` table to SQLite store: `path` (TEXT PK), `content_hash` (TEXT), `last_processed` (DATETIME)
- [x] Add migration in `internal/store/`
- [x] Add methods: `GetNoteHash(path)`, `UpsertNote(path, hash)`, `DeleteNote(path)`, `ListNotes()`

### 2. Notes scanner
- [x] Create `internal/notes/` package
- [x] `Scan(vaultPath) → []NoteFile` — recursive `filepath.Walk` over `notes/`, returns path + content hash (SHA-256) for each `.md` file
- [x] `ParseFrontmatter(content) → (tags []string)` — extract `tags:` from YAML frontmatter (both `tags: [a, b]` and `tags:\n  - a\n  - b` formats)
- [x] `TitleFromFilename(path) → string` — strip `.md`, use basename as title

### 3. Index management
- [x] Add `RemoveFromIndex(slug)` method to `vault.Vault` — removes a `[[slug]]` line from `index.md`
- [x] Ensure `UpdateIndex` works with `notes/` prefixed slugs (e.g. `[[notes/my-note]]`)

### 4. Process notes phase
- [x] `processNotes(ctx, db, v)` function in `cmd/main.go`
- [x] For each scanned note: compare hash against DB, skip if unchanged
- [x] New/changed: parse frontmatter tags, derive title from filename, call `UpdateIndex`, upsert DB record
- [x] Deletions: compare DB records against scan results, remove stale entries from index and DB
- [x] Handle empty files same as deleted (consistent with `FileExists` pattern)

### 5. Integration
- [x] Refactor `runPollCycle` to call `processBookmarks` then `processNotes` independently
- [x] Ensure `processBookmarks` doesn't short-circuit the cycle on failure
- [x] No new env vars or config needed

### 6. Tests
- [x] Unit tests for `Scan`, `ParseFrontmatter`, `TitleFromFilename`
- [x] Unit tests for `RemoveFromIndex`
- [x] Integration test: full cycle with notes added, changed, and deleted
- [x] Test that bookmark failures don't block notes processing
