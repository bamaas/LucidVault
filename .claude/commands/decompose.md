Decompose a plan into context-window-sized sub-plans for iterative delivery.

You are a planning agent. Your job is to break a large plan into ordered, self-contained sub-plans that can each be implemented by a single subagent in one context window.

## Input

The user provides a plan path as argument: $ARGUMENTS

If no argument given, ask the user which plan to decompose from `docs/plans/`.

## Step 1 — Read and understand the plan

- Read the plan file at the given path.
- Read any ADRs referenced in the plan (`docs/adr/`).
- Read relevant source files to understand current codebase state.
- Identify the distinct pieces of work: new packages, new functions, tests, integrations, config changes, documentation.

## Step 2 — Decompose into sub-plans

Break the plan into numbered sub-plans. Each sub-plan must be:

- **Self-contained** — implementable without reading other sub-plans. Include enough context (interfaces, types, file paths) for the implementing agent to work independently.
- **Context-sized** — scoped to fit one subagent lifecycle. Rule of thumb: one package or one integration point per sub-plan.
- **Dependency-ordered** — numbered so that plan N can assume plans 1..N-1 are already implemented and committed.
- **Testable** — has clear acceptance criteria that can be verified with tests or lint.

Each sub-plan should include:
1. **Goal** — one sentence describing what this sub-plan achieves.
2. **Context** — relevant interfaces, types, and file paths the implementing agent needs.
3. **Tasks** — concrete implementation steps.
4. **Acceptance criteria** — how to verify this sub-plan is done (tests to write, behavior to observe).
5. **Dependencies** — what prior sub-plans must be complete (by number).

## Step 3 — Write sub-plan files

Create a directory next to the plan file, named after the plan (without `.md` extension).
Write each sub-plan as a numbered markdown file:

```
docs/plans/
  plan-feature-name.md                  ← original plan (unchanged)
  plan-feature-name/                    ← new directory
    01-short-description.md
    02-short-description.md
    03-short-description.md
```

Naming: `NN-kebab-case-description.md` where NN is zero-padded.

## Step 4 — Present summary

Show the user:
- Number of sub-plans created
- One-line summary of each sub-plan with its goal
- Estimated dependency chain (which can run in parallel if any)
- Any risks or ambiguities found during decomposition

Do NOT start implementation. The user will run `/deliver` when ready.
