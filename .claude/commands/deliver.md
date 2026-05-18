Deliver a feature from plan to merged PR.

## Step 1 — Understand the plan
- Read PLAN.md in the current directory and fully understand the feature to implement.
- Write the execution plan to `tasks/todo.md` with checkable items — one per pipeline phase.
- Check in with the user before starting implementation.

## Step 2 — Implement (TDD)
Run /implement, which handles the full TDD cycle:
- Pulls latest from main
- Writes failing tests via subagent (from spec only, no implementation knowledge)
- Implements the minimum code to make them pass
- Lints, documents, and commits

## Step 3 — Test quality
Run /test iteratively to verify test quality, coverage, and catch false greens.

## Step 4 — Review
Run /review iteratively until the code is clean.

## Step 5 — PR
Run /pr to push the branch and open a PR with auto-merge enabled.

## Completion
- Mark all items complete in `tasks/todo.md` and add a review section summarizing the run.
- Update `tasks/lessons.md` with any patterns or mistakes encountered.
- Summarize:
  - What was implemented
  - Total test and review rounds
  - What was fixed across all rounds
  - PR link
  - Any intentionally skipped issues and why
