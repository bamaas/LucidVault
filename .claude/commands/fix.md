Fix one cluster of triaged bugs from GitHub issues to one merged PR. `/fix all` repeats until no
fixable bug remains.

You are an orchestrator. Delegate implementation, test auditing, code review, and PR publication to
subagents exactly as `/deliver` does. This command replaces `/deliver`'s plan file with the GitHub
issue: the issue body is the bug record, its `## Fix direction` comment is the spec, the regression
test named after the issue is the durable note, and `Fixes #N` in the PR body is the link.
**Nothing about a bug is written into tracked files** except code, tests, and docs the Fix
direction's `Docs:` line names. No plan file, no ADR.

Triage belongs to `/bug`. Everything after Step 1 is the generic `/implement` → `/test` → `/review`
→ `/pr` chain and must not be re-done by hand.

## Input

`$ARGUMENTS` — optional.

- none: pick the next cluster (Step 0).
- `N` or `#N`: that issue plus its cluster siblings.
- `N M …`: exactly those issues, one PR. Refuse if they span two cluster labels.
- `all`: loop Step 0 → Step 6 until Step 0 finds nothing, then report.

## Labels (GitHub is the only state)

| Label | Meaning |
|---|---|
| `bug` + `P0-critical` … `P3-low` | fixable backlog, ordered by priority then issue number |
| `cluster/<name>` | issues sharing a root cause or code path. One cluster = one PR |
| `needs-decision` | the fix needs a new or amended ADR; the user picks first. Never fix one |
| `in-progress` | a `/fix` branch is open. Skip; report it |

## Step 0 — Pick

```bash
gh issue list --label bug --state open --limit 100 \
  --json number,title,labels,body
```

Exclude `needs-decision` and `in-progress`. Order by priority label (P0 → P3, no priority label
last), then number. Take the first. If it carries a `cluster/*` label, the work item is every open
`bug` issue with that label.

- Any cluster member labelled `needs-decision` → the whole cluster is blocked. Present that issue's
  options to the user, then take the next cluster (in `all` mode) or stop (otherwise).
- Nothing left → stop and say so. In `all` mode this is the normal exit.

Report the pick: issue numbers, titles, priority, cluster, files cited.

## Step 1 — Spec

Every issue in the work item must have a comment starting with `## Fix direction`
(`gh issue view N --comments`). For those that lack it, run `/bug N M …` first. Triage can add
cluster siblings or `needs-decision`: re-read the labels and re-apply Step 0's rules to the work
item before continuing.

Then:

1. Start from a clean tree on `main`: `git fetch origin main` and
   `git switch -c fix/<cluster-name> origin/main` (or `fix/issue-N` for a lone issue).
2. Assemble the spec file **outside the tree** at
   `$(git rev-parse --git-path claude-fix)/<branch>.md`: for each issue, its title, body, every
   comment, and the `## Fix direction` comment, in issue order. Prepend:

   ```text
   # Fix: <branch>
   Delivery mode: Go/TDD. Gate: mise run lint && mise run test.
   Scope: only the functions named under "Fix" and their tests. Each issue gets a red regression
   test named exactly as its "Regression test" line before any production change. Do not widen.
   ADRs in docs/adr/ are binding. If a fix needs a new or amended ADR, stop.
   Docs: change only what each "Docs:" line names. Otherwise no README, CLAUDE.md, or ADR edits.
   ```

3. `gh issue edit N --add-label in-progress` for each issue.

## Step 2 — Implement

Spawn a phase orchestrator (`model: "sonnet"`):

> Run `/implement` with plan path: [spec file]. Use the current branch. The spec is a set of bug
> issues: `test-writer` writes one failing test per issue, named as the spec says, built from the
> issue's repro; confirm each is red for the reason the issue states before implementing. Commit
> the red tests as `test(<scope>): add regression tests for #N[, #M]`. `implementer` changes only
> what the spec's "Fix" lines name. In `/document`, touch only godoc on symbols you changed plus
> what the "Docs:" lines name; create no ADR, plan, or other markdown. Commit the fix as
> `fix(<scope>): <what is now true> (#N)`. Report files, tests, commits, and checks actually run.

If `/implement` reports that the fix needs something the spec does not settle, stop: label the
issue `needs-decision`, remove `in-progress`, post the question as a comment, and report. Leave the
branch for the user.

## Step 3 — Test quality

Spawn a phase orchestrator (`model: "sonnet"`):

> Run `/test` scoped to [spec file and the files this branch changed]. The `test-reviewer` must try
> to make each `TestIssueN_*` pass against the pre-fix code path in its head: a regression test
> that would pass without the fix is a finding. Report rounds, fixes, and production changes.

## Step 4 — Review

Spawn a phase orchestrator (`model: "sonnet"`):

> Run `/review` scoped to [spec file and `git diff origin/main...HEAD`]. For each issue, the
> `code-reviewer` tries to falsify the fix with the issue's own repro and with the neighbours the
> Fix direction names. Report rounds, fixes, and skipped findings.

Both loops keep their own five-round cap. Hitting it stops `/fix`: report the open findings.

## Step 5 — Records (minimal by design)

- `tasks/lessons.md`: one line, **only** for a recurring class (the second time the same pattern is
  fixed), never per bug.
- **No** `tasks/todo.md` entry, **no** plan file, **no** ADR, **no** per-bug markdown.

## Step 6 — One PR

Spawn a phase orchestrator (`model: "sonnet"`):

> Run `/pr`. Title `fix(<scope>): <what is now true>` — this becomes the squash commit. Body: one
> `Fixes #N` line per issue so the merge closes them all, the regression tests added, test and
> review rounds, and caveats. Push, open, enable auto-merge. Report URL and merge state.

In `all` mode wait for the merge before returning to Step 0, so the next branch starts from the
merged `main`: `gh pr checks <pr> --watch`, then `gh pr view <pr> --json state,mergedAt`. A failed
check stops the loop: report it and leave the PR open. `main` requires up-to-date branches, so a PR
left `BEHIND` (`gh pr view <pr> --json mergeStateStatus`) needs `gh pr update-branch <pr>` before
auto-merge can fire.

## Completion

Report, per PR: issues fixed with their regression tests; test and review rounds; any
`needs-decision` raised and its options; PR URL and merge result. In `all` mode add how many bugs
remain and why each remaining one was skipped.
