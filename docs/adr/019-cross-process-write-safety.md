# 019 — Cross-Process Write Safety via SQLite Exclusive Transaction

**Status:** Accepted

**Context:** The poll process and MCP server are separate OS processes sharing the same vault directory and SQLite database. Wiki file mutations (write, update, delete) from either process must be serialized to prevent data corruption.

**Decision:** Use SQLite's `BEGIN EXCLUSIVE` transaction as a cross-process mutex. All wiki file mutations are wrapped in `store.WithFileLock()` which acquires an exclusive lock on a dedicated connection.

**Consequences:**

- No external lock files or advisory locks needed — SQLite provides the primitive
- Lock is database-wide (not per-file), acceptable at current write frequency
- External edits (user in editor) are not protected by the lock — caught and reconciled by the hygiene cycle on next run
- Dedicated `sql.Conn` ensures the exclusive lock is held for the duration of the operation
