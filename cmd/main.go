package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lucidvault/internal/agentsmd"
	"lucidvault/internal/claudemd"
	"lucidvault/internal/enrich"
	"lucidvault/internal/inbox"
	"lucidvault/internal/mcpserver"
	"lucidvault/internal/notes"
	"lucidvault/internal/raindrop"
	"lucidvault/internal/scraper"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

type config struct {
	raindropToken     string
	ollamaAPIKey      string
	ollamaModel       string
	vaultPath         string
	pollInterval      time.Duration
	enrichDelayMs     int
	enrichRetries     int
	supadataAPIKey    string
	forceReEnrich     bool
	forceReFetch      bool
	hygieneInterval   int
	mcpHTTPAddr       string
	mcpAllowedHosts   []string
	mcpReadTools      bool
	webSearchStrategy agentsmd.WebSearchStrategy
}

// pollCycleCount tracks the number of poll cycles for hygiene scheduling.
// TODO: move to struct field when poll loop is refactored into a type.
var pollCycleCount int

func main() {
	// Handle "mcp" subcommand before flag parsing.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCP(os.Args[2:])
		return
	}

	reEnrich := flag.Bool("re-enrich", false, "re-enrich all bookmarks using updated prompt, then exit")
	reFetch := flag.Bool("re-fetch", false, "re-fetch all bookmarks from external sources to inbox, then exit")
	rebuildEdges := flag.Bool("rebuild-edges", false, "force full rebuild of wikilink edges, then continue normally")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *reEnrich && *reFetch {
		slog.Error("--re-enrich and --re-fetch are mutually exclusive")
		os.Exit(1)
	}

	cfg, err := loadConfig(*reEnrich, *reFetch)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if cfg.forceReFetch && cfg.raindropToken == "" {
		slog.Error("--re-fetch requires an external source (set RAINDROP_ACCESS_TOKEN)")
		os.Exit(1)
	}

	// Upsert LucidVault section into host CLAUDE.md (best-effort)
	claudeMDPath := os.Getenv("CLAUDE_MD_PATH")
	if claudeMDPath == "" {
		claudeMDPath = "/CLAUDE.md"
	}
	if _, err := os.Stat(claudeMDPath); err == nil {
		if err := claudemd.Upsert(claudeMDPath, cfg.vaultPath); err != nil {
			slog.Warn("failed to upsert CLAUDE.md section", "path", claudeMDPath, "error", err)
		} else {
			slog.Info("CLAUDE.md section upserted", "path", claudeMDPath)
		}
	}

	// Resolve the actual path inside the container (may differ from VAULT_PATH on Docker Desktop/macOS)
	vaultPath := resolveContainerPath(cfg.vaultPath)
	if vaultPath != cfg.vaultPath {
		slog.Info("resolved vault mount point", "host_path", cfg.vaultPath, "container_path", vaultPath)
	}

	// Initialize vault
	v := vault.New(vaultPath)
	if err := v.Init(); err != nil {
		slog.Error("failed to initialize vault", "error", err)
		os.Exit(1)
	}
	slog.Info("vault initialized", "path", vaultPath)

	// Initialize SQLite store
	dbPath := filepath.Join(vaultPath, ".lucidvault.db")
	db, err := store.New(dbPath)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	slog.Info("database initialized", "path", dbPath)

	// Edge rebuild: trigger on --rebuild-edges flag or when edges table is empty (D5)
	rebuildNeeded := *rebuildEdges
	if !rebuildNeeded {
		edgeCount, err := db.EdgeCount()
		if err != nil {
			slog.Error("failed to check edge count", "error", err)
		} else if edgeCount == 0 {
			rebuildNeeded = true
			slog.Info("edges table empty, triggering full rebuild")
		}
	}
	if rebuildNeeded {
		if err := rebuildAllEdges(db, v); err != nil {
			slog.Error("failed to rebuild edges", "error", err)
		}
	}

	// Initialize Raindrop client (optional)
	var rd *raindrop.Client
	if cfg.raindropToken != "" {
		rd = raindrop.NewClient(cfg.raindropToken)
		slog.Info("raindrop source enabled")
	}

	// Initialize scraper and enricher
	sc := scraper.New()
	if cfg.supadataAPIKey != "" {
		sc.SetYouTubeClient(scraper.NewYouTubeClient(cfg.supadataAPIKey))
		slog.Info("youtube transcript support enabled via supadata")
	}
	en := enrich.NewClient(cfg.ollamaAPIKey, cfg.ollamaModel, cfg.enrichRetries, cfg.enrichDelayMs)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		slog.Info("shutdown signal received, finishing current item...", "signal", sig)
		cancel()
	}()

	// Serve MCP over HTTP in-process, sharing the pipeline's store and vault.
	// Skipped for one-shot modes (--re-enrich / --re-fetch) which exit quickly.
	if cfg.mcpHTTPAddr != "" && !cfg.forceReEnrich && !cfg.forceReFetch {
		mcpSrv := mcpserver.NewServer(v, db, cfg.mcpReadTools)
		slog.Info("starting in-process MCP HTTP server",
			"addr", cfg.mcpHTTPAddr,
			"allowed_hosts", cfg.mcpAllowedHosts,
		)
		go func() {
			if err := mcpserver.ServeHTTP(ctx, mcpSrv, cfg.mcpHTTPAddr, cfg.mcpAllowedHosts); err != nil {
				slog.Error("mcp http server failed", "error", err)
				cancel() // a bind failure must not leave a half-running daemon
			}
		}()
	}

	slog.Info("starting lucidvault",
		"poll_interval", cfg.pollInterval,
		"model", cfg.ollamaModel,
		"raindrop_enabled", rd != nil,
		"force_re_enrich", cfg.forceReEnrich,
		"force_re_fetch", cfg.forceReFetch,
	)

	// Run immediately on startup, then on ticker
	runPollCycle(ctx, cfg, rd, sc, en, db, v)

	if cfg.forceReEnrich {
		slog.Info("re-enrichment complete, exiting")
		return
	}

	if cfg.forceReFetch {
		slog.Info("re-fetch complete, exiting")
		return
	}

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case <-ticker.C:
			runPollCycle(ctx, cfg, rd, sc, en, db, v)
		}
	}
}

