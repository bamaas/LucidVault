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

// TestNewServer verifies NewServer builds a server with the same set of tools
// that the static RegisteredTools list documents.
func TestNewServer(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)

	s := NewServer(v, db)
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
	for _, tool := range RegisteredTools() {
		staticNames = append(staticNames, tool.Name)
	}
	sort.Strings(staticNames)

	if len(serverNames) != len(staticNames) {
		t.Fatalf("tool count mismatch: NewServer has %d, RegisteredTools() has %d\nserver: %v\nstatic: %v",
			len(serverNames), len(staticNames), serverNames, staticNames)
	}
	for i := range serverNames {
		if serverNames[i] != staticNames[i] {
			t.Errorf("tool name mismatch at index %d: server %q, static %q",
				i, serverNames[i], staticNames[i])
		}
	}
}

// TestServeHTTP_CleanShutdown verifies that cancelling the context returns nil
// (a clean shutdown is not an error).
func TestServeHTTP_CleanShutdown(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)
	s := NewServer(v, db)

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
	s := NewServer(v, db)

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

// TestServeHTTP_ServesRequests verifies the running server answers HTTP requests
// (an unguarded foreign host is rejected when an allowlist is set).
func TestServeHTTP_HostGuardIntegration(t *testing.T) {
	v := vault.New(t.TempDir())
	db := newTestStoreForMCP(t)
	s := NewServer(v, db)

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
