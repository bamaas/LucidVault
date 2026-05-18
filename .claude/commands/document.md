Document all changes made in the current feature or fix.

## Step 1 — Godoc comments
Add or update godoc comments for:
- All newly exported types, functions, methods, and constants
- Any existing exported identifiers you modified (update to reflect new behavior)
- Do NOT add comments to private/internal identifiers unless you created them

Godoc rules:
- Start with the name of the identifier
- Use full sentences
- Document parameters and return values where non-obvious
- Document error conditions

## Step 2 — README.md
If this change introduces or modifies any of the following, update `README.md` accordingly:
- New or changed features
- New, changed, or removed environment variables (update the config table)
- Changes to the pipeline or architecture
- New dependencies or tech stack entries
- Completed to-do items

## Step 3 — CLAUDE.md
If this change introduces or modifies any of the following, update `CLAUDE.md` accordingly:
- Project structure (new packages or removed packages)
- Key interfaces (new interfaces or changed signatures)
- Required environment variables
- Build, test, or lint commands
- Common gotchas or design principles

## Step 4 — ADRs
If this change introduces a new architectural or design decision:
- Write an ADR in `docs/adr/` before or alongside the implementation
- Number sequentially (`NNN-slug.md`)
- Keep it short: Status, Context (2-3 sentences), Decision (1 sentence), Consequences (bullet list)

## Step 5 — Commit
Run /commit for any documentation changes:
- Use `docs(<scope>): <short description>` for README, CLAUDE.md, or godoc updates
- Use `chore(<scope>): <short description>` for ADRs