func runPollCycle(ctx context.Context, cfg *config, rd *raindrop.Client, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	// Step 1: Sync Raindrop bookmarks to inbox (optional)
	if rd != nil {
		syncRaindropToInbox(ctx, rd, db, v, cfg.forceReFetch)
	}

	// Step 2: Process inbox
	if cfg.forceReEnrich {
		reEnrichAll(ctx, en, db, v)
	} else {
		processInbox(ctx, sc, en, db, v)
	}

	// Step 3: Process notes
	processNotes(ctx, en, db, v)

	// Step 4: Hygiene cycle (every Nth poll cycle)
	pollCycleCount++
	if cfg.hygieneInterval > 0 && pollCycleCount%cfg.hygieneInterval == 0 {
		runHygiene(db, v)
	}

	// Step 5: Generate AGENTS.md
	generateAgentsMD(db, v, cfg.mcpReadTools, cfg.webSearchStrategy)
}

func syncRaindropToInbox(ctx context.Context, rd *raindrop.Client, db *store.Store, v *vault.Vault, force bool) {
	if ctx.Err() != nil {
		return
	}

	slog.Info("syncing raindrop bookmarks to inbox")

	bookmarks, err := rd.FetchBookmarks(ctx)
	if err != nil {
		slog.Error("failed to fetch raindrop bookmarks", "error", err)
		return
	}

	created, err := raindrop.SyncToInbox(bookmarks, db, v.BasePath, force)
	if err != nil {
		slog.Error("failed to sync bookmarks to inbox", "error", err)
		return
	}

	slog.Info("raindrop sync complete", "fetched", len(bookmarks), "new_inbox_files", created)
}

func processInbox(ctx context.Context, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	slog.Info("processing inbox")

	var processed, failed int
	failedPaths := make(map[string]bool)

	for {
		if ctx.Err() != nil {
			slog.Info("shutdown requested, stopping processing")
			break
		}

		item, err := inbox.ScanNext(v.BasePath, failedPaths)
		if err != nil {
			slog.Error("failed to scan inbox", "error", err)
			break
		}

		if item == nil {
			break
		}

		if err := processInboxItem(ctx, *item, sc, en, db, v); err != nil {
			slog.Error("failed to process inbox item", "url", item.URL, "error", err)
			failedPaths[item.Path] = true
			failed++
			continue
		}

		// Delete inbox file after successful processing
		if err := inbox.Delete(item.Path); err != nil {
			slog.Error("failed to delete inbox file", "path", item.Path, "error", err)
		}

		processed++
	}

	if processed == 0 && failed == 0 {
		slog.Info("inbox empty, nothing to process")
		return
	}

	if failed > 0 {
		slog.Warn("some inbox items failed — will be retried next cycle", "failed", failed)
	}

	slog.Info("inbox cycle complete", "processed", processed, "failed", failed)
}

