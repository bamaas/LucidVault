## v0.7.0 (2026-05-17)

### Feat

- add --re-enrich CLI flag to re-process bookmarks with updated prompt (#37)

## v0.6.2 (2026-05-17)

### Fix

- derive note title from first H1 heading instead of filename slug (#35)

## v0.6.1 (2026-05-16)

### Fix

- quote {{date}} in note template to prevent YAML parsing as mapping (#34)

## v0.6.0 (2026-05-16)

### Feat

- add dockerfile-roast, actionlint, govulncheck, yamllint, and rumdl linters (#32)

## v0.5.2 (2026-05-16)

### Refactor

- harden codebase with context propagation, security, and tests (#31)

## v0.5.1 (2026-05-16)

### Refactor

- simplify to Docker-only distribution (#28)

## v0.5.0 (2026-05-16)

### Feat

- add package:binary:all task for compressed release archives (#27)

## v0.4.2 (2026-05-16)

### Refactor

- separate workflows into CI, Bump, and Release (#26)

## v0.4.1 (2026-05-16)

### Fix

- use mise for Go builds in bump workflow (#25)

## v0.4.0 (2026-05-16)

### Feat

- reuse CI build artifacts instead of rebuilding on release (#24)

## v0.3.0 (2026-05-16)

### Feat

- attach cross-compiled binaries to GitHub releases & fix Docker image tags (#23)

## v0.2.0 (2026-05-16)

### Feat

- add release workflow and parallelize CI jobs (#22)

## v0.1.1 (2026-05-16)

### Fix

- read CIBOTBM_APP_ID from secrets instead of vars (#21)

## v0.1.0 (2026-05-16)

### Feat

- add personal notes indexing to knowledge graph (#18)
- add Commitizen for automatic versioning and changelog generation (#14)
- auto-reconcile deleted or empty vault wiki files (#13)
- add YouTube transcript support via Supadata API (#12)
- expand wiki page enrichment with richer content and guardrails
- add URL fetch as last-resort retrieval strategy
- upsert LucidVault retrieval strategy into host CLAUDE.md at startup

### Fix

- use GitHub App token to push bump commits to protected main (#20)
- update bump workflow for Commitizen v4 (#17)
- exclude commitizen from Docker image build (#16)
- exclude commitizen from Docker image build (#15)
- remove lastSyncAt filter from FetchBookmarks to fetch all bookmarks (#8)
- prevent sync state from advancing past failed bookmarks (#2)
- fetch bookmarks oldest-first and advance sync state correctly
- increase Ollama API timeouts to prevent header timeout on large prompts
- use mise-based builder in Dockerfile for consistent toolchain
- binary rename to lucidvault for mise run build:binary task

### Refactor

- use /src as Docker builder WORKDIR instead of /app
