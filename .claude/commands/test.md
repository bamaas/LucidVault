Run an iterative TDD loop until all tests pass and coverage meets the project threshold.

Coverage target: 80% for new code. Critical paths (store, scraper, enrich) require 90%+.

## TDD Loop

Maximum 5 rounds. If the tester has not returned "LGTM" after 5 rounds, stop and
present the remaining issues to the user for manual decision.

Repeat the following steps until the tester agent returns no actionable feedback:

### Step 1 — Spawn a tester agent

Spawn a `test-reviewer` agent (subagent_type: `test-reviewer`) with prompt:

> Review the current feature spec and all existing tests for the code in scope.
> When done, return your structured findings or "LGTM".

### Step 2 — Apply fixes (subagent)

- If the tester responded with "LGTM": stop the loop and report success.
- Otherwise, spawn a `test-fixer` agent (subagent_type: `test-fixer`) with prompt:

> Fix the following test quality issues. This is test round N.
>
> [paste the tester's full issue list here]

Pass the tester's full issue list to this subagent.

### Step 3 — Repeat

Go back to Step 1 with the updated tests.

## Completion

When the loop ends, summarize:
- How many rounds it took
- What tests were added or fixed
- Any gaps intentionally skipped and why
- Final test count and pass status