func processInboxItem(ctx context.Context, item inbox.Item, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) error {
	slug := vault.GenerateSlug(item.Title)
	rawFilename := vault.GenerateRawFilename(slug)
	dateSaved := time.Now().Format("2006-01-02")

	slog.Info("processing inbox item", "title", item.Title, "url", item.URL)

	// Scrape via Jina
	scrapeResult, err := sc.Scrape(ctx, item.URL)
	var rawContent string
	if err != nil || scrapeResult == nil || !scrapeResult.OK {
		slog.Warn("scrape failed, using fallback", "url", item.URL, "error", err)
		rawContent = buildFallbackContent(item)
	} else {
		rawContent = scrapeResult.Content
	}

	// Write raw file
	rawFormatted := vault.FormatRawContent(item.Title, item.URL, dateSaved, item.Tags, rawContent)
	rawPath, err := v.WriteRaw(rawFilename, rawFormatted)
	if err != nil {
		return fmt.Errorf("writing raw file: %w", err)
	}

	// Read index and soul for enrichment context
	index, err := v.ReadIndex()
	if err != nil {
		slog.Warn("failed to read index.md", "error", err)
	}
	profile, err := v.ReadSoul()
	if err != nil {
		slog.Warn("failed to read soul.md", "error", err)
	}

	// Enrich via Ollama
	enrichInput := &enrich.EnrichInput{
		Content:     rawContent,
		Index:       index,
		UserTags:    item.Tags,
		RawFilename: rawFilename,
		Title:       item.Title,
		URL:         item.URL,
		DateSaved:   dateSaved,
		Profile:     profile,
	}

	wikiContent, err := en.Enrich(ctx, enrichInput)
	if err != nil {
		return fmt.Errorf("enriching: %w", err)
	}
	wikiContent = vault.FixFrontmatter(wikiContent)

	// Write wiki file
	wikiFilename := slug + ".md"
	wikiPath, err := v.WriteWiki(wikiFilename, wikiContent)
	if err != nil {
		return fmt.Errorf("writing wiki file: %w", err)
	}

	// Sync wikilink edges incrementally
	syncEdgesFromContent(db, slug, wikiContent)

	// Extract tags from enriched content for index
	tags := notes.ParseFrontmatter(wikiContent)
	if len(tags) == 0 {
		tags = item.Tags
	}

	// Remove old index entry (for reprocessing case) and add new one
	if err := v.RemoveFromIndex(slug); err != nil {
		slog.Warn("failed to remove old index entry", "slug", slug, "error", err)
	}

	title := item.Title
	if enrichedTitle := notes.ParseTitle(wikiContent); enrichedTitle != "" {
		title = enrichedTitle
	}
	if err := v.UpdateIndex(slug, title, tags); err != nil {
		return fmt.Errorf("updating index: %w", err)
	}

	// Auto-link: add backlinks to related pages
	autoLinkRelated(db, v, slug, tags, wikiContent)

	// Upsert to database
	normalizedURL := vault.NormalizeURL(item.URL)
	record := &store.BookmarkRecord{
		WikiPath:      wikiPath,
		RawPath:       rawPath,
		Title:         item.Title,
		URL:           item.URL,
		URLNormalized: normalizedURL,
		ProcessedAt:   time.Now(),
	}
	if err := db.UpsertBookmark(record); err != nil {
		return fmt.Errorf("upserting bookmark record: %w", err)
	}

	slog.Info("inbox item processed", "title", item.Title, "wiki", wikiPath, "raw", rawPath)
	return nil
}

func reEnrichAll(ctx context.Context, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	slog.Info("re-enriching all bookmarks")

	records, err := db.ListBookmarks()
	if err != nil {
		slog.Error("failed to list bookmarks for re-enrichment", "error", err)
		return
	}

	var processed, failed int

	for _, rec := range records {
		if ctx.Err() != nil {
			slog.Info("shutdown requested, stopping re-enrichment")
			break
		}

		if err := reEnrichBookmark(ctx, rec, en, db, v); err != nil {
			slog.Error("failed to re-enrich bookmark", "title", rec.Title, "error", err)
			failed++
			continue
		}
		processed++
	}

	slog.Info("re-enrichment complete", "processed", processed, "failed", failed)
}

