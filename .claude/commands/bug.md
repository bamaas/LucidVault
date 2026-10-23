File and triage a bug as a GitHub issue, ready for `/fix`.

GitHub is the only bug record. This command writes nothing into tracked files. It turns a report
into a verified issue with a `## Fix direction` comment, priority, and cluster — or marks it
`needs-decision` when the fix needs an architectural choice. It never changes code.

## Input

`$ARGUMENTS` — required.

- free text: a new bug report (symptom, where you saw it, repro if known). Create and triage it.
- `N`, `#N`, or `N M …`: existing issues. Triage each one that lacks a `## Fix direction` comment.
  Skip, and report, any that already have one.

No argument: ask for a description and stop.

## Labels

| Label | Meaning |
|---|---|
| `bug` | every issue this command touches carries it |
| `P0-critical` | vault or DB data loss/corruption, security hole, pipeline crash loop |
| `P1-high` | user content silently overwritten or dropped, an MCP tool or pipeline stage broken |
| `P2-medium` | wrong behaviour with a workaround, or state that self-heals on a later cycle |
| `P3-low` | edge case, cosmetic, misleading docs or tool schema |
| `cluster/<name>` | issues sharing a root cause or code path; `/fix` ships one cluster as one PR |
| `needs-decision` | the fix needs a new or amended ADR; the user picks first |

Exactly one priority label per issue. Create a missing `cluster/<name>` label with
`gh label create cluster/<name> --color c5def5 --force`; `<name>` is short kebab-case naming the
shared code path (e.g. `cluster/delete-page`).

## Step 1 — Draft (new report only)

Search for duplicates first:

```bash
gh issue list --state all --limit 50 --search "<key terms>" --json number,title,state,labels
```

An open duplicate → triage that issue instead (Step 2 with its number) and add the new details as a
comment. A closed duplicate fixed by a merged PR → say so and stop; if the report shows it regressed,
continue and link the old issue in the body.

Do not create the issue yet. Step 2 verifies it first.

## Step 2 — Triage (subagent)

Spawn one read-only `general-purpose` agent per issue (in parallel for several). Prompt:

> Triage this LucidVault bug. Read-only: change no files, run no `gh` write commands.
>
> Input: [the new report text, or the output of `gh issue view N --comments`].
>
> 1. Read `CLAUDE.md`, `CONTEXT.md`, and the `docs/adr/` files touching the affected package. Find
>    the code path at current HEAD and the existing tests around it.
> 2. Verify the bug from the code. Run existing focused tests (`go test ./internal/<pkg>/...`) if
>    they help. Decide: **confirmed**, **not reproducible at HEAD** (say what fixed or prevents it),
>    or **not a bug** (behaviour matches an ADR or documented contract — cite it).
> 3. List open bug issues that share the root cause or code path:
>    `gh issue list --label bug --state open --json number,title,labels,body`.
> 4. Return exactly this, in plain English:
>
>    ```text
>    Verdict: confirmed | not-reproducible | not-a-bug
>    Title: <one line stating the wrong behaviour, for a new issue>
>    Priority: P0-critical | P1-high | P2-medium | P3-low — <one-line reason>
>    Cluster: <kebab-name> with #A, #B | none
>    Body: (new issue only) ## Context / ## Repro / ## Expected / ## Actual / ## Pointers
>
>    ## Fix direction
>    Class: mechanical | design-local | design-adr
>    Root cause: <why, with file:line>
>    Fix: <function by function, file:line, what changes>
>    Rejected: <alternatives and why not>
>    Regression test: TestIssueN_<Name> in <path>_test.go — <what it asserts, from the repro>
>    ADR: none | follows ADR-NNN | needs new/amended ADR — <question>
>    Docs: none | README.md / CLAUDE.md / AGENTS.md template — <what changes>
>    Ships with: #A, #B | alone
>    ```
>
>    `design-adr` means the fix changes an architectural decision or needs one that no ADR covers.
>    For it, replace `Fix` with options, each with its trade-off and your weight, and leave
>    `Regression test` as the behaviour to pin once decided. Use `N` literally in the test name for
>    a new issue.

## Step 3 — Publish

Per verdict:

- **confirmed, new report**: `gh issue create --title … --body … --label bug --label <priority>`.
  Replace `N` in the Fix direction with the new number, then post it with
  `gh issue comment N --body-file …`.
- **confirmed, existing issue**: add `bug` and the priority label, post the Fix direction comment.
  Keep the existing body.
- **cluster**: apply `cluster/<name>` to this issue and every sibling the agent named. A sibling
  that already has its own Fix direction stays as is; mention the shared cluster in a short comment.
- **`design-adr`**: add `needs-decision`. Present the options to the user here.
- **not-reproducible / not-a-bug, new report**: create nothing. Report the evidence.
- **not-reproducible / not-a-bug, existing issue**: post the evidence as a comment. Do not close
  it; closing is the user's call.

Write issue and comment text in normal English, never caveman.

## Completion

Report per issue: URL, verdict, priority, cluster, class, and the regression test name. For
`needs-decision`, the options with weights. Next step: `/fix N`, or `/fix` for the top of the
backlog.
