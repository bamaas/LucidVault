Deliver a feature from plan to merged PR.

You are an orchestrator. Delegate each phase to a subagent. Do NOT do implementation, testing, or review work directly — always spawn a subagent. This keeps your context clean for tracking progress.

## Input

The user provides a plan path as argument: $ARGUMENTS

If no argument given, look for plans in `docs/plans/` and ask the user which one to deliver.

## Subagent model strategy

- **Named agents** (`test-writer`, `implementer`, `test-reviewer`, `code-reviewer`, `test-fixer`, `review-fixer`) carry their own model in `.claude/agents/` — do NOT set a model when spawning them by `subagent_type`.
- **Phase orchestrators** (subagents that run `/implement`, `/test`, `/review`, `/pr`) have no agent file — spawn them with `model: "sonnet"`.

## Step 1 — Understand the plan and detect mode

- Read the plan file at the given path.
- Check if a sibling directory exists with the same name (minus `.md` extension) containing numbered sub-plan files (e.g. `docs/plans/plan-feature/01-*.md`, `02-*.md`).
- **Single mode**: No sub-plan directory found. Treat the plan file as a single deliverable.
- **Multi mode**: Sub-plan directory found. Each sub-plan will go through the implement → test → review cycle independently, in order.
- Write the execution plan to `tasks/todo.md` with checkable items — one per pipeline phase (or per sub-plan in multi mode).

## Step 2 — Implement (TDD)

### Single mode

Spawn a subagent (model: sonnet) with these instructions:
> Run /implement with plan path: [path to plan file]. This handles the full TDD
> cycle: pull latest from main, write failing tests via the `test-writer` agent,
> implement via the `implementer` agent, lint, document, and commit.
>
> When done, report: what was implemented, what tests were written, and what
> commits were created.

### Multi mode

For each sub-plan in order (01, 02, 03, ...):

1. **Implement** — Spawn a subagent (model: sonnet):
   > Run /implement with plan path: [path to sub-plan file]. This handles the full
   > TDD cycle. When done, report: what was implemented, what tests were written,
   > and what commits were created.

2. **Test quality** — Spawn a subagent (model: sonnet):
   > Run /test iteratively to verify test quality, coverage, and catch false greens.
   > Focus on the code changed in this sub-plan.
   >
   > When done, report: rounds completed, issues found and fixed, any intentionally
   > skipped gaps and why.

3. **Review** — Spawn a subagent (model: sonnet):
   > Run /review iteratively until the code is clean. Focus on the code changed in
   > this sub-plan.
   >
   > When done, report: rounds completed, issues found and fixed, any intentionally
   > skipped issues and why.

4. Update `tasks/lessons.md` with patterns or mistakes from this sub-plan.
5. Mark this sub-plan complete in `tasks/todo.md`.
6. Continue to next sub-plan.

After all sub-plans complete: skip to Step 5 (PR).

## Step 3 — Test quality (single mode only)

Spawn a subagent (model: sonnet) with these instructions:
> Run /test iteratively to verify test quality, coverage, and catch false greens.
>
> When done, report: rounds completed, issues found and fixed, any intentionally
> skipped gaps and why.

## Step 4 — Review (single mode only)

Spawn a subagent (model: sonnet) with these instructions:
> Run /review iteratively until the code is clean.
>
> When done, report: rounds completed, issues found and fixed, any intentionally
> skipped issues and why.

## Step 5 — PR

Spawn a subagent (model: sonnet) with these instructions:
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