func reEnrichBookmark(ctx context.Context, rec store.BookmarkRecord, en *enrich.Client, db *store.Store, v *vault.Vault) error {
	slog.Info("re-enriching bookmark", "title", rec.Title, "url", rec.URL)

	if rec.RawPath == "" {
		return fmt.Errorf("no raw path for bookmark %q, cannot re-enrich", rec.Title)
	}
	if rec.WikiPath == "" {
		return fmt.Errorf("no wiki path for bookmark %q, cannot re-enrich", rec.Title)
	}

	rawContent, err := v.ReadFile(rec.RawPath)
	if err != nil {
		return fmt.Errorf("reading raw file for re-enrichment: %w", err)
	}

	index, err := v.ReadIndex()
	if err != nil {
		slog.Warn("failed to read index.md", "error", err)
	}
	profile, err := v.ReadSoul()
	if err != nil {
		slog.Warn("failed to read soul.md", "error", err)
	}

	slug := vault.GenerateSlug(rec.Title)
	dateSaved := rec.ProcessedAt.Format("2006-01-02")

	// Recover user tags from the raw file frontmatter for enrichment context
	userTags := notes.ParseFrontmatter(rawContent)

	enrichInput := &enrich.EnrichInput{
		Content:     rawContent,
		Index:       index,
		UserTags:    userTags,
		RawFilename: filepath.Base(rec.RawPath),
		Title:       rec.Title,
		URL:         rec.URL,
		DateSaved:   dateSaved,
		Profile:     profile,
	}

	wikiContent, err := en.Enrich(ctx, enrichInput)
	if err != nil {
		return fmt.Errorf("re-enriching: %w", err)
	}
	wikiContent = vault.FixFrontmatter(wikiContent)

	// Handle slug change: remove old index entry and delete old wiki file
	wikiFilename := slug + ".md"
	oldSlug := strings.TrimSuffix(filepath.Base(rec.WikiPath), ".md")
	if oldSlug != slug {
		if err := v.RemoveFromIndex(oldSlug); err != nil {
			return fmt.Errorf("removing old index entry: %w", err)
		}
		if err := v.DeleteFile(rec.WikiPath); err != nil {
			slog.Warn("failed to delete old wiki file", "path", rec.WikiPath, "error", err)
		}
	} else {
		if err := v.RemoveFromIndex(slug); err != nil {
			return fmt.Errorf("removing old index entry: %w", err)
		}
	}

	wikiPath, err := v.WriteWiki(wikiFilename, wikiContent)
	if err != nil {
		return fmt.Errorf("writing wiki file: %w", err)
	}

	// Update index entry
	tags := notes.ParseFrontmatter(wikiContent)
	title := rec.Title
	if enrichedTitle := notes.ParseTitle(wikiContent); enrichedTitle != "" {
		title = enrichedTitle
	}
	if err := v.UpdateIndex(slug, title, tags); err != nil {
		return fmt.Errorf("updating index: %w", err)
	}

	// Update the DB record
	rec.WikiPath = wikiPath
	rec.ProcessedAt = time.Now()
	if err := db.UpsertBookmark(&rec); err != nil {
		return fmt.Errorf("updating bookmark record: %w", err)
	}

	slog.Info("bookmark re-enriched", "title", rec.Title, "wiki", wikiPath)
	return nil
}

func buildFallbackContent(item inbox.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", item.Title)
	fmt.Fprintf(&b, "URL: %s\n\n", item.URL)
	if len(item.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(item.Tags, ", "))
	}
	return b.String()
}

