package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/vault"
)

// Run starts the MCP server. If httpAddr is non-empty, it serves Streamable HTTP
// on that address; otherwise it uses stdio transport.
func Run(vaultPath, httpAddr string) {
	v := vault.New(vaultPath)

	s := server.NewMCPServer(
		"lucidvault",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	registerTools(s, v)

	if httpAddr != "" {
		httpServer := server.NewStreamableHTTPServer(s)
		log.Printf("LucidVault MCP server listening on %s", httpAddr)
		if err := httpServer.Start(httpAddr); err != nil {
			log.Fatalf("MCP HTTP server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("MCP stdio server error: %v", err)
		}
	}
}

func registerTools(s *server.MCPServer, v *vault.Vault) {
	// get_soul — Always
	s.AddTool(mcp.NewTool("get_soul",
		mcp.WithDescription("Read the user's profile (soul.md). Contains identity, interests, and preferences. Read this first to understand who you're helping."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, err := HandleGetSoul(v)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// search_index — Discovery
	s.AddTool(mcp.NewTool("search_index",
		mcp.WithDescription("Search the knowledge base index for topics, titles, and tags. Use this first for topic discovery before reading full pages. Returns lightweight references, not full content."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search keywords"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		results, err := HandleSearchIndex(v, query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshalling results: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})

	// read_wiki — Primary
	s.AddTool(mcp.NewTool("read_wiki",
		mcp.WithDescription("Read a curated wiki page. These are LLM-enriched summaries with key takeaways, tags, and links. Preferred source of knowledge — use before falling back to raw sources."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Wiki page slug (from search_index results)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug := req.GetString("slug", "")
		if slug == "" {
			return mcp.NewToolResultError("slug is required"), nil
		}
		content, err := HandleReadWiki(v, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// grep_vault — Search
	s.AddTool(mcp.NewTool("grep_vault",
		mcp.WithDescription("Search for exact terms across the vault. Useful for CLI flags, config keys, API names, error messages, and specific technical terms. Scoped to specific sections."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search pattern"),
		),
		mcp.WithString("scope",
			mcp.Description("Section to search: wiki (default), notes, raw, or all"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		scope := req.GetString("scope", "wiki")
		results, err := HandleGrepVault(v, query, scope)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshalling results: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})

	// read_note — Secondary
	s.AddTool(mcp.NewTool("read_note",
		mcp.WithDescription("Read a personal note. These contain the user's own thoughts, reflections, and working notes. Use after wiki pages for personal context."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Note path relative to vault (e.g. notes/aks-thoughts.md)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path := req.GetString("path", "")
		if path == "" {
			return mcp.NewToolResultError("path is required"), nil
		}
		content, err := HandleReadNote(v, path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// read_raw — Fallback
	s.AddTool(mcp.NewTool("read_raw",
		mcp.WithDescription("Read the original scraped source content. These files are verbose and token-expensive. Only use when wiki summaries and notes don't provide enough detail."),
		mcp.WithString("filename",
			mcp.Required(),
			mcp.Description("Raw filename (e.g. 2024-01-15-kubernetes-networking.md)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filename := req.GetString("filename", "")
		if filename == "" {
			return mcp.NewToolResultError("filename is required"), nil
		}
		content, err := HandleReadRaw(v, filename)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(content), nil
	})

	// related_notes — Navigation
	s.AddTool(mcp.NewTool("related_notes",
		mcp.WithDescription("Get pages related to a given note by following its wiki-links. Returns linked pages for exploratory navigation. Use after reading a page to discover connected knowledge."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Wiki page slug to find relations for"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug := req.GetString("slug", "")
		if slug == "" {
			return mcp.NewToolResultError("slug is required"), nil
		}
		results, err := HandleRelatedNotes(v, slug)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshalling results: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})

	// add_bookmark — Write
	s.AddTool(mcp.NewTool("add_bookmark",
		mcp.WithDescription("Add a URL to the inbox for processing. Creates an inbox file that the pipeline will scrape, enrich, and index. Use this to save articles, docs, or any web page to the knowledge base."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("URL to bookmark"),
		),
		mcp.WithString("title",
			mcp.Description("Human-readable title (used for filename). If omitted, derived from URL."),
		),
		mcp.WithString("tags",
			mcp.Description("Comma-separated tags for categorization (e.g. \"golang, testing\")"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rawURL := req.GetString("url", "")
		if rawURL == "" {
			return mcp.NewToolResultError("url is required"), nil
		}
		title := req.GetString("title", "")
		tags := parseTags(req.GetString("tags", ""))
		filename, err := HandleAddBookmark(v, rawURL, title, tags)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Bookmark saved to inbox/%s — it will be processed in the next pipeline run.", filename)), nil
	})

	// add_note — Write
	s.AddTool(mcp.NewTool("add_note",
		mcp.WithDescription("Create a personal note in the knowledge base. The note will be auto-tagged and indexed by the pipeline. Use this to capture thoughts, reflections, or working notes."),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description("Note title (used for H1 heading and filename)"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("Markdown body of the note"),
		),
		mcp.WithString("tags",
			mcp.Description("Comma-separated tags (e.g. \"golang, testing\"). If omitted, the pipeline will auto-tag."),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title := req.GetString("title", "")
		if title == "" {
			return mcp.NewToolResultError("title is required"), nil
		}
		content := req.GetString("content", "")
		if content == "" {
			return mcp.NewToolResultError("content is required"), nil
		}
		tags := parseTags(req.GetString("tags", ""))
		filename, err := HandleAddNote(v, title, content, tags)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Note saved to notes/%s", filename)), nil
	})
}

// parseTags splits a comma-separated tag string into a slice.
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}
