package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"lucidvault/internal/enrich"
	"lucidvault/internal/scraper"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// validWikiResponse returns a minimal response that passes validateResponse.
const validWikiResponse = `---
title: "Test Article"
source: "http://example.com/test"
date_saved: 2024-01-01
tags:
  - test
type: bookmark
---

# Test Article

## Summary
This is a test summary.

## Key Takeaways
- Test takeaway
`

func setupTestEnv(t *testing.T) (string, *store.Store, *vault.Vault, *scraper.Scraper, *enrich.Client) {
	t.Helper()

	tmpDir := t.TempDir()

	// Vault
	v := vault.New(tmpDir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	// SQLite store
	dbPath := filepath.Join(tmpDir, ".lucidvault.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Test HTTP server for Jina scraper
	jinaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("# Scraped Content\n\nSome content here.")); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	t.Cleanup(jinaServer.Close)

	sc := scraper.New()
	sc.SetBaseURL(jinaServer.URL + "/")

	// Test HTTP server for Ollama enricher
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{}
		resp.Message.Content = validWikiResponse
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("json.Encode: %v", err)
		}
	}))
	t.Cleanup(ollamaServer.Close)

	en := enrich.NewClient("test-key", "test-model", 0, 0)
	en.SetBaseURL(ollamaServer.URL)

	return tmpDir, db, v, sc, en
}

// writeInboxFile creates a .md file in the inbox directory.
func writeInboxFile(t *testing.T, vaultPath, filename, content string) {
	t.Helper()
	inboxDir := filepath.Join(vaultPath, "inbox")
	if err := os.WriteFile(filepath.Join(inboxDir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("writing inbox file: %v", err)
	}
}

func TestProcessInbox_ProcessesItems(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	writeInboxFile(t, tmpDir, "article-one.md", "https://example.com/1\n")
	writeInboxFile(t, tmpDir, "article-two.md", "https://example.com/2\n")

	ctx := context.Background()
	processInbox(ctx, sc, en, db, v)

	// Verify both URLs are in DB
	for _, url := range []string{"https://example.com/1", "https://example.com/2"} {
		exists, err := db.IsProcessedByURL(vault.NormalizeURL(url))
		if err != nil {
			t.Fatalf("checking url %s: %v", url, err)
		}
		if !exists {
			t.Errorf("expected URL %s to be processed", url)
		}
	}

	// Verify inbox files are deleted
	entries, _ := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if len(entries) != 0 {
		t.Errorf("expected inbox to be empty, got %d files", len(entries))
	}

	// Verify wiki files exist
	if _, err := os.Stat(filepath.Join(tmpDir, "wiki", "article-one.md")); os.IsNotExist(err) {
		t.Error("expected wiki file article-one.md to exist")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "wiki", "article-two.md")); os.IsNotExist(err) {
		t.Error("expected wiki file article-two.md to exist")
	}

	// Verify raw files exist (without date prefix)
	if _, err := os.Stat(filepath.Join(tmpDir, "raw", "article-one.md")); os.IsNotExist(err) {
		t.Error("expected raw file article-one.md to exist")
	}
}

func TestProcessInbox_WithFrontmatter(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	content := `---
title: "My Custom Title"
tags:
  - golang
  - testing
---
https://example.com/custom
`
	writeInboxFile(t, tmpDir, "custom.md", content)

	ctx := context.Background()
	processInbox(ctx, sc, en, db, v)

	// Verify it was processed
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/custom"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if !exists {
		t.Error("expected custom URL to be processed")
	}

	// Verify inbox file deleted
	entries, _ := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if len(entries) != 0 {
		t.Errorf("expected inbox to be empty, got %d files", len(entries))
	}
}

func TestProcessInbox_FailedItemKeepsFile(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	writeInboxFile(t, tmpDir, "will-fail.md", "https://example.com/fail\n")

	// Sabotage wiki write by creating a directory where the file should go
	wikiDir := filepath.Join(tmpDir, "wiki", "will-fail.md")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx := context.Background()
	processInbox(ctx, sc, en, db, v)

	// Verify inbox file still exists (for retry)
	entries, _ := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if len(entries) != 1 {
		t.Errorf("expected inbox file to remain for retry, got %d files", len(entries))
	}

	// Verify URL is NOT in DB
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/fail"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if exists {
		t.Error("expected failed URL to NOT be in DB")
	}
}

