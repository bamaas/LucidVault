package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"lucidvault/internal/agentsmd"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// Run starts the standalone MCP server (the `lucidvault mcp` subcommand). If
// httpAddr is non-empty, it serves Streamable HTTP on that address with a
// localhost Host-guard; otherwise it uses stdio transport. dbPath is the path
// to the SQLite database for write operations (edges, bookmarks, notes).
//
// For the in-process server that runs alongside the pipeline, use NewServer +
// ServeHTTP directly with a shared *store.Store and *vault.Vault.
func Run(vaultPath, httpAddr, dbPath string, readTools bool) {
	v := vault.New(vaultPath)

	var db *store.Store
	if dbPath != "" {
		var err error
		db, err = store.New(dbPath)
		if err != nil {
			log.Fatalf("opening database for MCP server: %v", err)
		}
		defer func() { _ = db.Close() }()
	}

	s := NewServer(v, db, readTools)

	if httpAddr != "" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		log.Printf("LucidVault MCP server listening on %s", httpAddr)
		// Standalone HTTP defaults to a localhost-only Host-guard.
		if err := ServeHTTP(ctx, s, httpAddr, []string{"localhost", "127.0.0.1"}); err != nil {
			log.Fatalf("MCP HTTP server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("MCP stdio server error: %v", err)
		}
	}
}

