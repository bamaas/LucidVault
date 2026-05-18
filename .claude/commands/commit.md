Commit all staged changes following the Conventional Commits v1.0.0 specification.

## Format

```
<type>[optional scope][optional !]: <description>

[optional body]

[optional footer(s)]
```

## Rules

### Type (required)
Must be one of:
- `feat` — introduces a new feature (correlates with MINOR in SemVer)
- `fix` — patches a bug (correlates with PATCH in SemVer)
- `refactor` — restructures code without changing behavior
- `test` — adds or updates tests
- `docs` — documentation only
- `chore` — maintenance, deps, tooling
- `ci` — CI/CD changes
- `perf` — performance improvements
- `revert` — reverts a previous commit

### Scope (optional)
A noun describing the section of the codebase in parentheses, e.g. `feat(auth):`.
For this project, scope reflects the package or domain: `auth`, `api`, `store`,
`scraper`, `vault`, `enrich`, `source`, `notes`, `cmd`.

### Description (required)
- Immediately follows the type/scope prefix
- Lowercase, imperative mood, no period at the end
- Short summary of the change

### Body (optional)
- Separated from description by one blank line
- Free-form, use to explain *what* and *why*, not *how*
- May contain multiple paragraphs

### Footers (optional)
- Separated from body (or description if no body) by one blank line
- Format: `Token: value` or `Token #value`
- Tokens use `-` instead of spaces (except `BREAKING CHANGE`)

### Breaking changes
Two ways to indicate a breaking change — use either or both:
1. Append `!` after type/scope: `feat(api)!: remove deprecated endpoint`
2. Add footer: `BREAKING CHANGE: <description>`

Breaking changes correlate with MAJOR in SemVer.

### Reverting commits
Use `revert` type with a `Refs` footer pointing to the reverted SHAs:
```
revert: let us never again speak of the noodle incident

Refs: 676104e, a215868
```

## Constraints
- Never batch unrelated changes into a single commit — split by logical unit
- Never commit broken code (`mise run test` and `mise run lint` must pass)
- Types are lowercase (except `BREAKING CHANGE` in footers, which must be uppercase)

## Examples

```
feat(scraper): add youtube transcript extraction via supadata

fix(store): handle nil bookmark in deduplication check

refactor(enrich): extract retry logic into shared helper

feat(api)!: remove v1 bookmark endpoint

BREAKING CHANGE: v1 endpoint removed, use v2 instead

test(vault): add edge cases for slug collision (round 2)

docs(readme): update env vars table for supadata key

chore(deps): bump golangci-lint to v1.58

fix(scraper): respect jina rate limits

Closes #42
Reviewed-by: Jane
```
