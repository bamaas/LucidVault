package main

import (
	"os"
	"reflect"
	"testing"
)

// unsetenv removes an env var and restores it (to empty) after the test. It is
// used to exercise the "variable not present" path that t.Setenv cannot, since
// t.Setenv always sets a value.
func unsetenv(t *testing.T, key string) error {
	t.Helper()
	t.Cleanup(func() { _ = os.Setenv(key, "") })
	return os.Unsetenv(key)
}

// TestLoadConfig_MCPHTTPAddr verifies MCP_HTTP_ADDR parsing: empty by default,
// honoured when set.
func TestLoadConfig_MCPHTTPAddr(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/tmp/test")

	// Default: empty → MCP off.
	t.Setenv("MCP_HTTP_ADDR", "")
	cfg, err := loadConfig(false, false)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.mcpHTTPAddr != "" {
		t.Errorf("expected empty mcpHTTPAddr by default, got %q", cfg.mcpHTTPAddr)
	}

	// Set.
	t.Setenv("MCP_HTTP_ADDR", ":8080")
	cfg, err = loadConfig(false, false)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.mcpHTTPAddr != ":8080" {
		t.Errorf("expected mcpHTTPAddr :8080, got %q", cfg.mcpHTTPAddr)
	}
}

// TestLoadConfig_MCPAllowedHost verifies MCP_ALLOWED_HOST parsing: default
// allowlist when unset, comma-split when set, and the `*`/empty-after-trim
// disable sentinel when explicitly set.
//
// Distinguishing "unset" from "explicitly empty" relies on os.LookupEnv:
//   - unset            → default localhost,127.0.0.1
//   - "" / "  " / "*"  → guard disabled (nil)
func TestLoadConfig_MCPAllowedHost(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test")
	t.Setenv("VAULT_PATH", "/tmp/test")

	tests := []struct {
		name string
		env  string
		set  bool
		want []string
	}{
		{
			name: "default when unset",
			set:  false,
			want: []string{"localhost", "127.0.0.1"},
		},
		{
			name: "comma split with trimming",
			env:  "lucidvault-mcp, localhost ",
			set:  true,
			want: []string{"lucidvault-mcp", "localhost"},
		},
		{
			name: "single host",
			env:  "lucidvault-mcp",
			set:  true,
			want: []string{"lucidvault-mcp"},
		},
		{
			name: "star disables guard",
			env:  "*",
			set:  true,
			want: nil,
		},
		{
			name: "empty disables guard",
			env:  "",
			set:  true,
			want: nil,
		},
		{
			name: "whitespace only disables guard",
			env:  "   ",
			set:  true,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("MCP_ALLOWED_HOST", tt.env)
			} else {
				// Ensure the var is truly unset for the default path.
				t.Setenv("MCP_ALLOWED_HOST", "")
				if err := unsetenv(t, "MCP_ALLOWED_HOST"); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}

			cfg, err := loadConfig(false, false)
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}

			got := cfg.mcpAllowedHosts
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MCP_ALLOWED_HOST=%q (set=%v): got %v, want %v",
					tt.env, tt.set, got, tt.want)
			}
		})
	}
}
