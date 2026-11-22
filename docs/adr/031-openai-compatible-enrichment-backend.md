# 031 — OpenAI-Compatible Enrichment Backend (amends ADR-003, ADR-030)

## Status

Accepted (amends ADR-003 "Ollama over Claude API" and ADR-030 "No default Ollama model")

## Context

Enrichment and Tag Suggestion call Ollama's native `/api/chat`, so the only usable backends are Ollama Cloud and a self-hosted Ollama. Ollama Cloud dropped its free tier (the `$0` plan includes no usage; requests return HTTP 402 without credits), and users want to point LucidVault at other LLM endpoints — OpenRouter, LiteLLM, or a local wrapper around another tool — without LucidVault shipping a client per provider. Ollama itself serves the OpenAI-compatible chat API at `/v1` (verified on a self-hosted `ollama/ollama` and on `https://ollama.com/v1`), so one protocol covers both today's users and the new ones.

## Decision

LucidVault talks to its Enrichment Backend only through the OpenAI-compatible chat completions API, configured by `ENRICH_BASE_URL` (required), `ENRICH_MODEL` (required) and `ENRICH_API_KEY` (optional), and never names a specific provider beyond Ollama in code or docs.

## Rejected

- **Keep Ollama's native protocol next to OpenAI's (`ENRICH_PROTOCOL=ollama|openai`).** Two code paths and two test sets for no user-visible gain, since Ollama serves `/v1` too.
- **A command backend (`ENRICH_COMMAND`, prompt on stdin).** Needs a shell and the tool installed in LucidVault's `FROM scratch` image, so every user builds a derived image. An HTTP endpoint keeps the tool outside LucidVault.
- **Driving Open Terminal's command API.** Couples LucidVault to one product and hands it a key that can run any command on that machine.
- **A built-in Claude subscription backend.** Anthropic does not allow third-party products to offer claude.ai login. Users who want that run their own OpenAI-compatible wrapper; LucidVault does not name or document one (same stance as ADR-024 for web search).
- **Default `ENRICH_BASE_URL` to Ollama Cloud.** Keeps old installs running, but hides which endpoint is in use; with Ollama Cloud no longer free, an explicit choice is the better default, in line with ADR-030.
- **Keep writing a minimal page when the backend is unusable.** That is how a dead backend filled one vault with 43 "Enrichment failed" stubs; those items are now retried instead.

## Consequences

- Breaking for every install: `ENRICH_BASE_URL` must be set (Ollama Cloud: `https://ollama.com/v1`; self-hosted Ollama: `http://<host>:11434/v1`). Startup fails with a message that names both.
- `OLLAMA_API_KEY`, `OLLAMA_MODEL` and `OLLAMA_BASE_URL` keep working as deprecated fallbacks (a legacy `OLLAMA_BASE_URL` gets `/v1` appended) with a warning per variable; the new name wins when both are set. The fallback is removed in the next major release.
- Keyless endpoints (self-hosted Ollama, a local wrapper) need no dummy key: the `Authorization` header is only sent when `ENRICH_API_KEY` is set.
- An unusable backend (connection failure, HTTP 401, 402, 403, 404, 410) is an error, not a minimal page: the inbox item stays, the note stays unhashed, and both are retried next cycle. 429 keeps its backoff; 5xx, timeouts and invalid model output still end in a minimal page after retries.
- A connection reset mid-request counts as a connection failure (backend unavailable): it can't be told apart from a crashed backend, and a wrong "stop" costs one cycle of delay while a wrong "retry" can write a stub page.
- ADR-030's requirement moves from `OLLAMA_MODEL` to `ENRICH_MODEL`; its "no default model" rationale is unchanged.
- Verify with: `grep -rn '/api/chat' internal/ cmd/` returns nothing; `grep -rniE 'claude|anthropic' internal/enrich README.md .env.example` returns nothing.
