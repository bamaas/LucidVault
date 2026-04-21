# ADR-003: Ollama Cloud over Claude API

## Status
Accepted

## Context
Need an LLM for enrichment. Options: Claude API, OpenAI, Ollama Cloud, local Ollama.

## Decision
Use Ollama Cloud API with configurable model (default: qwen3:8b).

## Consequences
- Simpler API than Claude/OpenAI
- Model is swappable via environment variable
- Cheaper than Claude API for high-volume enrichment
- Quality may be lower than Claude for complex analysis — acceptable for bookmark summarization
