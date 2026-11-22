package main

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lucidvault/internal/enrich"
	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

// Pipeline behaviour when the Enrichment Backend is unusable (ADR-031,
// docs/plans/plan-openai-compatible-enrichment.md section 2): the cycle stops,
// inbox files stay, note hashes stay, existing wiki pages stay.

// fakeBackend is an OpenAI-compatible endpoint whose failure status can be
// switched on and off between pipeline runs.
type fakeBackend struct {
	srv    *httptest.Server
	calls  atomic.Int32
	status atomic.Int32 // 0 means healthy (200 with reply)
}

func newFakeBackend(t *testing.T, reply string) *fakeBackend {
	t.Helper()
	fb := &fakeBackend{}
	fb.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fb.calls.Add(1)
		if s := int(fb.status.Load()); s != 0 {
			w.WriteHeader(s)
			_, _ = w.Write([]byte(`{"error":"backend unusable"}`))
			return
		}
		writeChatCompletion(t, w, reply)
	}))
	t.Cleanup(fb.srv.Close)
	return fb
}

// client returns an enrich client for this backend. Retries are 0 so a run
// against current behaviour stays fast; the call count still shows whether
// the loop moved on to the next item.
func (fb *fakeBackend) client() *enrich.Client {
	en := enrich.NewClient("", "test-model", 0, 0)
	en.SetBaseURL(fb.srv.URL)
	return en
}

// newDeadConnBackend returns a client whose backend fails every connection
// attempt (a TLS handshake failure — the client does not trust the test
// server's CA), plus a counter of connection attempts. Unlike a plain
// connection-refused server (nothing listening), this one is actually
// running, so ConnState can prove how many times the client tried to
// connect — which is what shows a stop-cycle break really happened instead
// of every item simply landing in failedPaths (see
// internal/enrich/backend_test.go TestClient_BackendUnavailableTLSError).
// apiKey is configured on the returned client so callers can assert it never
// leaks into logs for this (connection-error) path too.
func newDeadConnBackend(t *testing.T, apiKey string) (*enrich.Client, *atomic.Int32) {
	t.Helper()
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	en := enrich.NewClient(apiKey, "test-model", 0, 0)
	en.SetBaseURL(srv.URL)
	return en, &conns
}

var waitingInboxItems = []struct {
	file string
	url  string
}{
	{file: "item-a.md", url: "https://example.com/a"},
	{file: "item-b.md", url: "https://example.com/b"},
	{file: "item-c.md", url: "https://example.com/c"},
}

func assertInboxItemsWaiting(t *testing.T, vaultPath string, db *store.Store) {
	t.Helper()
	for _, it := range waitingInboxItems {
		data, err := os.ReadFile(filepath.Join(vaultPath, "inbox", it.file))
		if err != nil {
			t.Errorf("inbox/%s must stay for the next cycle: %v", it.file, err)
		} else if string(data) != it.url+"\n" {
			t.Errorf("inbox/%s content changed: %q", it.file, data)
		}

		if _, err := os.Stat(filepath.Join(vaultPath, "wiki", it.file)); !os.IsNotExist(err) {
			t.Errorf("wiki/%s must not be written while the backend is unavailable", it.file)
		}

		processed, err := db.IsProcessedByURL(vault.NormalizeURL(it.url))
		if err != nil {
			t.Fatalf("IsProcessedByURL(%s): %v", it.url, err)
		}
		if processed {
			t.Errorf("%s must not be marked processed", it.url)
		}
	}
}

// backendUnavailableTestAPIKey is a real-looking API key configured on the
// client in TestProcessInbox_BackendUnavailableStopsCycleAndKeepsInbox and
// TestProcessInbox_BackendConnectionErrorStopsCycleAndKeepsInbox so that the
// "logs never leak the key" assertion is meaningful for both the
// HTTP-status-based and connection-error backend-unavailable paths.
const backendUnavailableTestAPIKey = "sk-should-never-appear-in-logs-9001"

