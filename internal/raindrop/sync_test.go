package raindrop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lucidvault/internal/store"
	"lucidvault/internal/vault"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestSyncToInbox_CreatesFiles(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	mustMkdir(t, filepath.Join(vaultPath, "inbox"))

	bookmarks := []Bookmark{
		{ID: 1, Title: "Go Testing Guide", Link: "https://example.com/go-testing", Tags: []string{"go", "testing"}},
		{ID: 2, Title: "Rust Intro", Link: "https://example.com/rust-intro", Tags: []string{"rust"}},
	}

	created, err := SyncToInbox(bookmarks, db, vaultPath, false)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}
	if created != 2 {
		t.Errorf("expected 2 created, got %d", created)
	}

	// Verify files exist with correct content
	entries, _ := os.ReadDir(filepath.Join(vaultPath, "inbox"))
	if len(entries) != 2 {
		t.Fatalf("expected 2 inbox files, got %d", len(entries))
	}

	// Read one file and verify format
	data, err := os.ReadFile(filepath.Join(vaultPath, "inbox", vault.GenerateSlug("Go Testing Guide")+".md"))
	if err != nil {
		t.Fatalf("reading inbox file: %v", err)
	}
	content := string(data)
	if !contains(content, "https://example.com/go-testing") {
		t.Error("inbox file missing URL")
	}
	if !contains(content, "title:") {
		t.Error("inbox file missing title in frontmatter")
	}
	if !contains(content, "go") {
		t.Error("inbox file missing tags")
	}
}

func TestSyncToInbox_SkipsProcessedURLs(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	mustMkdir(t, filepath.Join(vaultPath, "inbox"))

	// Mark URL as already processed
	if err := db.UpsertBookmark(&store.BookmarkRecord{
		WikiPath:      "wiki/already-done.md",
		RawPath:       "raw/already-done.md",
		Title:         "Already Done",
		URL:           "https://example.com/already-done",
		URLNormalized: "https://example.com/already-done",
	}); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	bookmarks := []Bookmark{
		{ID: 1, Title: "Already Done", Link: "https://example.com/already-done"},
		{ID: 2, Title: "New Article", Link: "https://example.com/new-article"},
	}

	created, err := SyncToInbox(bookmarks, db, vaultPath, false)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}
	if created != 1 {
		t.Errorf("expected 1 created (skipping processed), got %d", created)
	}

	entries, _ := os.ReadDir(filepath.Join(vaultPath, "inbox"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 inbox file, got %d", len(entries))
	}
}

func TestSyncToInbox_SkipsExistingInboxFiles(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	inboxDir := filepath.Join(vaultPath, "inbox")
	mustMkdir(t, inboxDir)

	// Pre-create an inbox file
	slug := vault.GenerateSlug("Existing Article")
	mustWriteFile(t, filepath.Join(inboxDir, slug+".md"), []byte("https://example.com/existing\n"))

	bookmarks := []Bookmark{
		{ID: 1, Title: "Existing Article", Link: "https://example.com/existing"},
	}

	created, err := SyncToInbox(bookmarks, db, vaultPath, false)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}
	if created != 0 {
		t.Errorf("expected 0 created (file already exists), got %d", created)
	}
}

func TestSyncToInbox_FileFormat(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	mustMkdir(t, filepath.Join(vaultPath, "inbox"))

	bookmarks := []Bookmark{
		{ID: 42, Title: "My Article", Link: "https://example.com/my-article", Tags: []string{"tech", "go"}},
	}

	_, err := SyncToInbox(bookmarks, db, vaultPath, false)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(vaultPath, "inbox", "my-article.md"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	content := string(data)

	// Should have frontmatter with title and tags
	if !contains(content, "---") {
		t.Error("missing frontmatter delimiters")
	}
	if !contains(content, `title: "My Article"`) {
		t.Errorf("missing or incorrect title in frontmatter, got:\n%s", content)
	}
	if !contains(content, "- tech") || !contains(content, "- go") {
		t.Errorf("missing tags in frontmatter, got:\n%s", content)
	}
	// URL should be after frontmatter
	if !contains(content, "https://example.com/my-article") {
		t.Error("missing URL in file body")
	}
}

func TestSyncToInbox_SlugCollisionSkipsSecond(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	mustMkdir(t, filepath.Join(vaultPath, "inbox"))

	// Two bookmarks with titles that produce the same slug
	bookmarks := []Bookmark{
		{ID: 1, Title: "Go Testing", Link: "https://example.com/go-testing-1"},
		{ID: 2, Title: "Go Testing!", Link: "https://example.com/go-testing-2"},
	}

	created, err := SyncToInbox(bookmarks, db, vaultPath, false)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}
	if created != 1 {
		t.Errorf("expected 1 created (slug collision), got %d", created)
	}

	// File should contain URL of the first bookmark
	data, err := os.ReadFile(filepath.Join(vaultPath, "inbox", "go-testing.md"))
	if err != nil {
		t.Fatalf("reading inbox file: %v", err)
	}
	if !contains(string(data), "https://example.com/go-testing-1") {
		t.Error("expected inbox file to contain URL from first bookmark")
	}
}

func TestSyncToInbox_ForceCreatesFilesForProcessedURLs(t *testing.T) {
	db := newTestStore(t)
	vaultPath := t.TempDir()
	mustMkdir(t, filepath.Join(vaultPath, "inbox"))

	// Mark URL as already processed in DB
	if err := db.UpsertBookmark(&store.BookmarkRecord{
		WikiPath:      "wiki/already-done.md",
		RawPath:       "raw/already-done.md",
		Title:         "Already Done",
		URL:           "https://example.com/already-done",
		URLNormalized: "https://example.com/already-done",
	}); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	bookmarks := []Bookmark{
		{ID: 1, Title: "Already Done", Link: "https://example.com/already-done"},
		{ID: 2, Title: "New Article", Link: "https://example.com/new-article"},
	}

	// With force=true, the processed URL should still get an inbox file
	created, err := SyncToInbox(bookmarks, db, vaultPath, true)
	if err != nil {
		t.Fatalf("SyncToInbox: %v", err)
	}
	if created != 2 {
		t.Errorf("expected 2 created (force bypasses dedup), got %d", created)
	}

	entries, _ := os.ReadDir(filepath.Join(vaultPath, "inbox"))
	if len(entries) != 2 {
		t.Fatalf("expected 2 inbox files, got %d", len(entries))
	}

	// Verify the re-fetched file has correct content
	data, err := os.ReadFile(filepath.Join(vaultPath, "inbox", vault.GenerateSlug("Already Done")+".md"))
	if err != nil {
		t.Fatalf("reading inbox file: %v", err)
	}
	content := string(data)
	if !contains(content, "https://example.com/already-done") {
		t.Error("inbox file missing URL for re-fetched bookmark")
	}
	if !contains(content, `title: "Already Done"`) {
		t.Error("inbox file missing title in frontmatter")
	}

	// Verify DB record was NOT overwritten (wiki_path/raw_path preserved)
	rec, err := db.GetBookmarkByURL(vault.NormalizeURL("https://example.com/already-done"))
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if rec.WikiPath != "wiki/already-done.md" {
		t.Errorf("expected wiki_path preserved, got %q", rec.WikiPath)
	}
	if rec.RawPath != "raw/already-done.md" {
		t.Errorf("expected raw_path preserved, got %q", rec.RawPath)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
