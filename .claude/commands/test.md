Run an iterative TDD loop until all tests pass and coverage meets the project threshold.

Coverage target: 80% for new code. Critical paths (store, scraper, enrich) require 90%+.

## TDD Loop

Maximum 5 rounds. If the tester has not returned "LGTM" after 5 rounds, stop and
present the remaining issues to the user for manual decision.

Repeat the following steps until the tester agent returns no actionable feedback:

### Step 1 — Spawn a tester agent
Spawn a subagent with the following instructions:

> You are a senior Go engineer doing a thorough test review. Re-read CLAUDE.md
> before starting. Review the current feature spec and all existing tests for
> the code in scope.
>
> Check for:
> - Missing test cases (happy path, edge cases, error cases)
> - Incorrect or weak assertions
> - Tests that don't actually verify behavior (false greens)
> - Missing table-driven tests where applicable
> - Untested error handling paths
> - Coverage gaps on critical logic
>
> Return a structured list of missing or broken tests with:
> - File and line reference
> - Severity: critical / major / minor
> - Clear description of what is missing or wrong
> - Suggested test case or fix
>
> You may skip trivial issues (e.g., style nitpicks in test helpers) if they
> don't affect correctness or coverage.
>
> If coverage and quality are acceptable, respond with exactly: "LGTM"

### Step 2 — Process feedback
- If the tester responded with "LGTM": stop the loop and report success.
- Otherwise, process each issue by severity (critical first, then major, then minor).
- Write or fix the tests, run `mise run test` and confirm they pass.
- Run `mise run lint` and fix any violations introduced.
- Run /commit. Append the round number to the description: `test(auth): add edge cases for token expiry (round 2)`

### Step 3 — Repeat
Go back to Step 1 with the updated tests.

## Completion
When the loop ends, summarize:
- How many rounds it took
- What tests were added or fixed
- Any gaps intentionally skipped and why
- Final test count and pass status