func TestProcessInbox_BackendUnavailableStopsCycleAndKeepsInbox(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 bad key", status: http.StatusUnauthorized},
		{name: "402 no credits", status: http.StatusPaymentRequired},
		{name: "403 forbidden", status: http.StatusForbidden},
		{name: "404 wrong model", status: http.StatusNotFound},
		{name: "410 gone", status: http.StatusGone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, db, v, sc, _ := setupTestEnv(t)
			fb := newFakeBackend(t, validWikiResponse)
			fb.status.Store(int32(tt.status))

			en := enrich.NewClient(backendUnavailableTestAPIKey, "test-model", 0, 0)
			en.SetBaseURL(fb.srv.URL)

			for _, it := range waitingInboxItems {
				writeInboxFile(t, tmpDir, it.file, it.url+"\n")
			}

			logs := captureLogs(t, slog.LevelInfo)
			processInbox(context.Background(), sc, en, db, v)

			if got := fb.calls.Load(); got != 1 {
				t.Errorf("expected the cycle to stop after 1 backend call, got %d", got)
			}
			assertInboxItemsWaiting(t, tmpDir, db)

			// Plan §2: "one attempt, one ERROR" naming the endpoint and the
			// status — never the API key.
			logged := logs.String()
			if got := strings.Count(logged, "level=ERROR"); got != 1 {
				t.Errorf("expected exactly 1 ERROR log line, got %d:\n%s", got, logged)
			}
			if !strings.Contains(logged, fb.srv.URL) {
				t.Errorf("expected the ERROR log to name the endpoint %q:\n%s", fb.srv.URL, logged)
			}
			// Match the exact wording client.go's callAPI produces
			// ("enrichment backend returned %d: ...") rather than searching
			// the whole log buffer for the bare status digits, which can
			// spuriously match a test-server port (127.0.0.1:40412 contains
			// "404") or a log timestamp's milliseconds/offset.
			wantStatusWording := "enrichment backend returned " + strconv.Itoa(tt.status)
			if !strings.Contains(logged, wantStatusWording) {
				t.Errorf("expected the ERROR log to include %q:\n%s", wantStatusWording, logged)
			}
			if strings.Contains(logged, backendUnavailableTestAPIKey) {
				t.Errorf("log output leaks the API key:\n%s", logged)
			}
			if strings.Contains(logged, "inbox empty, nothing to process") {
				t.Errorf("misleading \"inbox empty\" message logged despite items remaining:\n%s", logged)
			}
		})
	}
}

// TestProcessInbox_BackendConnectionErrorStopsCycleAndKeepsInbox exercises a
// TLS handshake failure via newDeadConnBackend (not an actually-refused
// connection — nothing listens at all in that case, which would make
// ConnState unable to count attempts; see newDeadConnBackend's doc comment).
func TestProcessInbox_BackendConnectionErrorStopsCycleAndKeepsInbox(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	en, conns := newDeadConnBackend(t, backendUnavailableTestAPIKey)

	for _, it := range waitingInboxItems {
		writeInboxFile(t, tmpDir, it.file, it.url+"\n")
	}

	logs := captureLogs(t, slog.LevelInfo)
	processInbox(context.Background(), sc, en, db, v)

	if got := conns.Load(); got != 1 {
		t.Errorf("expected exactly 1 connection attempt despite %d inbox items, got %d (the cycle must stop, not retry every item)",
			len(waitingInboxItems), got)
	}
	assertInboxItemsWaiting(t, tmpDir, db)

	// Plan §2: "one attempt, one ERROR" naming the endpoint — never the API
	// key — for the connection-error path too, not just the HTTP-status one.
	logged := logs.String()
	wantEndpointWording := "enrichment backend unreachable at " + en.BaseURL()
	if !strings.Contains(logged, wantEndpointWording) {
		t.Errorf("expected the ERROR log to include %q:\n%s", wantEndpointWording, logged)
	}
	if strings.Contains(logged, backendUnavailableTestAPIKey) {
		t.Errorf("log output leaks the API key:\n%s", logged)
	}
	if strings.Contains(logged, "inbox empty, nothing to process") {
		t.Errorf("misleading \"inbox empty\" message logged despite items remaining:\n%s", logged)
	}
}

func TestProcessInbox_WaitingItemsProcessedOnceBackendRecovers(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	for _, it := range waitingInboxItems {
		writeInboxFile(t, tmpDir, it.file, it.url+"\n")
	}

	fb.status.Store(http.StatusPaymentRequired)
	processInbox(context.Background(), sc, en, db, v)
	assertInboxItemsWaiting(t, tmpDir, db)

	fb.status.Store(0) // credits added
	processInbox(context.Background(), sc, en, db, v)

	for _, it := range waitingInboxItems {
		processed, err := db.IsProcessedByURL(vault.NormalizeURL(it.url))
		if err != nil {
			t.Fatalf("IsProcessedByURL(%s): %v", it.url, err)
		}
		if !processed {
			t.Errorf("%s should be processed after the backend recovers", it.url)
		}
		wiki := readFile(t, filepath.Join(tmpDir, "wiki", it.file))
		assertNotContains(t, wiki, "Enrichment failed")
	}
	entries, err := os.ReadDir(filepath.Join(tmpDir, "inbox"))
	if err != nil {
		t.Fatalf("ReadDir inbox: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected inbox to be empty after recovery, got %d files", len(entries))
	}
}

