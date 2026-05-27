Implement the current feature using TDD.

## Before starting
- Pull latest from main: `git fetch origin main && git merge origin/main`
- Read PLAN.md to understand the feature spec.
- Read `docs/adr/` for any architectural decisions relevant to this feature.
- Read the relevant skill files in `.claude/skills/golang/` and follow their guidance throughout.
- If this change introduces a new architectural or design decision, write an ADR in `docs/adr/` before writing any code. Number it sequentially and keep it short: Status, Context, Decision, Consequences.

## Step 1 — Write failing tests (subagent)

Spawn a subagent with the following instructions:

> You are a senior Go engineer writing functional tests. Re-read CLAUDE.md before
> starting. Read PLAN.md for the feature spec.
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
> contracts. Read PLAN.md for the feature spec.
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
