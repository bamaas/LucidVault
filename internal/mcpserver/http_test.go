package mcpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"lucidvault/internal/vault"
)

// TestHostGuard verifies the Host-header allowlist middleware:
//   - foreign Host → 403
//   - allowed host (with or without port) → passes through (200)
//   - empty allowlist → pass-through (guard disabled)
func TestHostGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		allowed    []string
		host       string
		wantStatus int
	}{
		{
			name:       "foreign host blocked",
			allowed:    []string{"localhost", "127.0.0.1"},
			host:       "evil.example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "allowed host passes",
			allowed:    []string{"localhost", "127.0.0.1"},
			host:       "localhost",
			wantStatus: http.StatusOK,
		},
		{
			name:       "allowed host with port passes",
			allowed:    []string{"localhost", "127.0.0.1"},
			host:       "localhost:8080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "allowed ip with port passes",
			allowed:    []string{"localhost", "127.0.0.1"},
			host:       "127.0.0.1:8080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "foreign host with port blocked",
			allowed:    []string{"localhost"},
			host:       "evil.example.com:8080",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty allowlist disables guard",
			allowed:    nil,
			host:       "anything.example.com",
			wantStatus: http.StatusOK,
		},
		{
			name:       "service dns name allowlisted passes",
			allowed:    []string{"lucidvault-mcp"},
			host:       "lucidvault-mcp",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ipv6 literal with port passes",
			allowed:    []string{"::1"},
			host:       "[::1]:8080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "ipv6 literal without port passes",
			allowed:    []string{"::1"},
			host:       "[::1]",
			wantStatus: http.StatusOK,
		},
		{
			name:       "foreign ipv6 literal blocked",
			allowed:    []string{"::1"},
			host:       "[2001:db8::1]:8080",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "malformed ipv6 literal without closing bracket blocked",
			allowed:    []string{"::1"},
			host:       "[::1",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "case-insensitive host match passes",
			allowed:    []string{"localhost"},
			host:       "LOCALHOST",
			wantStatus: http.StatusOK,
		},
		{
			name:       "whitespace-only allowlist entries disable guard",
			allowed:    []string{"  ", ""},
			host:       "anything.example.com",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guarded := hostGuard(ok, tt.allowed)
			req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("host %q with allowed %v: got status %d, want %d",
					tt.host, tt.allowed, rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestStripPort verifies host/port splitting across bare hosts, host:port,
// IPv6 literals (with and without port), and the empty-string edge case.
func TestStripPort(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "localhost", want: "localhost"},
		{in: "localhost:8080", want: "localhost"},
		{in: "127.0.0.1:8080", want: "127.0.0.1"},
		{in: "[::1]", want: "::1"},
		{in: "[::1]:8080", want: "::1"},
		{in: "[2001:db8::1]:443", want: "2001:db8::1"},
		{in: "[::1", want: "[::1"}, // malformed: no closing bracket → returned as-is
	}
	for _, tt := range tests {
		if got := stripPort(tt.in); got != tt.want {
			t.Errorf("stripPort(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestNewServer verifies NewServer builds a server with the same set of tools
// that the static RegisteredTools list documents.
func TestNewServer(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)

	s := NewServer(v, db, true)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	registered := s.ListTools()
	var serverNames []string
	for name := range registered {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	var staticNames []string
	for _, tool := range RegisteredTools(true) {
		staticNames = append(staticNames, tool.Name)
	}
	sort.Strings(staticNames)

	if len(serverNames) != len(staticNames) {
		t.Fatalf("tool count mismatch: NewServer has %d, RegisteredTools(true) has %d\nserver: %v\nstatic: %v",
			len(serverNames), len(staticNames), serverNames, staticNames)
	}
	for i := range serverNames {
		if serverNames[i] != staticNames[i] {
			t.Errorf("tool name mismatch at index %d: server %q, static %q",
				i, serverNames[i], staticNames[i])
		}
	}
}

// TestNewServer_NilStore verifies NewServer's documented contract that, when the
// store is nil, the write/graph tools requiring it (update_wiki, edit_page,
// expand_graph, delete_page) are NOT registered, while all store-independent
// tools remain.
func TestNewServer_NilStore(t *testing.T) {
	v := vault.New(t.TempDir())

	s := NewServer(v, nil, true)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	registered := s.ListTools()

	// Tools gated behind a non-nil store must be absent.
	storeGated := []string{"update_wiki", "edit_page", "expand_graph", "delete_page"}
	for _, name := range storeGated {
		if _, ok := registered[name]; ok {
			t.Errorf("tool %q should not be registered when store is nil", name)
		}
	}

	// Every other tool from the static list must still be present.
	for _, tool := range RegisteredTools(true) {
		if isStoreGated(tool.Name, storeGated) {
			continue
		}
		if _, ok := registered[tool.Name]; !ok {
			t.Errorf("store-independent tool %q missing when store is nil", tool.Name)
		}
	}

	// Sanity: the only difference from the full set is exactly the gated tools.
	if got, want := len(registered), len(RegisteredTools(true))-len(storeGated); got != want {
		t.Errorf("nil-store tool count: got %d, want %d", got, want)
	}
}

func isStoreGated(name string, gated []string) bool {
	for _, g := range gated {
		if name == g {
			return true
		}
	}
	return false
}

// TestServeHTTP_CleanShutdown verifies that cancelling the context returns nil
// (a clean shutdown is not an error).
func TestServeHTTP_CleanShutdown(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)
	s := NewServer(v, db, true)

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- ServeHTTP(ctx, s, addr, []string{"localhost", "127.0.0.1"})
	}()

	// Wait until the server is accepting connections.
	waitForListen(t, addr)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ServeHTTP returned error on clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP did not return after context cancel")
	}
}

// TestServeHTTP_BindFailure verifies that an already-bound address yields a
// non-nil error.
func TestServeHTTP_BindFailure(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)
	s := NewServer(v, db, true)

	// Occupy a port so the bind fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = ServeHTTP(ctx, s, addr, nil)
	if err == nil {
		t.Fatal("expected error when binding to an already-used address, got nil")
	}
}

// TestServeHTTP_HostGuardIntegration verifies the running HTTP server applies
// the Host-guard end-to-end: a foreign Host header is rejected with 403 when an
// allowlist is configured.
func TestServeHTTP_HostGuardIntegration(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)
	s := NewServer(v, db, true)

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = ServeHTTP(ctx, s, addr, []string{"localhost"}) }()
	waitForListen(t, addr)

	// Foreign Host header → 403.
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign host: got status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// freeAddr returns a currently-free loopback address (host:port).
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// waitForListen blocks until addr accepts a TCP connection or the test times out.
func waitForListen(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not start listening", addr)
}
