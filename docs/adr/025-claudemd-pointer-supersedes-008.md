# 025 — Reduce CLAUDE.md Injection to a Pointer (supersedes ADR-008)

**Status:** Accepted (supersedes ADR-008)

**Context:** ADR-008 had LucidVault inject a full retrieval-strategy section into the host `CLAUDE.md` at startup so Claude Code could learn to query the vault. Since then, the generated `AGENTS.md` (ADR-015) became the single, always-regenerated source of truth for retrieval strategy, the live tool surface, and citation rules. The injected `CLAUDE.md` section now duplicates that strategy as a *static, hand-maintained* copy that drifts on every change — and the new Web Search Strategy (ADR-024) would otherwise have to be written in two places that silently diverge.

**Decision:** Reduce the injected `CLAUDE.md` section to a minimal pointer — the vault's absolute path plus "read `AGENTS.md` and follow it" — and stop duplicating the retrieval strategy and file legend.

**Consequences:**

- Eliminates strategy drift between `CLAUDE.md` and `AGENTS.md`; the strategy lives in exactly one place.
- Supersedes ADR-008. The upsert mechanism, the `<!-- lucidvault:start/end -->` markers, idempotent replacement, and the `CLAUDE_MD_PATH` env var all remain — only the section *body* shrinks.
- Preserves the one thing `AGENTS.md` cannot self-supply: the absolute host path of the vault (`AGENTS.md` uses vault-root-relative paths) plus zero-config discovery for Claude Code.
- Mirrors the established principle that an agent's own instructions supply only the vault path + "read `AGENTS.md`," never restating its contents.
- The web-search feature (ADR-024) therefore touches only `AGENTS.md`; `CLAUDE.md` inherits the strategy by pointing at it.
- Shipped as a **separate commit** from the web-search change.
