# ADR 011: Commitizen for Automatic Versioning

**Status:** Accepted

**Context:** The project uses conventional commits but has no automated versioning, changelog generation, or release workflow. Manual version management is error-prone and adds friction.

**Decision:** Adopt Commitizen (installed via Mise's pipx backend) for automatic version bumping, changelog generation, and GitHub release creation. Version is tracked in `.cz.toml`, tags use `v$version` format. A `bump.yml` workflow triggers after CI passes on `main` via `workflow_run`.

**Consequences:**
- Versions are bumped automatically based on conventional commit types (`feat:` → minor, `fix:` → patch, `BREAKING CHANGE` → major)
- CHANGELOG.md is generated and maintained automatically
- GitHub releases are created with auto-generated notes
- Commit messages are validated locally via a `commit-msg` git hook
- CI uses `GITHUB_TOKEN` (not a PAT); bump commits do not re-trigger workflows
- Starting version is `0.1.0`
