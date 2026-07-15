package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/agentsmd"
	"lucidvault/internal/vault"
)

// readToolNames are the content-read tools that duplicate direct filesystem
// access and are therefore gated behind MCP_READ_TOOLS (off by default so
// filesystem-capable agents like Hermes read the vault natively).
var readToolNames = []string{
	"get_soul",
	"read_wiki",
	"grep_vault",
	"read_note",
	"read_raw",
	"vault_overview",
}

// alwaysOnToolNames are registered regardless of the read-tool flag: graph
// traversal (not reconstructable from a single file read), discovery tools, and
// all writes. search_wiki is always-on because it returns only index metadata
// (slug/title/tags) — the map, not the territory — enabling slug discovery for
// graph tools without exposing page content (see ADR-026).
var alwaysOnToolNames = []string{
	"search_wiki",
	"related_notes",
	"add_bookmark",
	"add_note",
	"update_wiki",
	"expand_graph",
	"delete_page",
	"edit_page",
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

// ---------------------------------------------------------------------------
// edit_page — driven through the real server (M1)
// ---------------------------------------------------------------------------
//
// The tests below dispatch a genuine JSON-RPC "tools/call" message through
// server.MCPServer.HandleMessage, exercising the full registerTools closure
// (argument extraction, the slug/content guards, the HandleEditPage
// error->ToolResultError mapping, and the success string), not just the
// underlying HandleEditPage function tested in write_tools_test.go.

// callToolThroughServer builds a JSON-RPC "tools/call" request for the named
// tool and dispatches it via s.HandleMessage, returning the resulting
// *mcp.CallToolResult. It fails the test if the call errors at the transport
// level (i.e. anything other than a tool-level CallToolResult, such as an
// unknown tool or unparsable request).
func callToolThroughServer(t *testing.T, s *server.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshalling tool call request: %v", err)
	}

	resp := s.HandleMessage(context.Background(), raw)

	switch r := resp.(type) {
	case mcp.JSONRPCResponse:
		result, ok := r.Result.(*mcp.CallToolResult)
		if !ok {
			t.Fatalf("unexpected result type %T for tool %q", r.Result, name)
		}
		return result
	case mcp.JSONRPCError:
		t.Fatalf("tool %q call failed at transport level: %s", name, r.Error.Message)
	default:
		t.Fatalf("unexpected response type %T for tool %q", resp, name)
	}
	return nil
}

// resultText extracts the text of a CallToolResult's first content item,
// failing the test if the result has no text content.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("tool result has no content")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("tool result content is not text: %T", result.Content[0])
	}
	return tc.Text
}

// TestEditPageTool_ThroughServer drives the edit_page tool through a real
// server.MCPServer, pinning: the success path (result text + actual file
// mutation), the empty-slug guard, the empty-content guard, and that a
// HandleEditPage error (nonexistent slug) surfaces as a tool-level
// ToolResultError rather than a transport-level error.
func TestEditPageTool_ThroughServer(t *testing.T) {
	tmpDir := t.TempDir()
	for _, sub := range []string{"wiki", "raw", "notes"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, sub), 0o755); err != nil {
			t.Fatalf("creating %s dir: %v", sub, err)
		}
	}
	v := vault.New(tmpDir)
	db := newTestStoreForMCP(t)

	page := "---\ntitle: \"Server Test\"\ntags: []\n---\n\n## Summary\nOriginal.\n"
	pagePath := filepath.Join(tmpDir, "wiki/server-edit-test.md")
	if err := os.WriteFile(pagePath, []byte(page), 0o644); err != nil {
		t.Fatalf("writing wiki page: %v", err)
	}

	s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
	registerTools(s, v, db, false)

	t.Run("success", func(t *testing.T) {
		result := callToolThroughServer(t, s, "edit_page", map[string]any{
			"slug":    "server-edit-test",
			"content": "## Rewritten\nNew body.\n",
		})
		if result.IsError {
			t.Fatalf("expected success, got error result: %s", resultText(t, result))
		}
		if want, got := "wiki/server-edit-test.md updated successfully.", resultText(t, result); got != want {
			t.Errorf("result text = %q, want %q", got, want)
		}

		content, err := os.ReadFile(pagePath)
		if err != nil {
			t.Fatalf("reading wiki page: %v", err)
		}
		if !strings.Contains(string(content), "## Rewritten") {
			t.Errorf("wiki file should contain new body after edit_page, got:\n%s", string(content))
		}
		if strings.Contains(string(content), "Original.") {
			t.Errorf("wiki file should not contain old body after edit_page, got:\n%s", string(content))
		}
	})

	t.Run("empty slug returns tool error", func(t *testing.T) {
		result := callToolThroughServer(t, s, "edit_page", map[string]any{
			"slug":    "",
			"content": "content",
		})
		if !result.IsError {
			t.Fatalf("expected error result for empty slug")
		}
		if text := resultText(t, result); !strings.Contains(text, "slug is required") {
			t.Errorf("expected error text to contain %q, got %q", "slug is required", text)
		}
	})

	t.Run("empty content returns tool error", func(t *testing.T) {
		result := callToolThroughServer(t, s, "edit_page", map[string]any{
			"slug":    "server-edit-test",
			"content": "",
		})
		if !result.IsError {
			t.Fatalf("expected error result for empty content")
		}
		if text := resultText(t, result); !strings.Contains(text, "content is required") {
			t.Errorf("expected error text to contain %q, got %q", "content is required", text)
		}
	})

	t.Run("HandleEditPage error surfaces as tool error, not transport error", func(t *testing.T) {
		result := callToolThroughServer(t, s, "edit_page", map[string]any{
			"slug":    "does-not-exist",
			"content": "## Body\nSome content.\n",
		})
		if !result.IsError {
			t.Fatalf("expected error result for nonexistent slug")
		}
		if text := resultText(t, result); text == "" {
			t.Errorf("expected a non-empty error message for nonexistent slug")
		}
	})
}

