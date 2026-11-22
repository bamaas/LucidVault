# 032 — Agent Chat via Open WebUI's Open Terminal Agents (replaces Hermes)

## Status

Accepted

## Context

The full-stack guide wires OpenWebUI → Hermes (an LLM agent with its own provider config) → LucidVault MCP. Hermes needs its own paid LLM backend (Ollama Cloud has no free tier any more; Hermes's Anthropic OAuth route only draws on Claude Max extra-usage credits), and in practice it was barely used. Open WebUI 0.13.1 added coding agents (Claude Code, Codex, and any ACP agent) to its model picker through an Open Terminal container, using each agent's official CLI and its own login (subscription or API key).

## Decision

The agent-chat stack is OpenWebUI (≥ 0.13.4, pinned) plus an Open Terminal container (official `-agents` image, pinned) whose agent gets the vault mounted read-only and writes only through the LucidVault MCP tools.

## Rejected

- **Keep Hermes.** Needs a second paid LLM account for a component nobody used; its Claude route does not use the subscription allowance.
- **An OpenAI-compatible proxy around `claude -p` as an OpenWebUI model.** Works without an OpenWebUI upgrade, but the proxy flattens chat history, drops tool events, starts a CLI per message, and is a small third-party fork — less robust than the officially maintained Open Terminal integration.
- **Read-write vault mount.** Bypasses LucidVault's index, edges and file locks (ADR-019).
- **MCP-only vault access.** Needs `MCP_READ_TOOLS=true`, against native-first retrieval (ADR-023).
- **Custom slim Open Terminal image.** Smaller, but we would own keeping the agent CLI compatible with Open Terminal.

## Consequences

- OpenWebUI must be upgraded from the 0.9.x line to ≥ 0.13.4, which runs database migrations; back up its data volume first and pin the version instead of `:main`.
- The guide stays agent-agnostic: Claude Code is the worked example, Codex or any ACP agent work the same way. LucidVault still never provides or names an LLM login of its own (ADR-024, ADR-031).
- Agent chats cannot be regenerated or edited (Open WebUI limitation); tool use is visible and approved in the chat.
- The Open Terminal API key grants command execution inside that container: no published port, reachable only by OpenWebUI on the compose network.
- ADR-023 and ADR-024 mention Hermes as the example agent; their decisions are unchanged and apply to the Open Terminal agent.
- Verify with: `grep -rni hermes docs/guides/ README.md` returns nothing; `docker compose config` shows no published port on the Open Terminal service and `:ro` on its vault mount.