func processNotes(ctx context.Context, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	slog.Info("scanning notes")

	scanned, err := notes.Scan(v.BasePath)
	if err != nil {
		slog.Error("failed to scan notes", "error", err)
		return
	}

	// Build full set of scanned paths upfront for deletion detection.
	// This must happen before the processing loop so that an early shutdown
	// doesn't cause the reconciliation step to falsely delete valid notes.
	scannedPaths := make(map[string]struct{}, len(scanned))
	for _, nf := range scanned {
		scannedPaths[nf.Path] = struct{}{}
	}

	// Read index and soul for tag suggestion context
	index, err := v.ReadIndex()
	if err != nil {
		slog.Warn("failed to read index.md for notes", "error", err)
	}
	profile, err := v.ReadSoul()
	if err != nil {
		slog.Warn("failed to read soul.md for notes", "error", err)
	}

	var indexed, updated, skipped int

	for _, nf := range scanned {
		if ctx.Err() != nil {
			slog.Info("shutdown requested, stopping notes processing")
			break
		}

		existingHash, err := db.GetNoteHash(nf.Path)
		if err != nil {
			slog.Error("failed to check note hash", "path", nf.Path, "error", err)
			continue
		}

		if existingHash == nf.ContentHash {
			skipped++
			continue
		}

		// Read note content for wiki copy
		// TODO: cache content in NoteFile to avoid double read (Scan already reads for hashing)
		absPath := filepath.Join(v.BasePath, nf.Path)
		data, err := os.ReadFile(absPath)
		if err != nil {
			slog.Error("failed to read note file", "path", nf.Path, "error", err)
			continue
		}
		content := string(data)

		// Determine tags: use existing tags or auto-generate via LLM
		tags := nf.Tags
		if len(tags) == 0 {
			slog.Info("auto-tagging note", "path", nf.Path)
			tags, err = en.SuggestTags(ctx, &enrich.TagInput{
				Content: content,
				Title:   nf.Title,
				Index:   index,
				Profile: profile,
			})
			if err != nil {
				slog.Error("failed to suggest tags for note", "path", nf.Path, "error", err)
				continue
			}
		}

		// Build wiki copy: frontmatter + original body (stripped of existing frontmatter)
		body := notes.StripFrontmatter(content)
		wikiContent := buildNoteWikiContent(nf.Title, tags, body)

		// Wiki slug from filename: "notes/sub/my-note.md" → "my-note"
		wikiSlug := notes.TitleFromFilename(nf.Path)
		wikiFilename := wikiSlug + ".md"

		// Remove old index entry (may be under old slug)
		if existingHash != "" {
			if err := v.RemoveFromIndex(wikiSlug); err != nil {
				slog.Error("failed to remove old index entry for note", "path", nf.Path, "error", err)
				continue
			}
		}

		// Write wiki copy
		wikiPath, err := v.WriteWiki(wikiFilename, wikiContent)
		if err != nil {
			slog.Error("failed to write wiki copy for note", "path", nf.Path, "error", err)
			continue
		}

		// Sync wikilink edges incrementally
		syncEdgesFromContent(db, wikiSlug, wikiContent)

		// Auto-link: add backlinks to related pages
		autoLinkRelated(db, v, wikiSlug, tags, wikiContent)

		// Index the wiki slug (not the notes/ path)
		if err := v.UpdateIndex(wikiSlug, nf.Title, tags); err != nil {
			slog.Error("failed to update index for note", "path", nf.Path, "error", err)
			continue
		}

		if err := db.UpsertNote(nf.Path, nf.ContentHash, wikiPath); err != nil {
			slog.Error("failed to upsert note record", "path", nf.Path, "error", err)
			continue
		}

		if existingHash == "" {
			slog.Info("note indexed", "path", nf.Path, "wiki", wikiPath)
			indexed++
		} else {
			slog.Info("note updated", "path", nf.Path, "wiki", wikiPath)
			updated++
		}
	}

	// Reconcile deletions: remove DB records and wiki copies for notes no longer on disk
	if ctx.Err() != nil {
		return
	}
	dbNotes, err := db.ListNotes()
	if err != nil {
		slog.Error("failed to list notes from db", "error", err)
	} else {
		var deleted int
		for _, rec := range dbNotes {
			if _, exists := scannedPaths[rec.Path]; exists {
				continue
			}
			// Remove index entry by wiki slug
			wikiSlug := notes.TitleFromFilename(rec.Path)
			if err := v.RemoveFromIndex(wikiSlug); err != nil {
				slog.Error("failed to remove note from index", "path", rec.Path, "error", err)
				continue
			}
			// Delete wiki copy if it exists
			if rec.WikiPath != "" {
				if err := v.DeleteFile(rec.WikiPath); err != nil {
					slog.Error("failed to delete wiki copy for note", "path", rec.WikiPath, "error", err)
				}
			}
			if err := db.DeleteNote(rec.Path); err != nil {
				slog.Error("failed to delete note record", "path", rec.Path, "error", err)
			} else {
				slog.Info("note removed", "path", rec.Path)
				deleted++
			}
		}
		if deleted > 0 {
			slog.Info("reconciled deleted notes", "count", deleted)
		}
	}

	slog.Info("notes cycle complete", "indexed", indexed, "updated", updated, "skipped", skipped)
}

func buildNoteWikiContent(title string, tags []string, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", vault.QuoteYAMLValue(title))
	b.WriteString("tags:\n")
	for _, t := range tags {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	b.WriteString("type: note\n")
	b.WriteString("---\n\n")
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

func loadConfig(forceReEnrich, forceReFetch bool) (*config, error) {
	raindropToken := os.Getenv("RAINDROP_ACCESS_TOKEN")

	apiKey := os.Getenv("OLLAMA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OLLAMA_API_KEY is required")
	}

	vaultPath := os.Getenv("VAULT_PATH")
	if vaultPath == "" {
		return nil, fmt.Errorf("VAULT_PATH is required")
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "qwen3.5"
	}

	pollInterval := 5 * time.Minute
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parsing POLL_INTERVAL: %w", err)
		}
		pollInterval = d
	}

	enrichDelayMs := 500
	if v := os.Getenv("ENRICH_DELAY_MS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing ENRICH_DELAY_MS: %w", err)
		}
		enrichDelayMs = n
	}

	enrichRetries := 3
	if v := os.Getenv("ENRICH_MAX_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing ENRICH_MAX_RETRIES: %w", err)
		}
		enrichRetries = n
	}

	supadataAPIKey := os.Getenv("SUPADATA_API_KEY")

	hygieneInterval := 10
	if v := os.Getenv("HYGIENE_INTERVAL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing HYGIENE_INTERVAL: %w", err)
		}
		hygieneInterval = n
	}

	// MCP_HTTP_ADDR empty → in-process MCP server disabled (default).
	mcpHTTPAddr := os.Getenv("MCP_HTTP_ADDR")
	mcpAllowedHosts := parseAllowedHosts(os.LookupEnv("MCP_ALLOWED_HOST"))

	// MCP_READ_TOOLS off by default → the content-read MCP tools that duplicate
	// direct filesystem access are omitted, so filesystem-capable agents read
	// the vault natively. Enable for clients that reach the vault only over MCP.
	mcpReadTools := parseBoolEnv("MCP_READ_TOOLS")

	// AGENT_WEB_SEARCH_STRATEGY controls the AGENTS.md web-search guidance.
	// Unknown/empty falls back to the default; an unknown non-empty value warns.
	rawStrategy := os.Getenv("AGENT_WEB_SEARCH_STRATEGY")
	webSearchStrategy, ok := agentsmd.ParseWebSearchStrategy(rawStrategy)
	if !ok && rawStrategy != "" {
		slog.Warn("unknown AGENT_WEB_SEARCH_STRATEGY; falling back to default",
			"value", rawStrategy, "default", string(webSearchStrategy))
	}

	return &config{
		raindropToken:     raindropToken,
		ollamaAPIKey:      apiKey,
		ollamaModel:       model,
		vaultPath:         vaultPath,
		pollInterval:      pollInterval,
		enrichDelayMs:     enrichDelayMs,
		enrichRetries:     enrichRetries,
		supadataAPIKey:    supadataAPIKey,
		forceReEnrich:     forceReEnrich,
		forceReFetch:      forceReFetch,
		hygieneInterval:   hygieneInterval,
		mcpHTTPAddr:       mcpHTTPAddr,
		mcpAllowedHosts:   mcpAllowedHosts,
		mcpReadTools:      mcpReadTools,
		webSearchStrategy: webSearchStrategy,
	}, nil
}

