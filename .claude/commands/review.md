Run an iterative code review loop on the current changes until the code is clean.

## Review Loop

Maximum 5 rounds. If the reviewer has not returned "LGTM" after 5 rounds, stop and
present the remaining issues to the user for manual decision.

Repeat the following steps until the reviewer agent returns no actionable feedback:

### Step 1 — Spawn a reviewer agent

Spawn a subagent with the following instructions:

> You are a senior Go engineer doing a thorough code review. Re-read CLAUDE.md
> before starting. Review all staged/uncommitted changes and recently modified files.
>
> Check for:
> - Correctness and logic errors
> - Go idioms and conventions
> - Error handling (always `fmt.Errorf("context: %w", err)`)
> - Security issues
> - Performance concerns
> - Test coverage gaps
> - Naming and readability
> - Adherence to design principles in CLAUDE.md (KISS, minimal impact, separation of concerns)
>
> Return a structured list of issues with:
> - File and line reference
> - Severity: critical / major / minor / trivial
> - Clear description of the problem
> - Suggested fix
>
> You may skip trivial issues (e.g., style nitpicks) if they don't affect
> correctness or maintainability. If all remaining issues are trivial or
> intentionally out-of-scope, respond with exactly: "LGTM"

### Step 2 — Apply fixes (subagent)

- If the reviewer responded with "LGTM": stop the loop and report success.
- Otherwise, spawn a subagent with the following instructions:

> You are a senior Go engineer applying code review fixes. Re-read CLAUDE.md
> before starting. You have been given a list of review issues to fix.
>
> Process each issue by severity (critical first, then major, then minor).
> Apply the fixes, then run `mise run test` and `mise run lint` to confirm
> nothing is broken.
>
> When done, report: what you fixed, and confirm tests and lint pass.
> Run /commit. Append the round number to the description:
> `fix(<scope>): <description> (review round N)`

Pass the reviewer's full issue list to this subagent.

### Step 3 — Repeat

Go back to Step 1 with the updated code.

## Completion

When the loop ends, summarize:
- How many rounds it took
- What was fixed
- Any minor issues intentionally skipped and why