func TestProcessNotes_BackendUnavailableLeavesHashUnchanged(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)
	fb := newFakeBackend(t, "golang, testing")
	en := fb.client()

	// Tagless notes trigger Tag Suggestion.
	notePath := filepath.Join(tmpDir, "notes", "tagless.md")
	if err := os.WriteFile(notePath, []byte("# Tagless\n\nVersion one."), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}

	processNotes(context.Background(), en, db, v)

	hashBefore, err := db.GetNoteHash("notes/tagless.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hashBefore == "" {
		t.Fatal("precondition: note should be hashed while the backend is healthy")
	}

	if err := os.WriteFile(notePath, []byte("# Tagless\n\nVersion two."), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	fb.status.Store(http.StatusPaymentRequired)

	processNotes(context.Background(), en, db, v)

	hashAfter, err := db.GetNoteHash("notes/tagless.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hashAfter != hashBefore {
		t.Errorf("note hash changed from %q to %q while the backend was unavailable; the note must be retried next cycle",
			hashBefore, hashAfter)
	}
}

func TestProcessNotes_BackendUnavailableStopsCycle(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)
	fb := newFakeBackend(t, "golang, testing")
	fb.status.Store(http.StatusPaymentRequired)

	noteFiles := []string{"first.md", "second.md", "third.md"}
	for _, name := range noteFiles {
		content := "# " + name + "\n\nNo tags here."
		if err := os.WriteFile(filepath.Join(tmpDir, "notes", name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	processNotes(context.Background(), fb.client(), db, v)

	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected the cycle to stop after 1 backend call, got %d", got)
	}
	for _, name := range noteFiles {
		hash, err := db.GetNoteHash("notes/" + name)
		if err != nil {
			t.Fatalf("GetNoteHash(%s): %v", name, err)
		}
		if hash != "" {
			t.Errorf("notes/%s must stay unhashed while the backend is unavailable, got %q", name, hash)
		}
	}
}

// TestProcessNotes_BackendConnectionErrorStopsCycle is the connection-error
// counterpart of TestProcessNotes_BackendUnavailableStopsCycle (which only
// covers an HTTP status). Counting connection attempts (rather than hashed
// notes) proves the break actually happened for a connection-level failure
// too, not just an HTTP-status one.
func TestProcessNotes_BackendConnectionErrorStopsCycle(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)
	en, conns := newDeadConnBackend(t, "")

	noteFiles := []string{"first.md", "second.md", "third.md"}
	for _, name := range noteFiles {
		content := "# " + name + "\n\nNo tags here."
		if err := os.WriteFile(filepath.Join(tmpDir, "notes", name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	processNotes(context.Background(), en, db, v)

	if got := conns.Load(); got != 1 {
		t.Errorf("expected exactly 1 connection attempt despite %d tagless notes, got %d", len(noteFiles), got)
	}
	for _, name := range noteFiles {
		hash, err := db.GetNoteHash("notes/" + name)
		if err != nil {
			t.Fatalf("GetNoteHash(%s): %v", name, err)
		}
		if hash != "" {
			t.Errorf("notes/%s must stay unhashed while the backend is unavailable, got %q", name, hash)
		}
	}
}

// TestProcessNotes_BackendUnavailableSkipsRemainingIncludingTaggedNotes pins
// the stop-cycle semantic in processNotes: once the backend-unavailable
// break fires on the first tagless note, every remaining note in the scan
// is skipped too — even one that is already tagged and needs no backend
// call at all. This is the simpler, more predictable behaviour: a half-run
// cycle that quietly finishes the "free" notes would be harder to reason
// about than one that stops cleanly.
func TestProcessNotes_BackendUnavailableSkipsRemainingIncludingTaggedNotes(t *testing.T) {
	tmpDir, db, v, _, _ := setupTestEnv(t)
	fb := newFakeBackend(t, "golang, testing")
	fb.status.Store(http.StatusPaymentRequired)

	// "aaa-tagless" sorts (and so scans) before "zzz-tagged", so it triggers
	// the stop before the already-tagged note is ever reached.
	if err := os.WriteFile(filepath.Join(tmpDir, "notes", "aaa-tagless.md"),
		[]byte("# A\n\nNo tags here."), 0o644); err != nil {
		t.Fatalf("WriteFile aaa-tagless.md: %v", err)
	}
	taggedContent := "---\ntags:\n  - already-tagged\n---\n\n# Z\n\nAlready tagged; needs no backend call.\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "notes", "zzz-tagged.md"),
		[]byte(taggedContent), 0o644); err != nil {
		t.Fatalf("WriteFile zzz-tagged.md: %v", err)
	}

	processNotes(context.Background(), fb.client(), db, v)

	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 backend call (the tagless note only), got %d", got)
	}

	hash, err := db.GetNoteHash("notes/zzz-tagged.md")
	if err != nil {
		t.Fatalf("GetNoteHash(zzz-tagged.md): %v", err)
	}
	if hash != "" {
		t.Errorf("already-tagged note must be skipped once the cycle stops, got hash %q", hash)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "wiki", "zzz-tagged.md")); !os.IsNotExist(err) {
		t.Error("wiki copy for the already-tagged note must not be written once the cycle stops")
	}
}