// parseBoolEnv reads a boolean environment variable, returning false when the
// variable is unset, empty, or not parseable as a bool.
func parseBoolEnv(key string) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	return err == nil && v
}

// parseAllowedHosts resolves the MCP Host-guard allowlist from MCP_ALLOWED_HOST.
//   - unset (present=false)        → default localhost,127.0.0.1
//   - "*" or empty-after-trim      → nil (guard disabled; rely on network policy)
//   - comma-separated otherwise    → trimmed, non-empty entries
func parseAllowedHosts(value string, present bool) []string {
	if !present {
		return []string{"localhost", "127.0.0.1"}
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "*" {
		return nil
	}
	var hosts []string
	for _, h := range strings.Split(value, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// resolveContainerPath finds the actual mount point for a given host path by
// scanning /proc/self/mountinfo. On Docker Desktop for macOS, VAULT_PATH may
// be a host path like /Users/bas/git/.../test-vault while the volume is mounted
// at /vault — the mountinfo root field will be a suffix of the host path.
// Returns the host path unchanged if /proc/self/mountinfo is unavailable or no
// matching mount is found.
func resolveContainerPath(hostPath string) string {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return hostPath
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		// mountinfo: mountID parentID major:minor root mountPoint ...
		if len(fields) < 5 {
			continue
		}
		root, mountPoint := fields[3], fields[4]
		if root != "/" && strings.HasSuffix(hostPath, root) {
			return mountPoint
		}
	}
	return hostPath
}

// rebuildAllEdges performs a full scan of wiki/ and rebuilds all wikilink edges.
func rebuildAllEdges(db *store.Store, v *vault.Vault) error {
	slog.Info("rebuilding all wikilink edges")

	wikiFiles, err := v.ScanWikiDir()
	if err != nil {
		return fmt.Errorf("scanning wiki dir: %w", err)
	}

	var allEdges []store.Edge
	for _, relPath := range wikiFiles {
		content, err := v.ReadFile(relPath)
		if err != nil {
			slog.Warn("failed to read wiki file for edge rebuild", "path", relPath, "error", err)
			continue
		}
		slug := store.SlugFromWikiRelPath(relPath)
		links := mcpserver.ParseWikiLinks(content)
		for _, link := range links {
			allEdges = append(allEdges, store.Edge{
				FromSlug: slug,
				ToSlug:   link,
				Type:     "wikilink",
			})
		}
	}

	if err := db.RebuildEdges("wikilink", allEdges); err != nil {
		return fmt.Errorf("rebuilding edges: %w", err)
	}

	slog.Info("edge rebuild complete", "files_scanned", len(wikiFiles), "edges_created", len(allEdges))
	return nil
}

// syncEdgesFromContent parses wikilinks from content and upserts edges for the given slug.
func syncEdgesFromContent(db *store.Store, slug, content string) {
	links := mcpserver.ParseWikiLinks(content)
	var edges []store.Edge
	for _, link := range links {
		edges = append(edges, store.Edge{
			FromSlug: slug,
			ToSlug:   link,
			Type:     "wikilink",
		})
	}
	if err := db.UpsertEdgesFrom(slug, "wikilink", edges); err != nil {
		slog.Warn("failed to sync edges", "slug", slug, "error", err)
	}
}

// autoLinkRelated finds related pages by tag overlap and adds backlinks to them.
// It wraps file mutations in WithFileLock for cross-process safety.
func autoLinkRelated(db *store.Store, v *vault.Vault, slug string, tags []string, wikiContent string) {
	candidates, err := v.FindRelatedByTags(slug, tags)
	if err != nil {
		slog.Warn("failed to find related pages", "slug", slug, "error", err)
		return
	}

	for _, c := range candidates {
		line := c.BacklinkLine(slug)

		candidateWikiPath := filepath.Join("wiki", c.Slug+".md")
		err := db.WithFileLock(func() error {
			return v.UpdateRelatedSection(candidateWikiPath, []string{line})
		})
		if err != nil {
			slog.Warn("failed to update related section", "candidate", c.Slug, "error", err)
			continue
		}

		// Read updated candidate content and sync its edges
		updatedContent, err := v.ReadFile(candidateWikiPath)
		if err != nil {
			slog.Warn("failed to read updated candidate", "candidate", c.Slug, "error", err)
			continue
		}
		syncEdgesFromContent(db, c.Slug, updatedContent)

		slog.Info("auto-linked related page", "from", slug, "to", c.Slug, "shared_tags", c.SharedTags)
	}
}

func runMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	httpAddr := fs.String("http", "", "Serve Streamable HTTP on this address (e.g. :8080). Default: stdio transport.")
	_ = fs.Parse(args)

	vaultPath := os.Getenv("VAULT_PATH")
	if vaultPath == "" {
		vaultPath = "/vault"
	}
	dbPath := filepath.Join(vaultPath, ".lucidvault.db")
	mcpserver.Run(vaultPath, *httpAddr, dbPath, parseBoolEnv("MCP_READ_TOOLS"))
}

// runHygiene orchestrates all hygiene steps.
func runHygiene(db *store.Store, v *vault.Vault) {
	slog.Info("running hygiene cycle")

	// 1. Broken edges: remove edges where target file doesn't exist
	broken, err := db.FindBrokenEdges(func(slug string) bool {
		return v.FileHasContent("wiki/" + slug + ".md")
	})
	if err != nil {
		slog.Error("hygiene: failed to find broken edges", "error", err)
	} else {
		for _, edge := range broken {
			if err := db.DeleteEdge(edge.FromSlug, edge.ToSlug); err != nil {
				slog.Error("hygiene: failed to delete broken edge", "from", edge.FromSlug, "to", edge.ToSlug, "error", err)
			} else {
				slog.Info("hygiene: deleted broken edge", "from", edge.FromSlug, "to", edge.ToSlug)
			}
		}
	}

	// 2. Bidirectional index sync
	syncIndex(v)

	// 3. Raw/wiki consistency
	cleanRawWikiOrphans(v)

	// 4. Log orphan pages
	orphans, err := db.FindOrphans()
	if err != nil {
		slog.Error("hygiene: failed to find orphans", "error", err)
	} else {
		for _, slug := range orphans {
			slog.Warn("hygiene: orphan page (no inbound links)", "slug", slug)
		}
	}

	slog.Info("hygiene cycle complete")
}

// syncIndex performs bidirectional 3-direction sync between wiki files and index.md.
func syncIndex(v *vault.Vault) {
	// Build set of current index entries
	indexContent, err := v.ReadIndex()
	if err != nil {
		slog.Error("hygiene: failed to read index", "error", err)
		return
	}

	indexEntries := parseAllIndexEntries(indexContent)

	// Scan all wiki files on disk
	wikiFiles, err := v.ScanWikiDir()
	if err != nil {
		slog.Error("hygiene: failed to scan wiki dir", "error", err)
		return
	}

	diskSlugs := make(map[string]bool)
	for _, relPath := range wikiFiles {
		slug := store.SlugFromWikiRelPath(relPath)
		diskSlugs[slug] = true

		content, err := v.ReadFile(relPath)
		if err != nil {
			slog.Warn("hygiene: failed to read wiki file", "path", relPath, "error", err)
			continue
		}

		title := notes.ParseTitle(content)
		if title == "" {
			title = notes.ParseH1(content)
		}
		if title == "" {
			title = notes.TitleFromFilename(relPath)
		}
		tags := notes.ParseFrontmatter(content)

		existing, inIndex := indexEntries[slug]
		if !inIndex {
			// Direction 2: file exists, not in index → add
			if err := v.UpdateIndex(slug, title, tags); err != nil {
				slog.Error("hygiene: failed to add missing index entry", "slug", slug, "error", err)
			} else {
				slog.Info("hygiene: added missing index entry", "slug", slug)
			}
		} else if existing.title != title || !tagsEqual(existing.tags, tags) {
			// Direction 3: metadata drifted → update
			if err := v.RemoveFromIndex(slug); err != nil {
				slog.Error("hygiene: failed to remove drifted index entry", "slug", slug, "error", err)
				continue
			}
			if err := v.UpdateIndex(slug, title, tags); err != nil {
				slog.Error("hygiene: failed to update drifted index entry", "slug", slug, "error", err)
			} else {
				slog.Info("hygiene: synced drifted index entry", "slug", slug)
			}
		}
	}

	// Direction 1: index entry exists, file gone → remove
	for slug := range indexEntries {
		if !diskSlugs[slug] {
			if err := v.RemoveFromIndex(slug); err != nil {
				slog.Error("hygiene: failed to remove stale index entry", "slug", slug, "error", err)
			} else {
				slog.Info("hygiene: removed stale index entry", "slug", slug)
			}
		}
	}
}

// cleanRawWikiOrphans handles raw/wiki consistency (D14, D15).
func cleanRawWikiOrphans(v *vault.Vault) {
	// D14: raw file exists, no matching wiki → delete raw
	rawFiles, err := v.ScanRawDir()
	if err != nil {
		slog.Error("hygiene: failed to scan raw dir", "error", err)
		return
	}

	for _, rawPath := range rawFiles {
		slug := strings.TrimSuffix(filepath.Base(rawPath), ".md")
		if !v.FileHasContent("wiki/" + slug + ".md") {
			if err := v.DeleteFile(rawPath); err != nil {
				slog.Error("hygiene: failed to delete orphaned raw file", "path", rawPath, "error", err)
			} else {
				slog.Info("hygiene: deleted orphaned raw file", "slug", slug)
			}
		}
	}

	// D15: wiki exists, raw missing, footer has broken raw link → rewrite to URL
	wikiFiles, err := v.ScanWikiDir()
	if err != nil {
		slog.Error("hygiene: failed to scan wiki dir for footer check", "error", err)
		return
	}

	for _, wikiPath := range wikiFiles {
		slug := store.SlugFromWikiRelPath(wikiPath)
		rawPath := "raw/" + slug + ".md"
		if v.FileHasContent(rawPath) {
			continue
		}

		content, err := v.ReadFile(wikiPath)
		if err != nil {
			continue
		}

		if !vault.HasRawFooterLink(content, rawPath) {
			continue
		}

		url := vault.ParseFrontmatterURL(content)
		if url == "" {
			continue
		}

		if err := v.RewriteFooterLink(wikiPath, rawPath, url); err != nil {
			slog.Error("hygiene: failed to rewrite footer link", "path", wikiPath, "error", err)
		} else {
			slog.Info("hygiene: rewrote broken raw footer link", "slug", slug)
		}
	}
}

// generateAgentsMD generates AGENTS.md in the vault root with MCP tool docs and
// vault stats. readTools must match the in-process server's MCP_READ_TOOLS
// setting so the documented tools match the ones actually exposed.
func generateAgentsMD(db *store.Store, v *vault.Vault, readTools bool, strategy agentsmd.WebSearchStrategy) {
	stats, err := agentsmd.CollectStats(v, db)
	if err != nil {
		slog.Warn("failed to collect vault stats for AGENTS.md", "error", err)
		return
	}

	tools := mcpserver.RegisteredTools(readTools)
	content := agentsmd.Generate(tools, stats, strategy)

	written, err := agentsmd.WriteIfChanged(v.BasePath, content)
	if err != nil {
		slog.Warn("failed to write AGENTS.md", "error", err)
		return
	}
	if written {
		slog.Info("AGENTS.md updated")
	}
}

// hygiene index entry (local, avoids import cycle with autolink.go's indexEntry)
type hygieneIndexEntry struct {
	title string
	tags  []string
}

// parseAllIndexEntries parses all entries from index.md content into a map keyed by slug.
func parseAllIndexEntries(content string) map[string]hygieneIndexEntry {
	entries := make(map[string]hygieneIndexEntry)
	for _, line := range strings.Split(content, "\n") {
		// Parse: "- [[slug]] — Title [tag1, tag2]"
		if !strings.HasPrefix(line, "- [[") {
			continue
		}
		end := strings.Index(line, "]]")
		if end < 4 {
			continue
		}
		slug := line[4:end]

		// Extract title: after "]] — " up to " [" (tags bracket)
		rest := line[end+2:]
		title := ""
		tags := []string{}

		dashIdx := strings.Index(rest, " — ")
		if dashIdx >= 0 {
			afterDash := rest[dashIdx+len(" — "):]
			bracketIdx := strings.LastIndex(afterDash, " [")
			if bracketIdx >= 0 {
				title = afterDash[:bracketIdx]
				// Extract tags from [tag1, tag2]
				tagStr := afterDash[bracketIdx+2:]
				tagStr = strings.TrimSuffix(tagStr, "]")
				for _, t := range strings.Split(tagStr, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			} else {
				title = afterDash
			}
		}

		entries[slug] = hygieneIndexEntry{title: title, tags: tags}
	}
	return entries
}

// tagsEqual checks if two tag slices are equivalent (order-insensitive, duplicate-aware).
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, t := range a {
		counts[t]++
	}
	for _, t := range b {
		counts[t]--
		if counts[t] < 0 {
			return false
		}
	}
	return true
}
