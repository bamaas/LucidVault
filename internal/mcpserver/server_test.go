package mcpserver

import (
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/agentsmd"
	"lucidvault/internal/vault"
)

// readToolNames are the content-read tools that duplicate direct filesystem
// access and are therefore gated behind MCP_READ_TOOLS (off by default so
// filesystem-capable agents like Hermes read the vault natively).
var readToolNames = []string{
	"get_soul",
	"search_index",
	"read_wiki",
	"grep_vault",
	"read_note",
	"read_raw",
	"vault_overview",
}

// alwaysOnToolNames are registered regardless of the read-tool flag: graph
// traversal (not reconstructable from a single file read) and all writes.
var alwaysOnToolNames = []string{
	"related_notes",
	"add_bookmark",
	"add_note",
	"update_wiki",
	"expand_graph",
	"delete_page",
}

func toolNameSet(tools []agentsmd.ToolInfo) map[string]struct{} {
	set := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		set[tool.Name] = struct{}{}
	}
	return set
}

// TestRegisteredToolsMatchesServer verifies that the static list returned by
// RegisteredTools stays in sync with the tools actually registered on an
// MCPServer instance via registerTools, for BOTH read-tool states. A mismatch
// means someone added or removed a tool in registerTools without updating
// RegisteredTools (or vice versa).
func TestRegisteredToolsMatchesServer(t *testing.T) {
	for _, readTools := range []bool{false, true} {
		name := "readToolsOff"
		if readTools {
			name = "readToolsOn"
		}
		t.Run(name, func(t *testing.T) {
			// Build a real server with a store so all tools (including write
			// tools guarded by db != nil) are registered.
			tmpDir := t.TempDir()
			v := vault.New(tmpDir)
			db := newTestStoreForMCP(t)
			s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
			registerTools(s, v, db, readTools)

			// Collect names from the live server.
			registeredMap := s.ListTools()
			var serverNames []string
			for n := range registeredMap {
				serverNames = append(serverNames, n)
			}
			sort.Strings(serverNames)

			// Collect names from the static list.
			var staticNames []string
			for _, tool := range RegisteredTools(readTools) {
				staticNames = append(staticNames, tool.Name)
			}
			sort.Strings(staticNames)

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
		})
	}
}

// TestRegisteredTools_ReadToolGating verifies the read tools are excluded when
// the flag is off and included when on, while always-on tools appear in both.
func TestRegisteredTools_ReadToolGating(t *testing.T) {
	off := toolNameSet(RegisteredTools(false))
	for _, name := range readToolNames {
		if _, ok := off[name]; ok {
			t.Errorf("RegisteredTools(false) must NOT include gated read tool %q", name)
		}
	}
	for _, name := range alwaysOnToolNames {
		if _, ok := off[name]; !ok {
			t.Errorf("RegisteredTools(false) must include always-on tool %q", name)
		}
	}

	on := toolNameSet(RegisteredTools(true))
	for _, name := range readToolNames {
		if _, ok := on[name]; !ok {
			t.Errorf("RegisteredTools(true) must include read tool %q", name)
		}
	}
	for _, name := range alwaysOnToolNames {
		if _, ok := on[name]; !ok {
			t.Errorf("RegisteredTools(true) must include always-on tool %q", name)
		}
	}

	if len(off) >= len(on) {
		t.Errorf("enabling read tools should add tools: off=%d, on=%d", len(off), len(on))
	}
}

// TestServerOmitsReadToolsWhenDisabled verifies the live server does not expose
// any gated read tool when registered with readTools=false.
func TestServerOmitsReadToolsWhenDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	v := vault.New(tmpDir)
	db := newTestStoreForMCP(t)
	s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
	registerTools(s, v, db, false)

	live := s.ListTools()
	for _, name := range readToolNames {
		if _, ok := live[name]; ok {
			t.Errorf("server registered gated read tool %q despite readTools=false", name)
		}
	}
	for _, name := range alwaysOnToolNames {
		if _, ok := live[name]; !ok {
			t.Errorf("server missing always-on tool %q", name)
		}
	}
}
