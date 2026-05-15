package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lucidvault/internal/enrich"
	"lucidvault/internal/scraper"
	"lucidvault/internal/source"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// mockSource implements source.Client for testing.
type mockSource struct {
	bookmarks []source.Bookmark
}

func (m *mockSource) FetchBookmarks(lastSyncAt time.Time, batchSize int) ([]source.Bookmark, error) {
	var result []source.Bookmark
	for _, bm := range m.bookmarks {
		if bm.Created.After(lastSyncAt) {
			result = append(result, bm)
			if batchSize > 0 && len(result) >= batchSize {
				break
			}
		}
	}
	return result, nil
}

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
	t.Cleanup(func() { db.Close() })

	// Test HTTP server for Jina scraper
	jinaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Scraped Content\n\nSome content here."))
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
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ollamaServer.Close)

	en := enrich.NewClient("test-key", "test-model", 0, 0)
	en.SetBaseURL(ollamaServer.URL)

	return tmpDir, db, v, sc, en
}

func TestRunPollCycle_SyncAdvancesOnAllSuccess(t *testing.T) {
	_, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 1, Title: "Article One", Link: "http://example.com/1", Created: t1},
			{ID: 2, Title: "Article Two", Link: "http://example.com/2", Created: t2},
			{ID: 3, Title: "Article Three", Link: "http://example.com/3", Created: t3},
		},
	}

	cfg := &config{
		batchSize:     0,
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify sync state advanced to newest bookmark
	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(t3) {
		t.Errorf("expected sync at %v, got %v", t3, state.LastSyncAt)
	}
}

func TestRunPollCycle_SyncDoesNotAdvanceOnFailure(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 10, Title: "Good Article", Link: "http://example.com/good", Created: t1},
			{ID: 11, Title: "Bad Article", Link: "http://example.com/bad", Created: t2},
		},
	}

	// Make raw/ dir read-only so WriteRaw fails
	rawDir := filepath.Join(tmpDir, "raw")
	if err := os.Chmod(rawDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(rawDir, 0o755) })

	cfg := &config{
		batchSize:     0,
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify sync state did NOT advance (still at epoch)
	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected sync state to remain at epoch, got %v", state.LastSyncAt)
	}
}

func TestRunPollCycle_SyncDoesNotAdvanceOnShutdown(t *testing.T) {
	_, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 20, Title: "Article A", Link: "http://example.com/a", Created: t1},
			{ID: 21, Title: "Article B", Link: "http://example.com/b", Created: t2},
		},
	}

	cfg := &config{
		batchSize:     0,
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	// Cancel context immediately so processing loop breaks
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify sync state did NOT advance
	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected sync state to remain at epoch, got %v", state.LastSyncAt)
	}
}

func TestRunPollCycle_PartialFailureDoesNotAdvance(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 30, Title: "First", Link: "http://example.com/first", Created: t1},
			{ID: 31, Title: "Second", Link: "http://example.com/second", Created: t2},
			{ID: 32, Title: "Third", Link: "http://example.com/third", Created: t3},
		},
	}

	cfg := &config{
		batchSize:     0,
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	// First run: all succeed (raw/ is writable)
	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(t3) {
		t.Fatalf("expected sync to advance to t3 after success, got %v", state.LastSyncAt)
	}

	// Now add more bookmarks, but make raw/ read-only so new ones fail
	t4 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
	t5 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)
	ms.bookmarks = append(ms.bookmarks,
		source.Bookmark{ID: 33, Title: "Fourth", Link: "http://example.com/fourth", Created: t4},
		source.Bookmark{ID: 34, Title: "Fifth", Link: "http://example.com/fifth", Created: t5},
	)

	rawDir := filepath.Join(tmpDir, "raw")
	if err := os.Chmod(rawDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(rawDir, 0o755) })

	// Second run: new bookmarks fail at WriteRaw
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Sync state should still be at t3, not advanced to t5
	state, err = db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(t3) {
		t.Errorf("expected sync to stay at t3 after failure, got %v", state.LastSyncAt)
	}
}
