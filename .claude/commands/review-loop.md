Run an iterative code review loop on the current changes until the code is clean.

## Review Loop

Repeat the following steps until the reviewer agent returns no actionable feedback:

### Step 1 — Spawn a reviewer agent
Spawn a subagent with the following instructions:

> You are a senior Go engineer doing a thorough code review. Review all staged/uncommitted changes and recently modified files.
> 
> Check for:
> - Correctness and logic errors
> - Go idioms and conventions
> - Error handling
> - Security issues
> - Performance concerns
> - Test coverage gaps
> - Naming and readability
> 
> Return a structured list of issues with:
> - File and line reference
> - Severity: critical / major / minor
> - Clear description of the problem
> - Suggested fix
> 
> If there are no issues, respond with exactly: "LGTM"

### Step 2 — Process feedback
- If the reviewer responded with "LGTM": stop the loop and report success.
- Otherwise, process each issue by severity (critical first, then major, then minor).
- Apply fixes, then commit with message: `review: apply feedback round N`

### Step 3 — Repeat
Go back to Step 1 with the updated code.

## Completion
When the loop ends, summarize:
- How many rounds it took
- What was fixed
- Any minor issues that were intentionally skipped and why
