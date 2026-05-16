package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func (m *mockSource) FetchBookmarks(_ context.Context) ([]source.Bookmark, error) {
	return m.bookmarks, nil
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

func TestRunPollCycle_AllBookmarksProcessed(t *testing.T) {
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
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify all bookmarks were saved to DB
	for _, id := range []int{1, 2, 3} {
		exists, err := db.IsProcessedBySourceID(id)
		if err != nil {
			t.Fatalf("checking source_id %d: %v", id, err)
		}
		if !exists {
			t.Errorf("expected bookmark %d to be processed", id)
		}
	}
}

func TestRunPollCycle_DedupSkipsAlreadyProcessed(t *testing.T) {
	_, db, v, sc, en := setupTestEnv(t)

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
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	// First run: all succeed
	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	for _, id := range []int{30, 31, 32} {
		exists, err := db.IsProcessedBySourceID(id)
		if err != nil {
			t.Fatalf("checking source_id %d: %v", id, err)
		}
		if !exists {
			t.Fatalf("expected bookmark %d to be processed after first run", id)
		}
	}

	// Add new bookmarks; make raw/ read-only so new ones would fail if attempted
	t4 := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
	t5 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC)
	ms.bookmarks = append(ms.bookmarks,
		source.Bookmark{ID: 33, Title: "Fourth", Link: "http://example.com/fourth", Created: t4},
		source.Bookmark{ID: 34, Title: "Fifth", Link: "http://example.com/fifth", Created: t5},
	)

	// Second run: old bookmarks dedup-skipped, new ones processed
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// New bookmarks should also be processed
	for _, id := range []int{33, 34} {
		exists, err := db.IsProcessedBySourceID(id)
		if err != nil {
			t.Fatalf("checking source_id %d: %v", id, err)
		}
		if !exists {
			t.Errorf("expected bookmark %d to be processed after second run", id)
		}
	}
}

func TestRunPollCycle_ShutdownStopsProcessing(t *testing.T) {
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

	// Cancel context from the Ollama handler after enriching the first bookmark.
	// This means bookmark 1 processes fully, but the ctx.Err() check
	// before bookmark 2 triggers the break.
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
		// Cancel after first bookmark is fully enriched; the context check
		// at the top of the next loop iteration will catch this.
		cancel()
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
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify bookmark 1 was processed before shutdown
	exists, err := db.IsProcessedBySourceID(20)
	if err != nil {
		t.Fatalf("checking source_id: %v", err)
	}
	if !exists {
		t.Error("expected bookmark 20 to be processed before shutdown")
	}

	// Verify bookmark 2 was NOT processed (shutdown before it was reached)
	exists, err = db.IsProcessedBySourceID(21)
	if err != nil {
		t.Fatalf("checking source_id: %v", err)
	}
	if exists {
		t.Error("expected bookmark 21 to NOT be processed after shutdown")
	}
}

func TestRunPollCycle_MixedSuccessAndFailure(t *testing.T) {
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
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

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

func TestRunPollCycle_ReconcilesDeletedWikiFile(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 50, Title: "Reconcile Me", Link: "http://example.com/reconcile", Created: t1},
		},
	}

	cfg := &config{
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()

	// First run: process the bookmark
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	exists, err := db.IsProcessedBySourceID(50)
	if err != nil {
		t.Fatalf("checking source_id 50: %v", err)
	}
	if !exists {
		t.Fatal("expected bookmark 50 to be processed after first run")
	}

	// Delete the wiki file from the vault
	wikiFile := filepath.Join(tmpDir, "wiki", "reconcile-me.md")
	if err := os.Remove(wikiFile); err != nil {
		t.Fatalf("removing wiki file: %v", err)
	}

	// Second run: should reconcile and re-process
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify bookmark is still in DB (re-processed)
	exists, err = db.IsProcessedBySourceID(50)
	if err != nil {
		t.Fatalf("checking source_id 50 after reconciliation: %v", err)
	}
	if !exists {
		t.Error("expected bookmark 50 to be re-processed after reconciliation")
	}

	// Verify wiki file was re-created
	if _, err := os.Stat(wikiFile); os.IsNotExist(err) {
		t.Error("expected wiki file to be re-created after reconciliation")
	}
}