func TestReEnrichBookmark_BackendUnavailableLeavesWikiUntouched(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	writeInboxFile(t, tmpDir, "keep-me.md", "https://example.com/keep\n")
	processInbox(context.Background(), sc, en, db, v)

	wikiPath := filepath.Join(tmpDir, "wiki", "keep-me.md")
	before := readFile(t, wikiPath)

	rec, err := db.GetBookmarkByURL(vault.NormalizeURL("https://example.com/keep"))
	if err != nil || rec == nil {
		t.Fatalf("precondition: bookmark should be stored: rec=%v err=%v", rec, err)
	}

	fb.status.Store(http.StatusPaymentRequired)

	err = reEnrichBookmark(context.Background(), *rec, en, db, v)
	if err == nil {
		t.Fatal("expected an error when the backend is unavailable")
	}
	if !errors.Is(err, enrich.ErrBackendUnavailable) {
		t.Errorf("errors.Is(err, enrich.ErrBackendUnavailable) = false; err = %v", err)
	}
	if after := readFile(t, wikiPath); after != before {
		t.Errorf("existing wiki page was modified while the backend was unavailable:\n%s", after)
	}
}

func TestReEnrichAll_BackendUnavailableStopsRun(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	for _, it := range waitingInboxItems {
		writeInboxFile(t, tmpDir, it.file, it.url+"\n")
	}
	processInbox(context.Background(), sc, en, db, v)

	before := make(map[string]string, len(waitingInboxItems))
	for _, it := range waitingInboxItems {
		before[it.file] = readFile(t, filepath.Join(tmpDir, "wiki", it.file))
	}

	fb.status.Store(http.StatusPaymentRequired)
	fb.calls.Store(0)

	stoppedEarly := reEnrichAll(context.Background(), en, db, v)

	if !stoppedEarly {
		t.Error("expected reEnrichAll to report it stopped early due to the unavailable backend")
	}
	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected re-enrichment to stop after 1 backend call, got %d", got)
	}
	for _, it := range waitingInboxItems {
		if after := readFile(t, filepath.Join(tmpDir, "wiki", it.file)); after != before[it.file] {
			t.Errorf("wiki/%s was modified while the backend was unavailable", it.file)
		}
	}
}

// TestReEnrichAll_ConnectionErrorStopsRun is the connection-error
// counterpart of TestReEnrichAll_BackendUnavailableStopsRun (which only
// covers an HTTP status).
func TestReEnrichAll_ConnectionErrorStopsRun(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	for _, it := range waitingInboxItems {
		writeInboxFile(t, tmpDir, it.file, it.url+"\n")
	}
	processInbox(context.Background(), sc, en, db, v)

	before := make(map[string]string, len(waitingInboxItems))
	for _, it := range waitingInboxItems {
		before[it.file] = readFile(t, filepath.Join(tmpDir, "wiki", it.file))
	}

	deadEn, conns := newDeadConnBackend(t, "")

	stoppedEarly := reEnrichAll(context.Background(), deadEn, db, v)

	if !stoppedEarly {
		t.Error("expected reEnrichAll to report it stopped early due to the unavailable backend")
	}
	if got := conns.Load(); got != 1 {
		t.Errorf("expected exactly 1 connection attempt despite %d bookmarks, got %d", len(waitingInboxItems), got)
	}
	for _, it := range waitingInboxItems {
		if after := readFile(t, filepath.Join(tmpDir, "wiki", it.file)); after != before[it.file] {
			t.Errorf("wiki/%s was modified while the backend was unavailable", it.file)
		}
	}
}

