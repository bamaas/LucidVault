# 022 — Serve MCP over HTTP In-Process, Not as a Second Container

**Status:** Accepted

**Context:** An always-on MCP client (e.g. OpenWebUI in Kubernetes) needs the MCP server reachable as a persistent HTTP endpoint, but the `lucidvault mcp` subcommand is a short-lived process nobody starts in the deployed stack. Splitting MCP into its own container/pod was considered and rejected: both the pipeline and MCP write the same SQLite DB, which would require a shared RWX volume, and SQLite's `BEGIN EXCLUSIVE` locking is unreliable over the networked filesystems (NFS/CephFS) that back k8s RWX volumes.

**Decision:** Run the MCP Streamable HTTP server as a goroutine inside the pipeline process (opt-in via `MCP_HTTP_ADDR`), sharing the already-open `*store.Store` and `*vault.Vault`; security is handled at the network layer per environment rather than in the application.

**Consequences:**

- One binary, one container, one DB connection pool, one RWO volume — SQLite write safety is preserved (cross-process locking concerns disappear; in-process writes coordinate via the existing `store.WithFileLock` mutex)
- `MCP_HTTP_ADDR` empty (default) → byte-identical to prior pipeline-only behaviour
- A configurable Host-header guard (`MCP_ALLOWED_HOST`, default `localhost,127.0.0.1`) defends against DNS rebinding locally; `*`/empty disables it for Kubernetes where the request Host is the Service DNS name and a NetworkPolicy enforces access
- Bind failure cancels the shared context so the operator-requested config error exits the whole process non-silently; clean shutdown (`http.ErrServerClosed`) is treated as success
- Scales unchanged from `docker run` → Compose → a single k8s Deployment whose ClusterIP Service (never Ingress) exposes MCP
- No MCP auth/TLS — out of scope while access is confined to loopback or an in-cluster NetworkPolicy; revisit only if MCP is ever exposed via Ingress
- The standalone `lucidvault mcp` subcommand (stdio and `-http`) is unchanged, now built on the same `NewServer` + `ServeHTTP` primitives
