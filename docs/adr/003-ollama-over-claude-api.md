# ADR-003: Ollama Cloud over Claude API

## Status

Accepted

## Context

Need an LLM for enrichment. Options: Claude API, OpenAI, Ollama Cloud, local Ollama.

## Decision

Use Ollama Cloud API with configurable model (default: qwen3:8b).

> **Superseded in part by [ADR-030](030-no-default-ollama-model.md):** `OLLAMA_MODEL`
> is now a required environment variable with no default; the "default: qwen3:8b"
> line above reflects the original decision and is no longer current behavior.

## Consequences

- Simpler API than Claude/OpenAI
- Model is swappable via environment variable
- Cheaper than Claude API for high-volume enrichment
- Quality may be lower than Claude for complex analysis — acceptable for bookmark summarization
