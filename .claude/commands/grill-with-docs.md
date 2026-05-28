Stress-test a feature idea against the codebase, sharpen terminology, create ADRs, and produce a plan.

## What to do

Interview me relentlessly about every aspect of this feature idea until we reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one by one. For each question, provide your recommended answer.

Ask questions one at a time, waiting for feedback before continuing.

If a question can be answered by exploring the codebase, explore the codebase instead of asking.

## Before starting

- Read `CONTEXT.md` to understand the existing domain language.
- Read `CLAUDE.md` for project conventions and architecture.
- Scan `docs/adr/` for existing architectural decisions relevant to the topic.

## During the session

### Challenge against the glossary

When I use a term that conflicts with `CONTEXT.md`, call it out immediately. "Your glossary defines 'inbox' as X, but you seem to mean Y — which is it?"

### Sharpen fuzzy language

When I use vague or overloaded terms, propose a precise canonical term. "You're saying 'page' — do you mean a Wiki Page or a Raw Page? Those are different things in CONTEXT.md."

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force me to be precise about boundaries between concepts.

### Cross-reference with code

When I state how something works, check whether the code agrees. If you find a contradiction, surface it: "Your code does X, but you just said Y — which is right?"

### Update CONTEXT.md inline

When a term is resolved, update `CONTEXT.md` right there. Don't batch — capture terms as they crystallize.

`CONTEXT.md` is a pure glossary. No implementation details, no specs, no scratch notes.

### Create ADRs sparingly

Only create an ADR when all three are true:

1. **Hard to reverse** — the cost of changing your mind later is meaningful
2. **Surprising without context** — a future reader will wonder "why did they do it this way?"
3. **Real trade-off** — genuine alternatives existed and you picked one for specific reasons

If any of the three is missing, skip the ADR. Use the existing format in `docs/adr/`: title, status, context paragraph, decision paragraph, consequences list. Number sequentially.

## When all branches are resolved

1. Summarize the decisions made and terms sharpened.
2. Draft a detailed plan to `docs/plans/plan-<feature>.md` containing:
   - Goal and scope
   - Requirements and edge cases discussed during grilling
   - References to any ADRs created or existing
   - Acceptance criteria
3. Present the plan for approval. Do not proceed to implementation.

## Output artifacts

- Updated `CONTEXT.md` (if terms were sharpened)
- New ADRs in `docs/adr/` (if trade-offs were resolved)
- Plan in `docs/plans/plan-<feature>.md`
