package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lucid/internal/enrich"
	"lucid/internal/scraper"
	"lucid/internal/source"
	"lucid/internal/store"
	"lucid/internal/vault"

	_ "lucid/internal/raindrop" // register raindrop source
)

type config struct {
	sourceName     string
	sourceToken    string
	ollamaAPIKey   string
	ollamaModel    string
	vaultPath      string
	pollInterval   time.Duration
	batchSize      int
	enrichDelayMs  int
	enrichRetries  int
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Initialize vault
	v := vault.New(cfg.vaultPath)
	if err := v.Init(); err != nil {
		slog.Error("failed to initialize vault", "error", err)
		os.Exit(1)
	}
	slog.Info("vault initialized", "path", cfg.vaultPath)

	// Initialize SQLite store
	dbPath := filepath.Join(cfg.vaultPath, ".lucid.db")
	db, err := store.New(dbPath)
	if err != nil {
		slog.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database initialized", "path", dbPath)

	// Initialize clients
	rd, err := source.NewClient(cfg.sourceName, cfg.sourceToken)
	if err != nil {
		slog.Error("failed to initialize source client", "error", err)
		os.Exit(1)
	}
	sc := scraper.New()
	en := enrich.NewClient(cfg.ollamaAPIKey, cfg.ollamaModel, cfg.enrichRetries, cfg.enrichDelayMs)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		slog.Info("shutdown signal received, finishing current item...", "signal", sig)
		cancel()
	}()

	slog.Info("starting lucid", "poll_interval", cfg.pollInterval, "model", cfg.ollamaModel)

	// Run immediately on startup, then on ticker
	runPollCycle(ctx, cfg, rd, sc, en, db, v)

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

func runPollCycle(ctx context.Context, cfg *config, rd source.Client, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	syncState, err := db.GetSyncState()
	if err != nil {
		slog.Error("failed to get sync state", "error", err)
		return
	}

	slog.Info("polling raindrop", "last_sync_at", syncState.LastSyncAt)

	bookmarks, err := rd.FetchBookmarks(syncState.LastSyncAt, cfg.batchSize)
	if err != nil {
		slog.Error("failed to fetch bookmarks", "error", err)
		return
	}

	if len(bookmarks) == 0 {
		slog.Info("no new bookmarks")
		return
	}

	slog.Info("fetched bookmarks", "count", len(bookmarks))

	var processed, failed, skipped int

	for _, bm := range bookmarks {
		if ctx.Err() != nil {
			slog.Info("shutdown requested, stopping processing")
			break
		}

		if err := processBookmark(ctx, cfg, bm, sc, en, db, v); err != nil {
			if err == errSkipped {
				skipped++
			} else {
				slog.Error("failed to process bookmark", "title", bm.Title, "url", bm.Link, "error", err)
				failed++
			}
			continue
		}
		processed++
	}

	// Update sync state to now
	if err := db.UpdateSyncState(0, time.Now()); err != nil {
		slog.Error("failed to update sync state", "error", err)
	}

	slog.Info("poll cycle complete", "processed", processed, "failed", failed, "skipped", skipped)
}

var errSkipped = fmt.Errorf("skipped")

func processBookmark(ctx context.Context, cfg *config, bm source.Bookmark, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) error {
	// Dedup by source ID
	exists, err := db.IsProcessedBySourceID(bm.ID)
	if err != nil {
		return fmt.Errorf("checking source_id: %w", err)
	}
	if exists {
		slog.Debug("skipping already processed bookmark", "source_id", bm.ID)
		return errSkipped
	}

	// Dedup by normalized URL
	normalizedURL := vault.NormalizeURL(bm.Link)
	exists, err = db.IsProcessedByURL(normalizedURL)
	if err != nil {
		return fmt.Errorf("checking url: %w", err)
	}
	if exists {
		slog.Debug("skipping duplicate URL", "url", bm.Link)
		return errSkipped
	}

	slug := vault.GenerateSlug(bm.Title)
	dateSaved := bm.Created.Format("2006-01-02")
	rawFilename := vault.GenerateRawFilename(dateSaved, slug)

	slog.Info("processing bookmark", "title", bm.Title, "url", bm.Link)

	// Scrape via Jina
	scrapeResult, err := sc.Scrape(bm.Link)
	var rawContent string
	if err != nil || !scrapeResult.OK {
		slog.Warn("scrape failed, using fallback", "url", bm.Link, "error", err)
		rawContent = buildFallbackContent(bm)
	} else {
		rawContent = scrapeResult.Content
	}

	// Write raw file
	rawFormatted := vault.FormatRawContent(bm.Title, bm.Link, dateSaved, bm.Tags, rawContent)
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
		UserTags:    bm.Tags,
		RawFilename: rawFilename,
		Title:       bm.Title,
		URL:         bm.Link,
		DateSaved:   dateSaved,
		Profile:     profile,
	}

	wikiContent, err := en.Enrich(enrichInput)
	if err != nil {
		return fmt.Errorf("enriching: %w", err)
	}

	// Write wiki file
	wikiFilename := slug + ".md"
	wikiPath, err := v.WriteWiki(wikiFilename, wikiContent)
	if err != nil {
		return fmt.Errorf("writing wiki file: %w", err)
	}

	// Extract tags from the enriched content for index
	tags := extractTags(wikiContent, bm.Tags)

	// Update index
	title := bm.Title
	if enrichedTitle := extractTitle(wikiContent); enrichedTitle != "" {
		title = enrichedTitle
	}
	if err := v.UpdateIndex(slug, title, tags); err != nil {
		return fmt.Errorf("updating index: %w", err)
	}

	// Save to database
	record := &store.BookmarkRecord{
		SourceID:      bm.ID,
		WikiPath:      wikiPath,
		RawPath:       rawPath,
		Title:         bm.Title,
		URL:           bm.Link,
		URLNormalized: normalizedURL,
		ProcessedAt:   time.Now(),
	}
	if err := db.SaveBookmark(record); err != nil {
		return fmt.Errorf("saving bookmark record: %w", err)
	}

	slog.Info("bookmark processed", "title", bm.Title, "wiki", wikiPath, "raw", rawPath)
	return nil
}