// TestReEnrichAll_GenericErrorContinuesToNextItem covers the branch at
// main.go reEnrichAll where a per-item error that is NOT
// ErrBackendUnavailable (main.go:454-456-ish, the generic-failure branch)
// only fails that one item and the loop continues to the next bookmark,
// unlike the backend-unavailable branch which stops the whole run.
//
// db.ListBookmarks has no ORDER BY (internal/store/sqlite.go), so SQLite's
// row order is not guaranteed by SQL semantics — only observed to match
// insertion/rowid order in practice. This test IS order-dependent: it only
// distinguishes continue-vs-break if the broken record is visited before
// the good one. If the good record were visited first, reverting
// continue->break would still leave the good bookmark re-enriched before
// the loop ever reached (and broke on) the broken one, so the assertions
// below would pass even with the bug. Rather than assume that order holds,
// the pre-check right before reEnrichAll queries db.ListBookmarks() and
// fails loudly if the broken record is not first, so a change in SQLite's
// observed ordering surfaces as a clear failure instead of a silent false
// green.
func TestReEnrichAll_GenericErrorContinuesToNextItem(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)

	const reEnrichedWikiResponse = `---
title: "Test Article Re-Enriched"
source: "http://example.com/test"
date_saved: 2024-01-01
tags:
  - test
  - re-enriched
type: bookmark
---

# Test Article Re-Enriched

## Summary
This is the re-enriched summary.

## Key Takeaways
- Re-enriched takeaway
`

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeChatCompletion(t, w, validWikiResponse)
			return
		}
		writeChatCompletion(t, w, reEnrichedWikiResponse)
	}))
	t.Cleanup(srv.Close)

	en := enrich.NewClient("", "test-model", 0, 0)
	en.SetBaseURL(srv.URL)

	// A bookmark record with no raw path fails inside reEnrichBookmark with
	// a plain (non-ErrBackendUnavailable) error, before any backend call.
	if err := db.UpsertBookmark(&store.BookmarkRecord{
		WikiPath:      "wiki/broken.md",
		RawPath:       "",
		Title:         "Broken",
		URL:           "https://example.com/broken",
		URLNormalized: vault.NormalizeURL("https://example.com/broken"),
		ProcessedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("UpsertBookmark broken: %v", err)
	}

	// A real bookmark, created via processInbox (backend call #1, the
	// validWikiResponse reply) so its RawPath/WikiPath are valid and it can
	// actually be re-enriched.
	writeInboxFile(t, tmpDir, "good.md", "https://example.com/good\n")
	processInbox(context.Background(), sc, en, db, v)

	before := readFile(t, filepath.Join(tmpDir, "wiki", "good.md"))

	// This test's continue-vs-break distinction only holds if reEnrichAll
	// visits the broken record first (see doc comment above). Assert that
	// explicitly instead of assuming it, so a change in SQLite's observed
	// row order fails loudly here rather than silently passing a reverted
	// continue->break regression.
	bookmarks, err := db.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(bookmarks) != 2 || bookmarks[0].URL != "https://example.com/broken" {
		t.Fatalf("test requires db.ListBookmarks() to return the broken bookmark first; got order %+v (db.ListBookmarks has no ORDER BY, so this depends on SQLite's current insertion/rowid ordering — update this test if that ordering ever changes)", bookmarks)
	}

	stoppedEarly := reEnrichAll(context.Background(), en, db, v)

	if stoppedEarly {
		t.Error("a generic per-item error must not stop the run")
	}

	after := readFile(t, filepath.Join(tmpDir, "wiki", "good.md"))
	if after == before {
		t.Error("expected the good bookmark's wiki file to be rewritten by reEnrichAll; the loop must continue past the broken bookmark and reach it")
	}
	if !strings.Contains(after, "Re-enriched takeaway") {
		t.Errorf("expected the good bookmark's wiki file to contain the re-enriched content, got: %s", after)
	}
	// 1 call from processInbox (creation) + 1 call from reEnrichAll
	// (re-enrichment of the good bookmark; the broken one never reaches the
	// backend, since the pre-check above confirms it is visited first and
	// fails before any backend call).
	if got := calls.Load(); got != 2 {
		t.Errorf("expected exactly 2 backend calls total (1 create + 1 re-enrich), got %d", got)
	}
}

