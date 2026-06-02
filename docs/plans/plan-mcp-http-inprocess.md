# Plan: Serve MCP over HTTP alongside the pipeline (single process)

> Supersedes the original "split MCP and pipeline into two containers" idea.
> See the discussion that produced this plan: two processes sharing one SQLite
> file is unsafe (esp. over networked k8s volumes), so MCP runs as a goroutine
> in the pipeline process, sharing one `*store.Store`.

## Problem

The MCP server only runs as the `lucidvault mcp` subcommand — a separate,
short-lived process (stdio) that nobody starts in the deployed stack
(`docker-compose.yml` has no MCP service). The end goal is a Kubernetes
deployment with **OpenWebUI as the in-cluster MCP client**, which needs the MCP
server reachable as an always-on HTTP endpoint.

Splitting MCP into its own container/pod was rejected: both the pipeline and MCP
write the same SQLite DB (`/vault/.lucidvault.db`). Two processes would need the
DB on a shared RWX volume, and SQLite's `BEGIN EXCLUSIVE` locking is unreliable
over networked filesystems (NFS/CephFS) — the usual k8s RWX backends. One
process touching one RWO volume sidesteps this entirely.

## Solution

Run the MCP HTTP server as a **goroutine inside the pipeline process**, sharing
the already-open `*store.Store` and `*vault.Vault`. One binary, one container,
one DB connection pool. Scales unchanged from `docker run` → Compose → a single
k8s Deployment whose ClusterIP Service exposes MCP to OpenWebUI.

```text
┌──────────── lucidvault process (1 container / 1 pod) ────────────┐
│  main()                                                          │
│   ├─ vault.New + store.New        ← single DB pool, single PVC   │
│   ├─ go serveMCPHTTP(ctx, v, db)  ← MCP Streamable HTTP server   │
│   └─ poll loop (ticker)           ← existing pipeline            │
└──────────────────────────────────────────────────────────────────┘
        ▲ Streamable HTTP (ClusterIP Service in k8s)
        │
   OpenWebUI pod (in-cluster client)
```

Security is handled at the layer that fits each environment, not in business
logic:

- **Local Docker**: publish `127.0.0.1:8080` only + a configurable Host-header
  guard (DNS-rebinding defense).
- **Kubernetes**: ClusterIP Service (never Ingress) + a NetworkPolicy allowing
  ingress only from the OpenWebUI pod. The Host guard is then optional and is
  configured to the Service name (or disabled).

## Goals

1. MCP HTTP server runs in-process alongside the pipeline, sharing the store.
2. Opt-in via `MCP_HTTP_ADDR` (empty → pipeline-only, current behaviour).
3. Configurable Host-header allowlist (`MCP_ALLOWED_HOST`) to block DNS
   rebinding locally and to support the in-cluster Service name.
4. Graceful shutdown: the HTTP server stops cleanly on SIGINT/SIGTERM via the
   existing `ctx` cancel path.
5. The standalone `lucidvault mcp` subcommand keeps working (stdio + `-http`).
6. Docs + Compose updated; k8s guidance documented.

## Non-goals

- No second container/pod. No move off SQLite.
- No auth/TLS on the MCP endpoint (network-layer controls cover the threat
  model; auth is a separate future concern if MCP is ever exposed via Ingress).
- No k8s manifests in this repo (documented guidance only, for now).

## Scope / Changes

### 1. `internal/mcpserver` — make the server embeddable and guardable

- Extract server construction from transport:
  - `func NewServer(v *vault.Vault, db *store.Store) *server.MCPServer` — builds
    and registers tools (the current body of `registerTools`).
- Add an in-process HTTP entrypoint with graceful shutdown:
  - `func ServeHTTP(ctx context.Context, s *server.MCPServer, addr string, allowedHosts []string) error`
    - Wraps the MCP handler in a Host-guard middleware.
    - Serves until `ctx` is cancelled, then `Shutdown`s with a timeout.
    - Returns `nil` on clean shutdown (`http.ErrServerClosed`), the real error
      on bind failure.
- Add the Host guard:
  - `func hostGuard(next http.Handler, allowed []string) http.Handler`
    - If `allowed` is empty → pass through (disabled; for k8s NetworkPolicy
      deployments).
    - Else compare the request's `Host` (port stripped) against the allowlist;
      `403` on mismatch.
