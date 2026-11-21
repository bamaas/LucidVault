# 030 — No Default Ollama Model; `OLLAMA_MODEL` Is Required

## Status

Accepted

## Context

`loadConfig` fell back to `qwen3.5` when `OLLAMA_MODEL` was unset. Ollama Cloud retires models without notice: `qwen3.5` has returned HTTP 410 since 2026-09-25, so every install that relied on the default silently produced "Enrichment failed" pages. With `OLLAMA_BASE_URL` (PR #105), the endpoint can also be a self-hosted server whose installed models LucidVault cannot know.

## Decision

LucidVault ships no default model: `OLLAMA_MODEL` is required, and startup fails with a clear error when it is unset or whitespace-only.

## Rejected

- **Bump the default to a newer model.** Fixes today, breaks again at the next retirement, and the maintainer has to track Ollama's catalogue forever.
- **Query `/api/tags` at startup and pick a model.** Picks an arbitrary model the user never chose, adds a network call to startup, and Ollama Cloud's listing does not match what a given account can use.
- **Keep the default but warn on 410.** The failure still only shows up after items are processed; a startup error is louder and cheaper.

## Consequences

- Breaking change for installs that never set `OLLAMA_MODEL`. Those installs were already broken by the 410, so the startup error replaces a silent failure.
- Docs (README, `.env.example`, `docker-compose.yml`, guides) tell the user to pick a model from <https://ollama.com/search?c=cloud> (or `ollama list` for self-hosted) instead of naming one.
- Verify with: `grep -rn qwen3.5 cmd/ README.md .env.example` returns nothing; `OLLAMA_MODEL= go run ./cmd` exits with `OLLAMA_MODEL is required`.
