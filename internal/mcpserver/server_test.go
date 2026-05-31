package mcpserver

import (
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/vault"
)

// TestRegisteredToolsMatchesServer verifies that the static list returned by
// RegisteredTools stays in sync with the tools actually registered on an
// MCPServer instance via registerTools. A mismatch means someone added or
// removed a tool in registerTools without updating RegisteredTools (or vice
// versa).
func TestRegisteredToolsMatchesServer(t *testing.T) {
	t.Helper()

	// Build a real server with a store so all tools (including write tools
	// guarded by db != nil) are registered.
	tmpDir := t.TempDir()
	v := vault.New(tmpDir)
	db := newTestStoreForMCP(t)
	s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
	registerTools(s, v, db)

	// Collect names from the live server.
	registeredMap := s.ListTools()
	var serverNames []string
	for name := range registeredMap {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	// Collect names from the static list.
	staticTools := RegisteredTools()
	var staticNames []string
	for _, tool := range staticTools {
		staticNames = append(staticNames, tool.Name)
	}
	sort.Strings(staticNames)

	// Compare.
	if len(serverNames) != len(staticNames) {
		t.Fatalf("tool count mismatch: registerTools has %d tools, RegisteredTools() has %d\nserver: %v\nstatic: %v",
			len(serverNames), len(staticNames), serverNames, staticNames)
	}

	for i := range serverNames {
		if serverNames[i] != staticNames[i] {
			t.Errorf("tool name mismatch at index %d: server has %q, static has %q\nserver: %v\nstatic: %v",
				i, serverNames[i], staticNames[i], serverNames, staticNames)
		}
	}
}
