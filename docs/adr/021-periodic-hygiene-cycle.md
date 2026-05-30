# 021 — Periodic Hygiene Cycle

**Status:** Accepted

**Context:** Despite prevention (auto-linking), vault state drifts over time: users manually edit/delete files, LLMs hallucinate link targets, tags drift after enrichment. An event-driven approach would require filesystem watchers and add complexity.

**Decision:** Run a lightweight, deterministic hygiene cycle every Nth poll cycle (configurable via `HYGIENE_INTERVAL`, default 10). The cycle auto-fixes what's safe and logs what requires human judgment.

**Consequences:**

- Auto-fixes: broken edges removed, bidirectional index sync (stale removal + missing addition + tag/title drift), orphaned raw files deleted, broken raw footer links rewritten to original URL
- Logs only (no auto-fix): orphan wiki pages — may be re-connected by future auto-linking or require human/agent decision
- Wiki directory is source of truth — index.md is a derived artifact kept in sync
- Raw files are reproducible caches — safe to auto-delete when wiki is gone
- No filesystem watchers or event system needed — poll-cycle integration is simpler and sufficient
- External edits reconciled on next hygiene run (eventual consistency)
