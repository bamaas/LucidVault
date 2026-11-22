package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

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

func deadBackendClient(t *testing.T) *enrich.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close()
	en := enrich.NewClient("", "test-model", 0, 0)
	en.SetBaseURL(deadURL)
	return en
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

			for _, it := range waitingInboxItems {
				writeInboxFile(t, tmpDir, it.file, it.url+"\n")
			}

			processInbox(context.Background(), sc, fb.client(), db, v)

			if got := fb.calls.Load(); got != 1 {
				t.Errorf("expected the cycle to stop after 1 backend call, got %d", got)
			}
			assertInboxItemsWaiting(t, tmpDir, db)
		})
	}
}

func TestProcessInbox_BackendConnectionRefusedKeepsInbox(t *testing.T) {
	tmpDir, db, v, sc, _ := setupTestEnv(t)

	for _, it := range waitingInboxItems {
		writeInboxFile(t, tmpDir, it.file, it.url+"\n")
	}

	processInbox(context.Background(), sc, deadBackendClient(t), db, v)

	assertInboxItemsWaiting(t, tmpDir, db)
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

	reEnrichAll(context.Background(), en, db, v)

	if got := fb.calls.Load(); got != 1 {
		t.Errorf("expected re-enrichment to stop after 1 backend call, got %d", got)
	}
	for _, it := range waitingInboxItems {
		if after := readFile(t, filepath.Join(tmpDir, "wiki", it.file)); after != before[it.file] {
			t.Errorf("wiki/%s was modified while the backend was unavailable", it.file)
		}
	}
}
