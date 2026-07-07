# Contributing to LucidVault

## Development workflow

Feature development is driven by Claude Code commands and documented in the repo for
future reference.

1. **Grill**: Run `/grill-with-docs` with your feature idea. This stress-tests the
   idea against the codebase, sharpens domain terminology in `CONTEXT.md`, creates
   ADRs for trade-offs, and produces a detailed plan in `docs/plans/`.
2. **Decompose (large features)**: Run `/decompose docs/plans/plan-<feature>.md` to
   break the plan into numbered, context-window-sized sub-plans in
   `docs/plans/plan-<feature>/`.
3. **Deliver**: Run `/deliver docs/plans/plan-<feature>.md`. This automates the full
   cycle:
   - **Single mode** (no sub-plans): implement (TDD) → test quality → review → PR
   - **Multi mode** (with sub-plans): loops implement → test → review per sub-plan,
     single PR at end
4. **Reference later**: Plans, sub-plans, `CONTEXT.md`, and ADRs are committed to the
   repo. Use them to understand why a feature was built, what edge cases were
   considered, and what trade-offs were made.

For small fixes or changes that don't need a plan, work directly and commit with
[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).