// TestRunPollCycle_ReEnrichAllBackendUnavailableSkipsNotes pins the
// runPollCycle fix: when a forced re-enrichment run stops early because the
// backend is unavailable, the same poll cycle must not go on to call
// processNotes against that same dead backend.
func TestRunPollCycle_ReEnrichAllBackendUnavailableSkipsNotes(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	// Create one bookmark (to re-enrich) while the backend is healthy.
	writeInboxFile(t, tmpDir, "existing.md", "https://example.com/existing\n")
	processInbox(context.Background(), sc, en, db, v)

	// A tagless note that would otherwise trigger a SuggestTags call in
	// Step 3 (processNotes) of the same poll cycle.
	notePath := filepath.Join(tmpDir, "notes", "tagless.md")
	if err := os.WriteFile(notePath, []byte("# Tagless\n\nNo tags here."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fb.status.Store(http.StatusPaymentRequired)
	fb.calls.Store(0)

	cfg := &config{forceReEnrich: true}
	runPollCycle(context.Background(), cfg, nil, sc, en, db, v)

	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 backend call (re-enrichment only; processNotes must be skipped), got %d", got)
	}
	hash, err := db.GetNoteHash("notes/tagless.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "" {
		t.Errorf("note must stay unprocessed when processNotes is skipped, got hash %q", hash)
	}
}

// TestRunPollCycle_ProcessInboxBackendUnavailableSkipsNotes pins the
// non-forced counterpart of TestRunPollCycle_ReEnrichAllBackendUnavailableSkipsNotes:
// when the ordinary (non-forceReEnrich) processInbox path stops early
// because the backend is unavailable, runPollCycle must still skip
// processNotes in the same cycle — otherwise a tagless note in the vault
// would make processNotes call the same dead backend again, violating the
// plan's "one attempt, one ERROR" guarantee (docs/plans/plan-openai-compatible-enrichment.md
// section 2).
func TestRunPollCycle_ProcessInboxBackendUnavailableSkipsNotes(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	// An inbox item that will hit the dead backend in Step 2.
	writeInboxFile(t, tmpDir, "stuck.md", "https://example.com/stuck\n")

	// A tagless note that would otherwise trigger a second backend call in
	// Step 3 (processNotes) of the same poll cycle.
	notePath := filepath.Join(tmpDir, "notes", "tagless.md")
	if err := os.WriteFile(notePath, []byte("# Tagless\n\nNo tags here."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fb.status.Store(http.StatusPaymentRequired)

	logs := captureLogs(t, slog.LevelInfo)
	cfg := &config{forceReEnrich: false}
	runPollCycle(context.Background(), cfg, nil, sc, en, db, v)

	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 backend call total (processInbox only; processNotes must be skipped), got %d", got)
	}

	logged := logs.String()
	if got := strings.Count(logged, "level=ERROR"); got != 1 {
		t.Errorf("expected exactly 1 ERROR log line, got %d:\n%s", got, logged)
	}

	hash, err := db.GetNoteHash("notes/tagless.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "" {
		t.Errorf("note must stay unprocessed when processNotes is skipped, got hash %q", hash)
	}
}

// TestRunPollCycle_ForceReEnrichHealthyBackendStillProcessesNotes guards
// against gating processNotes on forceReEnrich alone instead of on the
// backend-unavailable signal: with a forced re-enrichment run against a
// healthy backend, Step 3 (processNotes) must still run and actually
// process a tagless note in the vault.
func TestRunPollCycle_ForceReEnrichHealthyBackendStillProcessesNotes(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)
	fb := newFakeBackend(t, validWikiResponse)
	en := fb.client()

	// A tagless note that SuggestTags must process once processNotes runs.
	notePath := filepath.Join(tmpDir, "notes", "tagless.md")
	if err := os.WriteFile(notePath, []byte("# Tagless\n\nNo tags here."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &config{forceReEnrich: true}
	runPollCycle(context.Background(), cfg, nil, sc, en, db, v)

	hash, err := db.GetNoteHash("notes/tagless.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash == "" {
		t.Error("expected processNotes to run and hash the tagless note when the backend is healthy, even for a forced re-enrichment cycle")
	}
}
