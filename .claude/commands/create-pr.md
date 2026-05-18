Create a PR for the current changes:

1. Make sure all changes are committed
2. Merge latest changes from main into the current branch: `git fetch origin && git merge origin/main`
3. Resolve any merge conflicts if needed, then commit
4. Push the branch to origin
5. Create a PR using `gh pr create` with an appropriate title and description based on the commits
6. Set it to auto-merge with `gh pr merge --auto --squash`

Always set auto-merge. Never skip this step.