func TestProcessInbox_ReprocessingOverwrites(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	// First processing
	writeInboxFile(t, tmpDir, "reprocess.md", "https://example.com/reprocess\n")

	ctx := context.Background()
	processInbox(ctx, sc, en, db, v)

	// Verify processed
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/reprocess"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if !exists {
		t.Fatal("expected URL to be processed after first run")
	}

	// Drop the same URL again (user wants reprocessing)
	writeInboxFile(t, tmpDir, "reprocess.md", "https://example.com/reprocess\n")

	processInbox(ctx, sc, en, db, v)

	// Verify still processed and inbox file deleted
	exists, err = db.IsProcessedByURL(vault.NormalizeURL("https://example.com/reprocess"))
	if err != nil {
		t.Fatalf("checking url after reprocess: %v", err)
	}
	if !exists {
		t.Error("expected URL to still be in DB after reprocessing")
	}

	entries, _ := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if len(entries) != 0 {
		t.Errorf("expected inbox to be empty after reprocessing, got %d files", len(entries))
	}
}

func TestProcessInbox_ShutdownStopsProcessing(t *testing.T) {
	tmpDir := t.TempDir()

	v := vault.New(tmpDir)
	if err := v.Init(); err != nil {
		t.Fatalf("vault init: %v", err)
	}

	dbPath := filepath.Join(tmpDir, ".lucidvault.db")
	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Cancel context from the Ollama handler after enriching the first item.
	ctx, cancel := context.WithCancel(context.Background())
	jinaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("# Scraped Content\n\nSome content.")); err != nil {
			t.Errorf("w.Write: %v", err)
		}
	}))
	t.Cleanup(jinaServer.Close)

	sc := scraper.New()
	sc.SetBaseURL(jinaServer.URL + "/")

	var ollamaCalls atomic.Int32
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := ollamaCalls.Add(1)
		if n > 1 {
			cancel()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{}
		resp.Message.Content = validWikiResponse
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("json.Encode: %v", err)
		}
	}))
	t.Cleanup(ollamaServer.Close)

	en := enrich.NewClient("test-key", "test-model", 0, 0)
	en.SetBaseURL(ollamaServer.URL)

	writeInboxFile(t, tmpDir, "article-a.md", "https://example.com/a\n")
	writeInboxFile(t, tmpDir, "article-b.md", "https://example.com/b\n")

	processInbox(ctx, sc, en, db, v)

	// First item should be processed
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/a"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if !exists {
		t.Error("expected first URL to be processed before shutdown")
	}

	// Second item's inbox file should still exist
	inboxFiles, _ := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if len(inboxFiles) != 1 {
		t.Errorf("expected 1 inbox file remaining, got %d", len(inboxFiles))
	}
}

func TestRunPollCycle_InboxAndNotes(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	// Create inbox file
	writeInboxFile(t, tmpDir, "test-article.md", "https://example.com/test\n")

	// Create note
	noteContent := `---
tags:
  - golang
---

# My Note

Some content.
`
	notePath := filepath.Join(tmpDir, "notes", "test-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &config{}
	ctx := context.Background()
	runPollCycle(ctx, cfg, nil, sc, en, db, v)

	// Verify bookmark processed
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/test"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if !exists {
		t.Error("expected inbox item to be processed")
	}

	// Verify note indexed
	hash, err := db.GetNoteHash("notes/test-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected note to be indexed")
	}

	// Verify index has both
	indexPath := filepath.Join(tmpDir, "index.md")
	content := readFile(t, indexPath)
	assertContains(t, content, "[[test-article]]")
	assertContains(t, content, "[[notes/test-note]]")
}

func TestReEnrichAll_UpdatesWikiContent(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	// First: process an inbox item to create raw + wiki + DB record
	writeInboxFile(t, tmpDir, "enrich-test.md", "https://example.com/enrich\n")
	ctx := context.Background()
	processInbox(ctx, sc, en, db, v)

	// Verify wiki exists
	wikiPath := filepath.Join(tmpDir, "wiki", "enrich-test.md")
	if _, err := os.Stat(wikiPath); os.IsNotExist(err) {
		t.Fatal("expected wiki file to exist after initial processing")
	}

	// Re-enrich — should overwrite wiki
	reEnrichAll(ctx, en, db, v)

	// Wiki file should still exist (overwritten)
	if _, err := os.Stat(wikiPath); os.IsNotExist(err) {
		t.Error("expected wiki file to exist after re-enrichment")
	}

	// DB record should still exist
	exists, err := db.IsProcessedByURL(vault.NormalizeURL("https://example.com/enrich"))
	if err != nil {
		t.Fatalf("checking url: %v", err)
	}
	if !exists {
		t.Error("expected URL to still be in DB after re-enrichment")
	}
}