func registerTools(s *server.MCPServer, v *vault.Vault, db *store.Store, readTools bool) {
	// Read tools duplicate direct filesystem access and are gated behind
	// MCP_READ_TOOLS (off by default) so filesystem-capable agents read the
	// vault natively. Graph traversal and write tools (below) are always on.
	if readTools {
		// get_soul — reads soul.md
		s.AddTool(mcp.NewTool("get_soul",
			mcp.WithDescription("Read the user's profile (soul.md). Contains identity, interests, and preferences. Read this first to understand who you're helping."),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			content, err := HandleGetSoul(v)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(content), nil
		})

		// read_wiki — Primary
		s.AddTool(mcp.NewTool("read_wiki",
			mcp.WithDescription("Read a curated wiki page. These are LLM-enriched summaries with key takeaways, tags, and links. Preferred source of knowledge — use before falling back to raw sources. Not-found errors include similar-slug suggestions so you can retry without an extra search_wiki call."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Wiki page slug (from search_wiki results)"),
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
	} // end gated read tools (get_soul..read_raw)

	// search_wiki — Discovery (always-on: returns only index metadata, not page content;
	// see ADR-026 for rationale on moving this out of the read-tool gate).
	// Filesystem-capable agents should prefer native grep for content search.
	s.AddTool(mcp.NewTool("search_wiki",
		mcp.WithDescription("Search wiki pages by topic keywords across slug, title, and tags. Returns slugs for use with related_notes / expand_graph / read_wiki. For filesystem-capable agents, prefer native grep for content search."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search keywords (multi-word queries use AND semantics: every term must match)"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := strings.TrimSpace(req.GetString("query", ""))
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

	// related_notes — Navigation (bidirectional via edges table)
	s.AddTool(mcp.NewTool("related_notes",
		mcp.WithDescription("Get pages related to a given wiki page using bidirectional edge traversal. Returns outbound links (pages this page references), inbound links (pages that reference this page), and pages with both directions. Not-found errors include similar-slug suggestions so you can retry without an extra search_wiki call."),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Wiki page slug to find relations for"),
		),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		slug := req.GetString("slug", "")
		if slug == "" {
			return mcp.NewToolResultError("slug is required"), nil
		}
		results, err := HandleRelatedNotes(v, slug, db)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshalling results: %v", err)), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	})

	// vault_overview — Orientation (gated read tool)
	if readTools {
		s.AddTool(mcp.NewTool("vault_overview",
			mcp.WithDescription("Get a high-level overview of the vault: page counts, edge count, top tags, and metadata. Use this for orientation before diving into specific queries."),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			overview, err := HandleVaultOverview(v, db)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, err := json.Marshal(overview)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("marshalling overview: %v", err)), nil
			}
			return mcp.NewToolResultText(string(data)), nil
		})
	} // end gated read tool (vault_overview)

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

	// update_wiki — Write (requires store)
	if db != nil {
		s.AddTool(mcp.NewTool("update_wiki",
			mcp.WithDescription("Update a section of a wiki page. Replaces the content under a ## heading while preserving frontmatter and other sections. If the Related section is updated, edges are synced automatically."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Wiki page slug (e.g. kubernetes-networking)"),
			),
			mcp.WithString("section",
				mcp.Required(),
				mcp.Description("Section heading to update (e.g. Summary, Key Takeaways, Related)"),
			),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("New markdown content for the section"),
			),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			slug := req.GetString("slug", "")
			if slug == "" {
				return mcp.NewToolResultError("slug is required"), nil
			}
			section := req.GetString("section", "")
			if section == "" {
				return mcp.NewToolResultError("section is required"), nil
			}
			content := req.GetString("content", "")
			if content == "" {
				return mcp.NewToolResultError("content is required"), nil
			}
			if err := HandleUpdateWiki(v, db, slug, section, content); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Section %q of wiki/%s.md updated successfully.", section, slug)), nil
		})

		// edit_page — Write (requires store)
		s.AddTool(mcp.NewTool("edit_page",
			mcp.WithDescription("Replace the entire body of an existing wiki page. Frontmatter (title, source, tags, created) is preserved verbatim except last_updated, which is bumped. All [[wikilinks]] in the new body are re-derived as edges. Use for whole-page rewrites or adding new sections; for a single-section edit, prefer update_wiki."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Wiki page slug (e.g. kubernetes-networking)"),
			),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("New body markdown, no frontmatter"),
			),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			slug := req.GetString("slug", "")
			if slug == "" {
				return mcp.NewToolResultError("slug is required"), nil
			}
			content := req.GetString("content", "")
			if strings.TrimSpace(content) == "" {
				return mcp.NewToolResultError("content is required"), nil
			}
			if err := HandleEditPage(v, db, slug, content); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("wiki/%s.md updated successfully.", slug)), nil
		})

		// expand_graph — Graph traversal (requires store)
		s.AddTool(mcp.NewTool("expand_graph",
			mcp.WithDescription("Expand a set of seed slugs by traversing wiki-link edges up to N hops. Returns all connected slugs within the hop radius. Use this to discover clusters of related content."),
			mcp.WithString("seeds",
				mcp.Required(),
				mcp.Description("Comma-separated list of slugs to expand from"),
			),
			mcp.WithNumber("hops",
				mcp.Description("Max traversal depth (1-5, default 2)"),
			),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			seedsRaw := req.GetString("seeds", "")
			if seedsRaw == "" {
				return mcp.NewToolResultError("seeds is required"), nil
			}
			seeds := parseTags(seedsRaw) // reuse comma-splitting logic
			hops := int(req.GetFloat("hops", 2))
			result, err := HandleExpandGraph(db, seeds, hops)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, err := json.Marshal(result)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("marshalling result: %v", err)), nil
			}
			return mcp.NewToolResultText(string(data)), nil
		})

		// delete_page — Write (requires store)
		s.AddTool(mcp.NewTool("delete_page",
			mcp.WithDescription("Delete a vault page and all its artifacts (wiki file, raw file, index entry, edges, DB record). Returns a list of pages that still reference the deleted page (dangling references)."),
			mcp.WithString("slug",
				mcp.Required(),
				mcp.Description("Wiki page slug to delete (e.g. kubernetes-networking or notes/my-note)"),
			),
		), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			slug := req.GetString("slug", "")
			if slug == "" {
				return mcp.NewToolResultError("slug is required"), nil
			}
			result, err := HandleDeletePage(v, db, slug)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, err := json.Marshal(result)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("marshalling result: %v", err)), nil
			}
			return mcp.NewToolResultText(string(data)), nil
		})
	}
}

