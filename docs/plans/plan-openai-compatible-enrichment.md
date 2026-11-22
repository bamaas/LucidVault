# Plan: OpenAI-compatible Enrichment Backend

See `docs/adr/031-openai-compatible-enrichment-backend.md` (amends ADR-003 and ADR-030). Glossary: **Enrichment Backend** in `CONTEXT.md`.

## Goal

Enrichment and Tag Suggestion call any OpenAI-compatible chat endpoint instead of Ollama's native API, so users can choose Ollama Cloud, a self-hosted Ollama, or any other compatible endpoint. A backend that is unusable no longer fills the wiki with minimal pages.

## Scope

In:

- Replace Ollama's native `/api/chat` call with the OpenAI-compatible chat completions call.
- New env vars `ENRICH_BASE_URL`, `ENRICH_API_KEY`, `ENRICH_MODEL`, with deprecated `OLLAMA_*` fallbacks.
- Retry-next-cycle behaviour for an unusable backend.
- Docs, compose, `.env.example`, mise task, guides.

Out:

- Streaming, tool calls, `temperature`/`max_tokens` tuning.
- Re-enriching existing minimal pages (users run `--force-re-enrich` themselves; mention it in the release notes).
- Naming or documenting any Claude wrapper (ADR-031).
- Replacing Hermes in agent chat (issue #112).

## Requirements

### 1. Protocol (`internal/enrich`)

1. The client sends `POST {ENRICH_BASE_URL}/chat/completions` with JSON `{"model": <ENRICH_MODEL>, "messages": [{"role": "user", "content": <prompt>}], "stream": false}` and `Content-Type: application/json`.
2. It reads the answer from `choices[0].message.content`. An empty `choices` array or an empty content is a failed call (treated like invalid output: retry, then minimal page).
3. `Authorization: Bearer <ENRICH_API_KEY>` is sent only when the key is non-empty.
4. Prompt building, `cleanResponse`, `validateResponse`, `buildMinimalPage`, `parseTags`, the proactive delay, and the existing 10 MB response limit and 300 s timeout stay unchanged.
5. No `/api/chat` remains in the code. Rename Ollama-specific identifiers (file `ollama.go`, `config.ollama*` fields, error text `ollama API returned`) to neutral names; keep the `enrich.Client` type and its `Enrich` / `SuggestTags` methods.

### 2. Failure handling

| Condition | Behaviour |
|---|---|
| Connection error (refused, DNS, TLS), HTTP 401, 402, 403, 404, 410 | **Backend unavailable**: no retries, no minimal page. `Enrich` and `SuggestTags` return an error wrapping an exported sentinel `enrich.ErrBackendUnavailable`; the message includes the status code and the response body (truncated), never the API key. |
| HTTP 429 | Existing exponential backoff, unchanged. |
| HTTP 5xx, timeout, invalid or empty model output | Existing retries, then minimal page (`Enrich`) or `["untagged"]` (`SuggestTags`), unchanged. |
| Context cancelled | Return `ctx.Err()` immediately, unchanged. |

Pipeline effects (`cmd/main.go`):

- `processInbox`: on `ErrBackendUnavailable` the inbox file stays (already the case for any error), and the loop **stops for this cycle** instead of trying the remaining items against the same dead backend. One `ERROR` log line names the endpoint (base URL, no key) and the status.
- `processNotes`: on `ErrBackendUnavailable` the note is skipped without updating its content hash (already the case for any error), and the loop stops for this cycle.
- `reEnrichBookmark` / `reEnrichAll`: on `ErrBackendUnavailable` the existing wiki page is left untouched and the run stops with an error.

### 3. Configuration (`loadConfig`)

| New | Required | Legacy fallback | Notes |
|---|---|---|---|
| `ENRICH_BASE_URL` | Yes | `OLLAMA_BASE_URL` + `/v1` | Trim whitespace and trailing `/`. Must be an OpenAI-compatible API base URL. Error when unset: `ENRICH_BASE_URL is required: the base URL of an OpenAI-compatible API (Ollama Cloud: https://ollama.com/v1, self-hosted Ollama: http://<host>:11434/v1)`. |
| `ENRICH_MODEL` | Yes | `OLLAMA_MODEL` | Trim whitespace. Error: `ENRICH_MODEL is required`. Same no-default rule as ADR-030. |
| `ENRICH_API_KEY` | No | `OLLAMA_API_KEY` | Empty means no `Authorization` header. |

- When a new variable is set (non-blank), it wins and the legacy one is ignored.
- When only the legacy variable is set, use it and log one `WARN` at startup: `OLLAMA_MODEL is deprecated, use ENRICH_MODEL`.
- Legacy `OLLAMA_BASE_URL` is an Ollama root, so append `/v1` after trimming the trailing `/` (`http://ollama:11434/` becomes `http://ollama:11434/v1`). Don't append if it already ends in `/v1`.
- Whitespace-only counts as unset for all six variables.
- An install that only has `OLLAMA_API_KEY` (no base URL ever set) fails at startup with the `ENRICH_BASE_URL is required …` message. This is the intended break (ADR-031).
- Log the effective base URL and model at startup, never the key.

### 4. Docs and deployment files

- Every place that documents `ENRICH_BASE_URL` (README table, `.env.example` comment, `CLAUDE.md` env list) states explicitly: **it must be the base URL of an OpenAI-compatible chat completions API**, i.e. the URL that `/chat/completions` is appended to (usually ending in `/v1`). Ollama's native root (`http://host:11434` without `/v1`) does not work.
- `README.md` config table: replace the `OLLAMA_*` rows with `ENRICH_BASE_URL`, `ENRICH_API_KEY`, `ENRICH_MODEL`. Add one line on the deprecated fallbacks. Quickstart `docker run` examples use the new names. Examples: only Ollama Cloud (`https://ollama.com/v1`), self-hosted Ollama (`http://<host>:11434/v1`), and "any OpenAI-compatible endpoint". No Claude or wrapper names.
- `.env.example`, `docker-compose.yml` (`ENRICH_BASE_URL` and `ENRICH_MODEL` with `:?` required, `ENRICH_API_KEY` optional), `.mise/tasks/run/container`, `docs/guides/simple-setup.md`, `docs/guides/agent-chat/docker-compose.agent.yml` comments: new names.
- `CLAUDE.md`: Pipeline, Key Interfaces (`enrich.Client` no longer "Ollama Cloud"), Common Gotchas ("Ollama Cloud retries" becomes backend-neutral), Environment Variables.
- Commit message for the feature: `feat!:` with a `BREAKING CHANGE:` footer explaining `ENRICH_BASE_URL`.

## Edge cases

- `ENRICH_BASE_URL=https://ollama.com/v1/` with a trailing slash: trimmed, request goes to `https://ollama.com/v1/chat/completions`.
- Both `ENRICH_MODEL` and `OLLAMA_MODEL` set to different values: `ENRICH_MODEL` used, no warning about the legacy one being ignored needed beyond the deprecation warning.
- Self-hosted Ollama with no key: no `Authorization` header sent; works.
- Backend returns 404 because the model name is wrong: backend unavailable, inbox untouched, clear log.
- Backend returns 402 (no credits) for days: inbox items accumulate and are processed once credits are added; no stub pages.
- Model returns 200 with reasoning text and an empty `content`: failed call, retry, then minimal page.
- Inbox has 30 items and the backend is down: one attempt, one `ERROR`, loop stops; the other 29 wait.

## Tests (spec for the test-writer)

- Request shape: path `/chat/completions`, method, JSON body fields, `Content-Type`; `Authorization` present only when a key is set.
- Response parsing: valid `choices[0].message.content`; empty `choices`; empty content.
- Each status in 401, 402, 403, 404, 410 and a connection error: `errors.Is(err, enrich.ErrBackendUnavailable)`, exactly one request made, no minimal page, for both `Enrich` and `SuggestTags`.
- 429 then 200: backoff and success (existing behaviour kept).
- 500 on every attempt: minimal page or `["untagged"]` (existing behaviour kept).
- `loadConfig`: each row of the config table, new-wins-over-legacy, legacy `OLLAMA_BASE_URL` gets `/v1` (with and without trailing slash, and not doubled when it already ends in `/v1`), blank values treated as unset, required errors name the variable.
- Pipeline: `processInbox` stops after the first `ErrBackendUnavailable` and leaves all inbox files; `processNotes` leaves the note's hash unchanged.
- Existing tests that set `OLLAMA_*` keep passing through the fallback, or are moved to `ENRICH_*`. Don't weaken any of them.

## Acceptance criteria

- [ ] `grep -rn '/api/chat' internal/ cmd/` returns nothing.
- [ ] `grep -rniE 'claude|anthropic' internal/enrich README.md .env.example` returns nothing.
- [ ] Self-hosted Ollama (`ollama/ollama` with a small model, `ENRICH_BASE_URL=http://localhost:11434/v1`, no key) enriches one inbox URL into a full wiki page.
- [ ] With `ENRICH_BASE_URL` pointing at a server that returns 402, an inbox item stays in `inbox/` and no wiki page is written.
- [ ] An `.env` with only `OLLAMA_API_KEY`, `OLLAMA_MODEL` and `OLLAMA_BASE_URL` still starts and logs deprecation warnings.
- [ ] `mise run lint` and `mise run test` pass.
- [ ] README, CLAUDE.md, `.env.example`, compose files, mise task and guides use the new names.
