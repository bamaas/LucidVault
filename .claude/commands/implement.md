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

Spawn a subagent with the following instructions:

> You are a senior Go engineer writing functional tests. Re-read CLAUDE.md before
> starting. Read the plan file at: [plan path] for the feature spec.
>
> Your job is to define **what** the code should do, not **how** it does it.
> Write tests based purely on the spec — inputs, expected outputs, error cases.
>
> Rules:
> - Do NOT write any implementation code
> - Do NOT look at existing implementation details to shape your tests
> - Use table-driven tests where applicable
> - Cover happy path, edge cases, and error cases
> - Follow test conventions from CLAUDE.md and `.claude/skills/golang/`
>
> When done, report what test files and cases you wrote.

Run /commit: `test(<scope>): add failing tests for <feature>`

## Step 2 — Implement (subagent)

Spawn a subagent with the following instructions:

> You are a senior Go engineer implementing a feature. Re-read CLAUDE.md before
> starting. Read the failing tests to understand the required behavior and
> contracts. Read the plan file at: [plan path] for the feature spec.
>
> Write the implementation code until `mise run test` passes completely.
>
> Rules:
> - Write the minimum code to make tests pass — no over-engineering
> - Follow the patterns documented in CLAUDE.md and relevant Go skills
> - Accept interfaces, return structs
> - Always wrap errors: `fmt.Errorf("context: %w", err)`
> - Stop when tests pass — refactoring is handled later in /review
>
> When done, report: what files you created/modified, and confirm `mise run test`
> passes.

## Step 3 — Lint

Run `mise run lint` and fix all violations before proceeding.

## Step 4 — Document

Run /document to cover godoc comments, README, CLAUDE.md, and ADRs.

## Step 5 — Commit

Run /commit for any remaining uncommitted implementation changes. Split into multiple commits by logical unit if needed.