// RegisteredTools returns metadata for the MCP tools registered for the given
// read-tool state, matching the tools defined in registerTools. Callers must
// pass the same readTools value used for NewServer/registerTools so AGENTS.md
// documents exactly the tools the server exposes.
//
// It assumes a non-nil store — the only configuration used in practice, since
// both the in-process server and the `mcp` subcommand always open the DB. The
// store-gated tools (update_wiki, edit_page, expand_graph, delete_page) are therefore
// always listed.
func RegisteredTools(readTools bool) []agentsmd.ToolInfo {
	var tools []agentsmd.ToolInfo

	// Gated read tools — see registerTools / MCP_READ_TOOLS.
	if readTools {
		tools = append(tools,
			agentsmd.ToolInfo{
				Name:        "get_soul",
				Description: "Read the user's profile (soul.md). Contains identity, interests, and preferences.",
			},
			agentsmd.ToolInfo{
				Name:        "read_wiki",
				Description: "Read a curated wiki page (LLM-enriched summary with key takeaways, tags, and links). Not-found errors include similar-slug suggestions.",
				Parameters: []agentsmd.ParamInfo{
					{Name: "slug", Description: "Wiki page slug (from search_wiki results)", Required: true},
				},
			},
			agentsmd.ToolInfo{
				Name:        "grep_vault",
				Description: "Search for exact terms across the vault. Scoped to specific sections.",
				Parameters: []agentsmd.ParamInfo{
					{Name: "query", Description: "Search pattern", Required: true},
					{Name: "scope", Description: "Section to search: wiki (default), notes, raw, or all"},
				},
			},
			agentsmd.ToolInfo{
				Name:        "read_note",
				Description: "Read a personal note (user's own thoughts, reflections, and working notes).",
				Parameters: []agentsmd.ParamInfo{
					{Name: "path", Description: "Note path relative to vault (e.g. notes/aks-thoughts.md)", Required: true},
				},
			},
			agentsmd.ToolInfo{
				Name:        "read_raw",
				Description: "Read the original scraped source content. Verbose and token-expensive.",
				Parameters: []agentsmd.ParamInfo{
					{Name: "filename", Description: "Raw filename (e.g. 2024-01-15-kubernetes-networking.md)", Required: true},
				},
			},
			agentsmd.ToolInfo{
				Name:        "vault_overview",
				Description: "Get a high-level overview of the vault: page counts, edge count, top tags, and metadata.",
			},
		)
	}

	// Always-on: discovery (search_wiki), graph traversal, and all writes.
	// search_wiki returns only index metadata (slug/title/tags), making it a
	// discovery tool rather than a content-read tool (see ADR-026).
	tools = append(tools,
		agentsmd.ToolInfo{
			Name:        "search_wiki",
			Description: "Search wiki pages by topic keywords across slug, title, and tags. Returns slugs for use with related_notes / expand_graph / read_wiki.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "query", Description: "Search keywords (multi-word queries use AND semantics)", Required: true},
			},
		},
		agentsmd.ToolInfo{
			Name:        "related_notes",
			Description: "Get pages related to a wiki page using bidirectional edge traversal. Not-found errors include similar-slug suggestions.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "slug", Description: "Wiki page slug to find relations for", Required: true},
			},
		},
		agentsmd.ToolInfo{
			Name:        "expand_graph",
			Description: "Expand seed slugs by traversing wiki-link edges up to N hops.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "seeds", Description: "Comma-separated list of slugs to expand from", Required: true},
				{Name: "hops", Description: "Max traversal depth (1-5, default 2)"},
			},
		},
		agentsmd.ToolInfo{
			Name:        "add_bookmark",
			Description: "Add a URL to the inbox for processing by the pipeline.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "url", Description: "URL to bookmark", Required: true},
				{Name: "title", Description: "Human-readable title (used for filename). If omitted, derived from URL."},
				{Name: "tags", Description: "Comma-separated tags for categorization"},
			},
		},
		agentsmd.ToolInfo{
			Name:        "add_note",
			Description: "Create a personal note in the knowledge base.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "title", Description: "Note title (used for H1 heading and filename)", Required: true},
				{Name: "content", Description: "Markdown body of the note", Required: true},
				{Name: "tags", Description: "Comma-separated tags. If omitted, the pipeline will auto-tag."},
			},
		},
		agentsmd.ToolInfo{
			Name:        "update_wiki",
			Description: "Update a section of a wiki page. Preserves frontmatter and other sections.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "slug", Description: "Wiki page slug (e.g. kubernetes-networking)", Required: true},
				{Name: "section", Description: "Section heading to update (e.g. Summary, Key Takeaways, Related)", Required: true},
				{Name: "content", Description: "New markdown content for the section", Required: true},
			},
		},
		agentsmd.ToolInfo{
			Name:        "delete_page",
			Description: "Delete a vault page and all its artifacts (wiki, raw, index entry, edges, DB record).",
			Parameters: []agentsmd.ParamInfo{
				{Name: "slug", Description: "Wiki page slug to delete", Required: true},
			},
		},
		agentsmd.ToolInfo{
			Name:        "edit_page",
			Description: "Replace the entire body of a wiki page. Preserves frontmatter (bumps last_updated) and re-derives edges from the whole new body.",
			Parameters: []agentsmd.ParamInfo{
				{Name: "slug", Description: "Wiki page slug (e.g. kubernetes-networking)", Required: true},
				{Name: "content", Description: "New body markdown, no frontmatter", Required: true},
			},
		},
	)

	return tools
}

// parseTags splits a comma-separated tag string into a slice.
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	var tags []string
	for t := range strings.SplitSeq(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}
