# Plan: Require `OLLAMA_MODEL` (no default model)

See `docs/adr/030-no-default-ollama-model.md`.

## Goal

Remove the hard-coded `qwen3.5` fallback. The user must set `OLLAMA_MODEL` themselves.

## Requirements

1. `loadConfig` (`cmd/main.go`) returns `fmt.Errorf("OLLAMA_MODEL is required")` when `OLLAMA_MODEL` is unset, empty, or whitespace-only. Same style as the existing `OLLAMA_API_KEY` / `VAULT_PATH` checks.
2. A set value is trimmed of surrounding whitespace and used as-is.
3. No `qwen3.5` (or any other model name) remains as a default in code.
4. Existing tests that call `loadConfig` must set `OLLAMA_MODEL` (via `t.Setenv`) so they keep passing. Do not weaken them.
5. Docs:
   - `README.md` config table: `OLLAMA_MODEL` Required = Yes, Default = `-`, description says to pick a model from <https://ollama.com/search?c=cloud> (or `ollama list` on a self-hosted server). Update any quickstart / `docker run` example to include `-e OLLAMA_MODEL=<model>`.
   - `.env.example`: `OLLAMA_MODEL=` uncommented in the required section, with a comment pointing to the same URL.
   - `docker-compose.yml`: keep passing `OLLAMA_MODEL` through; make it required (`${OLLAMA_MODEL:?set OLLAMA_MODEL}`) if consistent with how `OLLAMA_API_KEY` is handled there, else leave as-is.
   - `CLAUDE.md` env var list: add `OLLAMA_MODEL — (required)`.
   - `docs/guides/simple-setup.md` and any other guide with a setup snippet: add `OLLAMA_MODEL`.

## Tests

- `OLLAMA_MODEL` unset → error mentioning `OLLAMA_MODEL`.
- `OLLAMA_MODEL="   "` → error.
- `OLLAMA_MODEL=" gpt-oss:120b "` → `cfg.ollamaModel == "gpt-oss:120b"`.

## Out of scope

- Validating the model name against Ollama's API.
- Changing `OLLAMA_BASE_URL` / `SUPADATA_LANG` (PR #105).

## Commit

`feat!: require OLLAMA_MODEL, drop qwen3.5 default` with a `BREAKING CHANGE:` footer.