- Keep `Run(vaultPath, httpAddr, dbPath string)` for the subcommand, refactored
  to call `NewServer` + (`ServeHTTP` | `ServeStdio`). It still opens its own
  store for standalone use.

### 2. `cmd/main.go` — launch the goroutine + config

- `config` gains `mcpHTTPAddr string` and `mcpAllowedHosts []string`.
- `loadConfig` reads:
  - `MCP_HTTP_ADDR` (default `""` → MCP off).
  - `MCP_ALLOWED_HOST` (comma-separated; default `localhost,127.0.0.1`; the
    literal `*` or empty-after-trim → guard disabled).
- After `store.New` and before the poll loop, when `mcpHTTPAddr != ""`:

  ```go
  mcpSrv := mcpserver.NewServer(v, db)
  go func() {
      if err := mcpserver.ServeHTTP(ctx, mcpSrv, cfg.mcpHTTPAddr, cfg.mcpAllowedHosts); err != nil {
          slog.Error("mcp http server failed", "error", err)
          cancel() // bind failure should not leave a half-running daemon
      }
  }()
  ```

- Reuses the existing SIGINT/SIGTERM → `cancel()` handler; `ServeHTTP` observes
  `ctx.Done()`.

### 3. Deployment + docs

- `docker-compose.yml`: on the `lucidvault` service add
  `MCP_HTTP_ADDR=${MCP_HTTP_ADDR:-}` and a loopback-only port publish guarded by
  a comment (`# "127.0.0.1:8080:8080"`), off by default.
- `.env.example`: add `MCP_HTTP_ADDR`, `MCP_ALLOWED_HOST`.
- `README.md`: config table + a short "Exposing MCP" section (local loopback vs
  k8s ClusterIP + NetworkPolicy).
- `CLAUDE.md`: env vars + note that MCP can run in-process.
- Optional: `docs/adr/NNN-mcp-http-in-process.md` capturing "goroutine, not a
  second container, because SQLite".

## Edge cases

- `MCP_HTTP_ADDR` empty → no goroutine, byte-identical to today's behaviour.
- Bind failure (port in use) → goroutine logs + cancels `ctx` → whole process
  exits non-silently (it's a config error the operator asked for).
- Clean shutdown → `http.ErrServerClosed` is treated as success, not an error.
- `MCP_ALLOWED_HOST=*` (or empty) → guard disabled; required for k8s where the
  request `Host` is the Service DNS name unless explicitly allowlisted.
- Host header with a port (`localhost:8080`) → port is stripped before compare.
- Shared store: MCP write tools and the pipeline already coordinate via
  `store.WithFileLock` (`BEGIN EXCLUSIVE`) — in-process this is one mutex on one
  pool, so no extra work needed.

## Acceptance criteria

- With `MCP_HTTP_ADDR=:8080`, the pipeline runs AND the MCP endpoint answers MCP
  requests, using the same DB the pipeline writes.
- With `MCP_HTTP_ADDR` unset, behaviour is unchanged.
- A request with a foreign `Host` header gets `403`; `localhost`/`127.0.0.1`
  (or a configured host) passes.
- SIGTERM shuts down both the HTTP server and the poll loop cleanly (no panic,
  no leaked goroutine, process exits 0).
- `lucidvault mcp` (stdio) and `lucidvault mcp -http :8080` still work standalone.

## Test strategy (TDD — write these first)

- `hostGuard`: foreign Host → 403; allowed host (with/without port) → 200;
  empty allowlist → pass-through. (table test, `httptest`)
- `NewServer`: returns a server with the expected tools registered (assert tool
  count/names via `RegisteredTools` parity).
- `ServeHTTP`: cancelling `ctx` returns `nil` (clean shutdown); an already-bound
  addr returns a non-nil error.
- `loadConfig`: parses `MCP_HTTP_ADDR` and `MCP_ALLOWED_HOST` (default,
  comma-split, `*`/empty → disabled).

## Out of scope / follow-ups

- k8s manifests (Deployment + ClusterIP Service + NetworkPolicy) — separate plan
  once this lands.
- MCP authentication/TLS — only needed if MCP is ever exposed beyond the cluster.