func buildFallbackContent(bm source.Bookmark) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", bm.Title)
	fmt.Fprintf(&b, "URL: %s\n\n", bm.Link)
	if bm.Excerpt != "" {
		fmt.Fprintf(&b, "%s\n\n", bm.Excerpt)
	}
	if len(bm.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(bm.Tags, ", "))
	}
	return b.String()
}

func extractTags(content string, fallbackTags []string) []string {
	// Try to extract tags from YAML frontmatter
	if !strings.HasPrefix(content, "---") {
		return fallbackTags
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return fallbackTags
	}

	frontmatter := parts[1]
	var tags []string
	inTags := false
	for _, line := range strings.Split(frontmatter, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "tags:" {
			inTags = true
			continue
		}
		if inTags {
			if strings.HasPrefix(trimmed, "- ") {
				tag := strings.TrimPrefix(trimmed, "- ")
				tags = append(tags, strings.TrimSpace(tag))
			} else {
				break
			}
		}
	}

	if len(tags) > 0 {
		return tags
	}
	return fallbackTags
}

func extractTitle(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return ""
	}

	for _, line := range strings.Split(parts[1], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "title:") {
			title := strings.TrimPrefix(trimmed, "title:")
			title = strings.TrimSpace(title)
			title = strings.Trim(title, `"'`)
			return title
		}
	}
	return ""
}

func loadConfig() (*config, error) {
	sourceName := os.Getenv("SOURCE_NAME")
	if sourceName == "" {
		sourceName = "raindrop"
	}

	sourceToken := os.Getenv("SOURCE_TOKEN")
	if sourceToken == "" {
		// Fall back to legacy env var
		sourceToken = os.Getenv("RAINDROP_ACCESS_TOKEN")
	}
	if sourceToken == "" {
		return nil, fmt.Errorf("SOURCE_TOKEN is required")
	}

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

	batchSize := 10
	if v := os.Getenv("BATCH_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing BATCH_SIZE: %w", err)
		}
		batchSize = n
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

	return &config{
		sourceName:    sourceName,
		sourceToken:   sourceToken,
		ollamaAPIKey:  apiKey,
		ollamaModel:   model,
		vaultPath:     vaultPath,
		pollInterval:  pollInterval,
		batchSize:     batchSize,
		enrichDelayMs: enrichDelayMs,
		enrichRetries: enrichRetries,
	}, nil
}
