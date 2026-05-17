package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lucidvault/internal/claudemd"
	"lucidvault/internal/enrich"
	"lucidvault/internal/notes"
	"lucidvault/internal/scraper"
	"lucidvault/internal/source"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"

	_ "lucidvault/internal/raindrop" // register raindrop source
)

type config struct {
	sourceName     string
	sourceToken    string
	ollamaAPIKey   string
	ollamaModel    string
	vaultPath      string
	pollInterval   time.Duration
	enrichDelayMs  int
	enrichRetries  int
	supadataAPIKey string
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
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

	// Initialize clients
	rd, err := source.NewClient(cfg.sourceName, cfg.sourceToken)
	if err != nil {
		slog.Error("failed to initialize source client", "error", err)
		os.Exit(1)
	}
	sc := scraper.New()
	if cfg.supadataAPIKey != "" {
		sc.SetYouTubeClient(scraper.NewYouTubeClient(cfg.supadataAPIKey))
		slog.Info("youtube transcript support enabled via supadata")
	}
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

	slog.Info("starting lucidvault", "poll_interval", cfg.pollInterval, "model", cfg.ollamaModel)

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
	processBookmarks(ctx, cfg, rd, sc, en, db, v)
	processNotes(ctx, db, v)
}

func processBookmarks(ctx context.Context, cfg *config, rd source.Client, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) {
	if ctx.Err() != nil {
		return
	}

	slog.Info("polling source")

	bookmarks, err := rd.FetchBookmarks(ctx)
	if err != nil {
		slog.Error("failed to fetch bookmarks", "error", err)
		return
	}

	slog.Info("fetched bookmarks", "count", len(bookmarks))

	// Build set of fetched source IDs upfront for deletion detection.
	fetchedIDs := make(map[int]struct{}, len(bookmarks))
	for _, bm := range bookmarks {
		fetchedIDs[bm.ID] = struct{}{}
	}

	var processed, failed, skipped int

	for _, bm := range bookmarks {
		if ctx.Err() != nil {
			slog.Info("shutdown requested, stopping processing")
			break
		}

		if err := processBookmark(ctx, cfg, bm, sc, en, db, v); err != nil {
			if errors.Is(err, errSkipped) {
				skipped++
			} else {
				slog.Error("failed to process bookmark", "title", bm.Title, "url", bm.Link, "error", err)
				failed++
			}
			continue
		}
		processed++
	}

	if failed > 0 {
		slog.Warn("some bookmarks failed — will be retried next cycle", "failed", failed)
	}

	// Reconcile deletions: remove vault files and DB records for bookmarks
	// no longer present in the source. Unlike processNotes (which scans the
	// local filesystem), this depends on an external API — an empty response
	// could be an API glitch, so we skip reconciliation when the source
	// returned zero bookmarks to avoid wiping the vault.
	if ctx.Err() != nil {
		return
	}
	if len(fetchedIDs) == 0 {
		slog.Warn("skipping deletion reconciliation — source returned zero bookmarks")
	} else {
		dbBookmarks, err := db.ListBookmarks()
		if err != nil {
			slog.Error("failed to list bookmarks from db", "error", err)
		} else {
			var deleted int
			for _, rec := range dbBookmarks {
				if ctx.Err() != nil {
					slog.Info("shutdown requested, stopping deletion reconciliation")
					break
				}
				if _, exists := fetchedIDs[rec.SourceID]; exists {
					continue
				}
				// Derive slug from wiki_path: "wiki/my-article.md" → "my-article"
				slug := strings.TrimSuffix(strings.TrimPrefix(rec.WikiPath, "wiki/"), ".md")
				if err := v.RemoveFromIndex(slug); err != nil {
					slog.Error("failed to remove bookmark from index", "source_id", rec.SourceID, "error", err)
					continue
				}
				cleanupOK := true
				if err := v.DeleteFile(rec.WikiPath); err != nil {
					slog.Error("failed to delete wiki file", "path", rec.WikiPath, "error", err)
					cleanupOK = false
				}
				if err := v.DeleteFile(rec.RawPath); err != nil {
					slog.Error("failed to delete raw file", "path", rec.RawPath, "error", err)
					cleanupOK = false
				}
				if !cleanupOK {
					continue // keep DB record for retry next cycle
				}
				if err := db.DeleteBySourceID(rec.SourceID); err != nil {
					slog.Error("failed to delete bookmark record", "source_id", rec.SourceID, "error", err)
				} else {
					slog.Info("bookmark deleted from vault", "source_id", rec.SourceID, "title", rec.Title)
					deleted++
				}
			}
			if deleted > 0 {
				slog.Info("reconciled deleted bookmarks", "count", deleted)
			}
		}
	}

	slog.Info("bookmarks cycle complete", "processed", processed, "failed", failed, "skipped", skipped)
}