func TestReEnrichBookmark_MissingRawPath(t *testing.T) {
	_, db, v, _, _ := setupTestEnv(t)

	// Insert a bookmark with no raw path
	rec := store.BookmarkRecord{
		WikiPath:      "wiki/no-raw.md",
		RawPath:       "",
		Title:         "No Raw",
		URL:           "http://example.com/no-raw",
		URLNormalized: "http://example.com/no-raw",
	}

	ctx := context.Background()
	en := enrich.NewClient("test-key", "test-model", 0, 0)

	err := reEnrichBookmark(ctx, rec, en, db, v)
	if err == nil {
		t.Error("expected error for bookmark with empty raw path")
	}
}

func TestProcessNotes_IndexesNewNote(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	noteContent := `---
tags:
  - golang
  - testing
---

# My Test Note

Some content here.
`
	notePath := filepath.Join(tmpDir, "notes", "test-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	processNotes(ctx, db, v)

	hash, err := db.GetNoteHash("notes/test-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected DB to have a hash for the note")
	}

	indexPath := filepath.Join(tmpDir, "index.md")
	content := readFile(t, indexPath)
	assertContains(t, content, "[[notes/test-note]]")
	assertContains(t, content, "golang")
	assertContains(t, content, "testing")
}

func TestProcessNotes_SkipsUnchanged(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	noteContent := `---
tags:
  - golang
---

# Unchanged Note

Content here.
`
	notePath := filepath.Join(tmpDir, "notes", "unchanged-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	processNotes(ctx, db, v)

	indexPath := filepath.Join(tmpDir, "index.md")
	contentBefore := readFile(t, indexPath)

	processNotes(ctx, db, v)

	contentAfter := readFile(t, indexPath)
	if contentBefore != contentAfter {
		t.Errorf("expected index.md to be unchanged on second run")
	}
}

func TestProcessNotes_UpdatesChangedNote(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	noteV1 := `---
tags:
  - golang
---

# My Note

Version one.
`
	notePath := filepath.Join(tmpDir, "notes", "changing-note.md")
	if err := os.WriteFile(notePath, []byte(noteV1), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}

	ctx := context.Background()
	processNotes(ctx, db, v)

	noteV2 := `---
tags:
  - rust
---

# My Note

Version two.
`
	if err := os.WriteFile(notePath, []byte(noteV2), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}

	processNotes(ctx, db, v)

	indexPath := filepath.Join(tmpDir, "index.md")
	content := readFile(t, indexPath)
	assertContains(t, content, "rust")
	assertNotContains(t, content, "golang")
}

func TestProcessNotes_ReconcilesDeletedNote(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	noteContent := `---
tags:
  - golang
---

# To Be Deleted

Some content.
`
	notePath := filepath.Join(tmpDir, "notes", "deleted-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	processNotes(ctx, db, v)

	indexPath := filepath.Join(tmpDir, "index.md")
	assertContains(t, readFile(t, indexPath), "[[notes/deleted-note]]")

	if err := os.Remove(notePath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	processNotes(ctx, db, v)

	assertNotContains(t, readFile(t, indexPath), "[[notes/deleted-note]]")

	hash, err := db.GetNoteHash("notes/deleted-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash after delete: %v", err)
	}
	if hash != "" {
		t.Errorf("expected DB hash to be empty after deletion, got %q", hash)
	}
}

func TestProcessNotes_RecursiveSubdirectories(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	subDir := filepath.Join(tmpDir, "notes", "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	noteContent := `---
tags:
  - deep
---

# Deep Note

Nested content.
`
	notePath := filepath.Join(subDir, "deep-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	processNotes(ctx, db, v)

	indexPath := filepath.Join(tmpDir, "index.md")
	assertContains(t, readFile(t, indexPath), "[[notes/sub/deep-note]]")
}
