package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// shutdownTimeout bounds how long ServeHTTP waits for in-flight requests to
// finish after the context is cancelled.
const shutdownTimeout = 10 * time.Second

// NewServer builds an MCP server and registers all tools against the given
// vault and store. The store may be nil; in that case the write/graph tools
// that require it are not registered. The returned server is transport-agnostic
// — callers wire it to stdio (ServeStdio) or HTTP (ServeHTTP).
func NewServer(v *vault.Vault, db *store.Store) *server.MCPServer {
	s := server.NewMCPServer(
		"lucidvault",
		"1.0.0",
		server.WithToolCapabilities(false),
	)
	registerTools(s, v, db)
	return s
}

// ServeHTTP serves the MCP server over Streamable HTTP on addr until ctx is
// cancelled, then shuts down gracefully. The handler is wrapped in a Host-header
// guard built from allowedHosts (see hostGuard). It returns nil on clean
// shutdown and a non-nil error if the listener fails to bind or serving fails
// for any reason other than a clean shutdown.
func ServeHTTP(ctx context.Context, s *server.MCPServer, addr string, allowedHosts []string) error {
	handler := hostGuard(server.NewStreamableHTTPServer(s), allowedHosts)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		// Drain the serve goroutine; a clean shutdown reports ErrServerClosed
		// which is normalised to nil above.
		return <-errCh
	case err := <-errCh:
		// Serving stopped on its own — almost always a bind failure.
		return err
	}
}

// hostGuard wraps next with a Host-header allowlist to defend against DNS
// rebinding. If allowed is empty the guard is disabled and all requests pass
// through (rely on network-layer controls, e.g. a k8s NetworkPolicy). Otherwise
// the request Host (port stripped) must match one of the allowed hosts
// case-insensitively, or the request is rejected with 403.
func hostGuard(next http.Handler, allowed []string) http.Handler {
	if len(allowed) == 0 {
		return next
	}

	allowSet := make(map[string]struct{}, len(allowed))
	for _, h := range allowed {
		h = strings.TrimSpace(strings.ToLower(h))
		if h != "" {
			allowSet[h] = struct{}{}
		}
	}
	// All entries were blank → treat as disabled.
	if len(allowSet) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := stripPort(r.Host)
		if _, ok := allowSet[strings.ToLower(host)]; !ok {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stripPort removes a trailing :port from a Host header value, leaving the
// hostname. It tolerates bare hosts, IPv6 literals, and host:port forms.
func stripPort(host string) string {
	if host == "" {
		return host
	}
	// IPv6 literal: "[::1]:8080" or "[::1]".
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end >= 0 {
			return host[1:end]
		}
		return host
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}
