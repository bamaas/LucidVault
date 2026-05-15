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

func TestRunPollCycle_SyncDoesNotAdvanceOnMidBatchShutdown(t *testing.T) {
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
	t.Cleanup(func() { db.Close() })

	// Cancel context from the Jina handler after serving the first scrape.
	// This means bookmark 1 processes fully, but the ctx.Err() check
	// before bookmark 2 triggers the break.
	ctx, cancel := context.WithCancel(context.Background())
	var scrapeCount int
	jinaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scrapeCount++
		if scrapeCount >= 2 {
			// Should not be reached — loop should break before second processBookmark
			t.Error("unexpected second scrape request after context cancellation")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Scraped Content\n\nSome content."))
		// Cancel after first bookmark is fully scraped; the context check
		// at the top of the next loop iteration will catch this.
		cancel()
	}))
	t.Cleanup(jinaServer.Close)

	sc := scraper.New()
	sc.SetBaseURL(jinaServer.URL + "/")

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

	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Bookmark 1 was processed, but context was cancelled before bookmark 2.
	// Sync state must NOT advance because not all bookmarks were attempted.
	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected sync state to remain at epoch, got %v", state.LastSyncAt)
	}

	// Verify bookmark 1 was actually processed (proving we tested the in-loop break)
	exists, err := db.IsProcessedBySourceID(20)
	if err != nil {
		t.Fatalf("checking source_id: %v", err)
	}
	if !exists {
		t.Error("expected bookmark 20 to be processed before shutdown")
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

func TestRunPollCycle_MixedSuccessAndFailureInSingleBatch(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 40, Title: "Success One", Link: "http://example.com/s1", Created: t1},
			{ID: 41, Title: "Will Fail", Link: "http://example.com/fail", Created: t2},
			{ID: 42, Title: "Success Two", Link: "http://example.com/s2", Created: t3},
		},
	}

	// Sabotage bookmark 2's wiki write by creating a directory where the
	// wiki file should be written. os.WriteFile will fail on a directory.
	wikiDir := filepath.Join(tmpDir, "wiki", "will-fail.md")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := &config{
		batchSize:     0,
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Sync must NOT advance — bookmark 2 failed even though 1 and 3 succeeded
	state, err := db.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}
	if !state.LastSyncAt.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected sync state to remain at epoch, got %v", state.LastSyncAt)
	}

	// Verify bookmark 1 and 3 were processed despite bookmark 2 failing
	exists, err := db.IsProcessedBySourceID(40)
	if err != nil {
		t.Fatalf("checking source_id 40: %v", err)
	}
	if !exists {
		t.Error("expected bookmark 40 to be processed")
	}

	exists, err = db.IsProcessedBySourceID(42)
	if err != nil {
		t.Fatalf("checking source_id 42: %v", err)
	}
	if !exists {
		t.Error("expected bookmark 42 to be processed")
	}

	// Verify bookmark 2 was NOT recorded
	exists, err = db.IsProcessedBySourceID(41)
	if err != nil {
		t.Fatalf("checking source_id 41: %v", err)
	}
	if exists {
		t.Error("expected bookmark 41 to NOT be processed")
	}
}
