Implement the current feature using TDD.

## Input

The plan path is provided as argument: $ARGUMENTS

If no argument given, look for plans in `docs/plans/` and ask the user which one to implement.

## Before starting
- Pull latest from main: `git fetch origin main && git merge origin/main`
- Read the plan file at the given path to understand the feature spec.
- Read `CONTEXT.md` for domain terminology — use consistent naming in code, tests, and comments.
- Read `docs/adr/` for any architectural decisions relevant to this feature.
- Read the relevant skill files in `.claude/skills/golang/` and follow their guidance throughout.
- ADRs should already exist from the planning phase (`/grill-with-docs`). Follow existing ADRs — do not create new ones during implementation. If you discover a missing architectural decision, stop and flag it to the user rather than deciding on the fly.

## Step 1 — Write failing tests (subagent)

Spawn a `test-writer` agent (subagent_type: `test-writer`) with prompt:

> Read the plan file at: [plan path] for the feature spec. Write failing tests
> that define the expected behavior. When done, report what test files and cases
> you wrote.

Run /commit: `test(<scope>): add failing tests for <feature>`

## Step 2 — Implement (subagent)

Spawn an `implementer` agent (subagent_type: `implementer`) with prompt:

> Read the plan file at: [plan path] for the feature spec. Read the failing tests
> to understand the required behavior and contracts. Implement until `mise run test`
> passes. When done, report: what files you created/modified, and confirm tests pass.

## Step 3 — Lint

Run `mise run lint` and fix all violations before proceeding.

## Step 4 — Document

Run /document to cover godoc comments, README, CLAUDE.md, and ADRs.

## Step 5 — Commit

Run /commit for any remaining uncommitted implementation changes. Split into multiple commits by logical unit if needed.
