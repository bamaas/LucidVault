Deliver a feature from plan to merged PR.

You are an orchestrator. Delegate each phase to a subagent. Do NOT do implementation, testing, or review work directly — always spawn a subagent. This keeps your context clean for tracking progress.

## Step 1 — Understand the plan

- Read PLAN.md in the current directory and fully understand the feature to implement.
- Write the execution plan to `tasks/todo.md` with checkable items — one per pipeline phase.
- Check in with the user before starting implementation.

## Step 2 — Implement (TDD)

Spawn a subagent with these instructions:

> Run /implement. This handles the full TDD cycle: pull latest from main, write
> failing tests via a nested subagent (spec-only), implement via another subagent,
> lint, document, and commit.
>
> When done, report: what was implemented, what tests were written, and what
> commits were created.

## Step 3 — Test quality

Spawn a subagent with these instructions:

> Run /test iteratively to verify test quality, coverage, and catch false greens.
>
> When done, report: rounds completed, issues found and fixed, any intentionally
> skipped gaps and why.

## Step 4 — Review

Spawn a subagent with these instructions:

> Run /review iteratively until the code is clean.
>
> When done, report: rounds completed, issues found and fixed, any intentionally
> skipped issues and why.

## Step 5 — PR

Spawn a subagent with these instructions:

> Run /pr to push the branch and open a PR with auto-merge enabled.
>
> When done, report: PR URL, title, and whether auto-merge was set.

## Completion

- Mark all items complete in `tasks/todo.md` and add a review section summarizing the run.
- Update `tasks/lessons.md` with any patterns or mistakes encountered.
- Summarize:
  - What was implemented
  - Total test and review rounds
  - What was fixed across all rounds
  - PR link
  - Any intentionally skipped issues and why