// ---------------------------------------------------------------------------
// search_wiki — driven through the real server
// ---------------------------------------------------------------------------

// TestSearchWikiTool_ThroughServer verifies the search_wiki tool is always
// registered (regardless of readTools flag), that empty/whitespace queries
// return a tool-level error, and that a valid query returns JSON results.
func TestSearchWikiTool_ThroughServer(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a minimal index so the vault can serve search results.
	indexContent := "# Wiki Index\n\n- [[kubernetes-networking]] — Kubernetes Networking Deep Dive [kubernetes, networking]\n- [[gitops]] — GitOps with ArgoCD [gitops, argocd, kubernetes]\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	v := vault.New(tmpDir)
	db := newTestStoreForMCP(t)

	// search_wiki must be always-on: register with readTools=false.
	s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
	registerTools(s, v, db, false)

	t.Run("empty query returns tool error", func(t *testing.T) {
		result := callToolThroughServer(t, s, "search_wiki", map[string]any{
			"query": "",
		})
		if !result.IsError {
			t.Fatalf("expected tool error for empty query, got success")
		}
		if text := resultText(t, result); !strings.Contains(text, "query is required") {
			t.Errorf("expected 'query is required' in error, got %q", text)
		}
	})

	t.Run("whitespace-only query returns tool error", func(t *testing.T) {
		result := callToolThroughServer(t, s, "search_wiki", map[string]any{
			"query": "   ",
		})
		if !result.IsError {
			t.Fatalf("expected tool error for whitespace-only query, got success")
		}
		if text := resultText(t, result); !strings.Contains(text, "query is required") {
			t.Errorf("expected 'query is required' in error, got %q", text)
		}
	})

	t.Run("valid query returns JSON array of results", func(t *testing.T) {
		result := callToolThroughServer(t, s, "search_wiki", map[string]any{
			"query": "kubernetes",
		})
		if result.IsError {
			t.Fatalf("expected success, got error: %s", resultText(t, result))
		}
		text := resultText(t, result)
		var entries []IndexEntry
		if err := json.Unmarshal([]byte(text), &entries); err != nil {
			t.Fatalf("result is not valid JSON array: %v — got: %s", err, text)
		}
		if len(entries) == 0 {
			t.Error("expected at least one result for 'kubernetes'")
		}
	})

	t.Run("search_wiki available when readTools=false", func(t *testing.T) {
		live := s.ListTools()
		if _, ok := live["search_wiki"]; !ok {
			t.Error("search_wiki must be registered even when readTools=false")
		}
	})
}

// ---------------------------------------------------------------------------
// related_notes — through-server: unknown slug error includes suggestions
// ---------------------------------------------------------------------------

// TestRelatedNotesTool_NotFoundIncludesSuggestions verifies that when
// related_notes is called with a slug that does not exist, the tool-level error
// text contains "similar pages:" populated from the index.
func TestRelatedNotesTool_NotFoundIncludesSuggestions(t *testing.T) {
	tmpDir := t.TempDir()
	for _, sub := range []string{"wiki", "raw", "notes"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, sub), 0o755); err != nil {
			t.Fatalf("creating %s dir: %v", sub, err)
		}
	}

	// Seed index with Apple-M7 entries so suggestSlugs finds them.
	indexContent := "# Wiki Index\n\n- [[apple-m7-ultra-komt-in-2028]] — Apple M7 Ultra [apple, silicon, m7]\n- [[apple-silicon-roadmap]] — Apple Silicon Roadmap [apple, silicon]\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "index.md"), []byte(indexContent), 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}

	v := vault.New(tmpDir)
	db := newTestStoreForMCP(t)
	s := server.NewMCPServer("lucidvault-test", "0.0.0", server.WithToolCapabilities(false))
	registerTools(s, v, db, false)

	result := callToolThroughServer(t, s, "related_notes", map[string]any{
		"slug": "apple-m7",
	})
	if !result.IsError {
		t.Fatalf("expected tool error for unknown slug 'apple-m7', got success")
	}
	text := resultText(t, result)
	if !strings.Contains(text, "similar pages:") {
		t.Errorf("tool error text missing 'similar pages:' clause; got: %q", text)
	}
	if !strings.Contains(text, "apple-m7-ultra-komt-in-2028") {
		t.Errorf("tool error text missing expected suggestion; got: %q", text)
	}
}