func TestRunPollCycle_ReconcilesEmptyWikiFile(t *testing.T) {
	tmpDir, db, v, sc, en := setupTestEnv(t)

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	ms := &mockSource{
		bookmarks: []source.Bookmark{
			{ID: 60, Title: "Empty Wiki", Link: "http://example.com/empty", Created: t1},
		},
	}

	cfg := &config{
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	ctx := context.Background()

	// First run: process the bookmark
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	exists, err := db.IsProcessedBySourceID(60)
	if err != nil {
		t.Fatalf("checking source_id 60: %v", err)
	}
	if !exists {
		t.Fatal("expected bookmark 60 to be processed after first run")
	}

	// Empty the wiki file
	wikiFile := filepath.Join(tmpDir, "wiki", "empty-wiki.md")
	if err := os.WriteFile(wikiFile, []byte(""), 0o644); err != nil {
		t.Fatalf("emptying wiki file: %v", err)
	}

	// Second run: should reconcile and re-process
	runPollCycle(ctx, cfg, ms, sc, en, db, v)

	// Verify wiki file was re-written with content
	data, err := os.ReadFile(wikiFile)
	if err != nil {
		t.Fatalf("reading wiki file: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected wiki file to have content after reconciliation")
	}
}

// errorSource is a mock source.Client that always returns an error.
type errorSource struct{}

func (e *errorSource) FetchBookmarks(_ context.Context) ([]source.Bookmark, error) {
	return nil, fmt.Errorf("api error")
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

	// Verify DB has the note record
	hash, err := db.GetNoteHash("notes/test-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected DB to have a hash for the note")
	}

	// Verify index.md contains the note link and tags
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

	// First run
	processNotes(ctx, db, v)

	hashAfterFirst, err := db.GetNoteHash("notes/unchanged-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash after first run: %v", err)
	}

	// Capture index content before second run
	indexPath := filepath.Join(tmpDir, "index.md")
	contentBefore := readFile(t, indexPath)

	// Second run
	processNotes(ctx, db, v)

	// Index content should be identical (no duplicate entries)
	contentAfter := readFile(t, indexPath)
	if contentBefore != contentAfter {
		t.Errorf("expected index.md to be unchanged on second run\nbefore: %q\nafter: %q", contentBefore, contentAfter)
	}

	// DB hash should be the same
	hashAfterSecond, err := db.GetNoteHash("notes/unchanged-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash after second run: %v", err)
	}
	if hashAfterFirst != hashAfterSecond {
		t.Errorf("expected DB hash to be unchanged: got %q then %q", hashAfterFirst, hashAfterSecond)
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

	hashV1, err := db.GetNoteHash("notes/changing-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash after v1: %v", err)
	}

	// Modify note: replace tag golang with rust
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

	// Verify index has rust but not golang
	indexPath := filepath.Join(tmpDir, "index.md")
	content := readFile(t, indexPath)
	assertContains(t, content, "rust")
	assertNotContains(t, content, "golang")

	// Verify DB hash changed
	hashV2, err := db.GetNoteHash("notes/changing-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash after v2: %v", err)
	}
	if hashV1 == hashV2 {
		t.Error("expected DB hash to change after note update")
	}
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

	// Verify note is indexed
	indexPath := filepath.Join(tmpDir, "index.md")
	assertContains(t, readFile(t, indexPath), "[[notes/deleted-note]]")

	// Delete the file
	if err := os.Remove(notePath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	processNotes(ctx, db, v)

	// Verify index no longer contains the note
	assertNotContains(t, readFile(t, indexPath), "[[notes/deleted-note]]")

	// Verify DB has no record
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

	// Verify DB has the note
	hash, err := db.GetNoteHash("notes/sub/deep-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected DB to have a hash for the nested note")
	}
}

func TestProcessNotes_BookmarkFailureDoesNotBlockNotes(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)

	noteContent := `---
tags:
  - independent
---

# Independent Note

Not affected by bookmark failures.
`
	notePath := filepath.Join(tmpDir, "notes", "independent-note.md")
	if err := os.WriteFile(notePath, []byte(noteContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &config{
		enrichDelayMs: 0,
		enrichRetries: 0,
	}

	// Use errorSource so bookmark phase always fails
	es := &errorSource{}

	// We need a scraper and enrich client — reuse stubs that won't be called
	sc := scraper.New()
	en := enrich.NewClient("test-key", "test-model", 0, 0)

	ctx := context.Background()
	runPollCycle(ctx, cfg, es, sc, en, db, v)

	// Notes should still be indexed despite bookmark failure
	indexPath := filepath.Join(tmpDir, "index.md")
	assertContains(t, readFile(t, indexPath), "[[notes/independent-note]]")

	hash, err := db.GetNoteHash("notes/independent-note.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected note to be indexed despite bookmark source error")
	}
}
