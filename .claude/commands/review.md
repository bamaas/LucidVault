Run an iterative code review loop on the current changes until the code is clean.

## Review Loop

Maximum 5 rounds. If the reviewer has not returned "LGTM" after 5 rounds, stop and
present the remaining issues to the user for manual decision.

Repeat the following steps until the reviewer agent returns no actionable feedback:

### Step 1 — Spawn a reviewer agent

Spawn a `code-reviewer` agent (subagent_type: `code-reviewer`) with prompt:

> Review all staged/uncommitted changes and recently modified files. Focus on the
> code changed in the current feature. When done, return your structured findings
> or "LGTM".

### Step 2 — Apply fixes (subagent)

- If the reviewer responded with "LGTM": stop the loop and report success.
- Otherwise, spawn a `review-fixer` agent (subagent_type: `review-fixer`) with prompt:

> Fix the following code review issues. This is review round N.
>
> [paste the reviewer's full issue list here]

Pass the reviewer's full issue list to this subagent.

### Step 3 — Repeat

Go back to Step 1 with the updated code.

## Completion

When the loop ends, summarize:
- How many rounds it took
- What was fixed
- Any minor issues intentionally skipped and why
