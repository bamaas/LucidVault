Create a PR for the current changes.

## Steps

1. Make sure all changes are committed. Run /commit if needed.
2. Merge latest changes from main: `git fetch origin main && git merge origin/main`
3. Resolve any merge conflicts if needed, then run /commit:
   - Use type `chore` and description `merge main into <branch>`
4. Re-run `mise run test` and `mise run lint` — the merge may introduce new violations.
   - If violations are in your feature code: fix them locally and run /commit
   - If violations come from merged code in main: report the issue and stop — main is broken
5. Push the branch to origin.
6. Create a PR using `gh pr create` with:
   - Title following the Conventional Commits spec (this becomes the squash commit message):
     `<type>(<scope>): <short description>`
   - Description summarizing what was implemented, what was changed in review rounds, and any known caveats
7. Set to auto-merge: `gh pr merge --auto --squash --delete-branch`
   - If auto-merge fails (no branch protection configured), fall back to:
     `gh api repos/{owner}/{repo}/pulls/{number}/merge -X PUT -f merge_method=squash`

Always set auto-merge. Never skip this step.