func processNotes(ctx context.Context, db *store.Store, v *vault.Vault) {
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

		// Derive slug from path: "notes/sub/my-note.md" → "notes/sub/my-note"
		slug := strings.TrimSuffix(nf.Path, ".md")

		// Remove existing entry first so tags/title get refreshed on updates
		if existingHash != "" {
			if err := v.RemoveFromIndex(slug); err != nil {
				slog.Error("failed to remove old index entry for note", "path", nf.Path, "error", err)
				continue
			}
		}

		if err := v.UpdateIndex(slug, nf.Title, nf.Tags); err != nil {
			slog.Error("failed to update index for note", "path", nf.Path, "error", err)
			continue
		}

		if err := db.UpsertNote(nf.Path, nf.ContentHash); err != nil {
			slog.Error("failed to upsert note record", "path", nf.Path, "error", err)
			continue
		}

		if existingHash == "" {
			slog.Info("note indexed", "path", nf.Path)
			indexed++
		} else {
			slog.Info("note updated", "path", nf.Path)
			updated++
		}
	}

	// Reconcile deletions: remove DB records for notes no longer on disk
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
			slug := strings.TrimSuffix(rec.Path, ".md")
			if err := v.RemoveFromIndex(slug); err != nil {
				slog.Error("failed to remove note from index", "path", rec.Path, "error", err)
				continue
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

var errSkipped = fmt.Errorf("skipped")

func processBookmark(ctx context.Context, cfg *config, bm source.Bookmark, sc *scraper.Scraper, en *enrich.Client, db *store.Store, v *vault.Vault) error {
	// Dedup by source ID, with vault file reconciliation
	rec, err := db.GetBookmarkBySourceID(bm.ID)
	if err != nil {
		return fmt.Errorf("checking source_id: %w", err)
	}
	if rec != nil {
		if v.FileExists(rec.WikiPath) {
			slog.Debug("skipping already processed bookmark", "source_id", bm.ID)
			return errSkipped
		}
		// Wiki file missing or empty — remove stale DB record and re-process
		if err := db.DeleteBySourceID(bm.ID); err != nil {
			return fmt.Errorf("deleting stale record: %w", err)
		}
		slog.Info("reconciled missing vault file, re-processing", "source_id", bm.ID, "wiki_path", rec.WikiPath)
	}

	// Dedup by normalized URL
	normalizedURL := vault.NormalizeURL(bm.Link)
	urlExists, err := db.IsProcessedByURL(normalizedURL)
	if err != nil {
		return fmt.Errorf("checking url: %w", err)
	}
	if urlExists {
		slog.Debug("skipping duplicate URL", "url", bm.Link)
		return errSkipped
	}

	slug := vault.GenerateSlug(bm.Title)
	dateSaved := bm.Created.Format("2006-01-02")
	rawFilename := vault.GenerateRawFilename(dateSaved, slug)

	slog.Info("processing bookmark", "title", bm.Title, "url", bm.Link)

	// Scrape via Jina
	scrapeResult, err := sc.Scrape(ctx, bm.Link)
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

	wikiContent, err := en.Enrich(ctx, enrichInput)
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
	tags := notes.ParseFrontmatter(wikiContent)
	if len(tags) == 0 {
		tags = bm.Tags
	}

	// Update index
	title := bm.Title
	if enrichedTitle := notes.ParseTitle(wikiContent); enrichedTitle != "" {
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

	return &config{
		sourceName:     sourceName,
		sourceToken:    sourceToken,
		ollamaAPIKey:   apiKey,
		ollamaModel:    model,
		vaultPath:      vaultPath,
		pollInterval:   pollInterval,
		enrichDelayMs:  enrichDelayMs,
		enrichRetries:  enrichRetries,
		supadataAPIKey: supadataAPIKey,
	}, nil
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

