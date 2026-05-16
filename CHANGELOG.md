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
