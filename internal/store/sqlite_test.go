package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedBookmark(t *testing.T, s *Store) {
	t.Helper()
	err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/test-article.md",
		RawPath:       "raw/test-article.md",
		Title:         "Test Article",
		URL:           "http://example.com/test",
		URLNormalized: "http://example.com/test",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}
}

func TestUpsertNote_New(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/foo.md", "abc123", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	hash, err := s.GetNoteHash("notes/foo.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want %q", hash, "abc123")
	}
}

func TestUpsertNote_Update(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/foo.md", "abc123", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote (initial): %v", err)
	}

	if err := s.UpsertNote("notes/foo.md", "def456", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote (update): %v", err)
	}

	hash, err := s.GetNoteHash("notes/foo.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "def456" {
		t.Errorf("hash = %q, want %q after update", hash, "def456")
	}
}

func TestUpsertNote_WikiPath(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/bar.md", "hash1", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	records, err := s.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].WikiPath != "wiki/bar.md" {
		t.Errorf("WikiPath = %q, want %q", records[0].WikiPath, "wiki/bar.md")
	}
}

func TestGetNoteHash_NotFound(t *testing.T) {
	s := newTestStore(t)

	hash, err := s.GetNoteHash("notes/nonexistent.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "" {
		t.Errorf("hash = %q, want empty string for missing note", hash)
	}
}

func TestDeleteNote(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/foo.md", "abc123", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	if err := s.DeleteNote("notes/foo.md"); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	hash, err := s.GetNoteHash("notes/foo.md")
	if err != nil {
		t.Fatalf("GetNoteHash: %v", err)
	}
	if hash != "" {
		t.Errorf("hash = %q, want empty string after deletion", hash)
	}
}

func TestListBookmarks(t *testing.T) {
	s := newTestStore(t)

	err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/alpha.md",
		RawPath:       "raw/alpha.md",
		Title:         "Alpha",
		URL:           "http://example.com/alpha",
		URLNormalized: "http://example.com/alpha",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertBookmark alpha: %v", err)
	}

	err = s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/beta.md",
		RawPath:       "raw/beta.md",
		Title:         "Beta",
		URL:           "http://example.com/beta",
		URLNormalized: "http://example.com/beta",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertBookmark beta: %v", err)
	}

	records, err := s.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	byURL := make(map[string]BookmarkRecord, len(records))
	for _, r := range records {
		byURL[r.URLNormalized] = r
	}

	if r, ok := byURL["http://example.com/alpha"]; !ok {
		t.Error("missing alpha in ListBookmarks result")
	} else if r.WikiPath != "wiki/alpha.md" {
		t.Errorf("alpha WikiPath = %q, want %q", r.WikiPath, "wiki/alpha.md")
	}

	if r, ok := byURL["http://example.com/beta"]; !ok {
		t.Error("missing beta in ListBookmarks result")
	} else if r.WikiPath != "wiki/beta.md" {
		t.Errorf("beta WikiPath = %q, want %q", r.WikiPath, "wiki/beta.md")
	}
}

func TestGetBookmarkByURL_Found(t *testing.T) {
	s := newTestStore(t)
	seedBookmark(t, s)

	rec, err := s.GetBookmarkByURL("http://example.com/test")
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.URLNormalized != "http://example.com/test" {
		t.Errorf("URLNormalized = %q, want %q", rec.URLNormalized, "http://example.com/test")
	}
}

func TestGetBookmarkByURL_NotFound(t *testing.T) {
	s := newTestStore(t)

	rec, err := s.GetBookmarkByURL("http://example.com/missing")
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil, got %+v", rec)
	}
}

func TestUpsertBookmark_Insert(t *testing.T) {
	s := newTestStore(t)

	rec := &BookmarkRecord{
		WikiPath:      "wiki/new-article.md",
		RawPath:       "raw/new-article.md",
		Title:         "New Article",
		URL:           "http://example.com/new",
		URLNormalized: "http://example.com/new",
		ProcessedAt:   time.Now(),
	}
	if err := s.UpsertBookmark(rec); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	found, err := s.GetBookmarkByURL("http://example.com/new")
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if found == nil {
		t.Fatal("expected record after insert")
	}
	if found.Title != "New Article" {
		t.Errorf("Title = %q, want %q", found.Title, "New Article")
	}
}

func TestUpsertBookmark_Update(t *testing.T) {
	s := newTestStore(t)

	rec := &BookmarkRecord{
		WikiPath:      "wiki/article.md",
		RawPath:       "raw/article.md",
		Title:         "Original Title",
		URL:           "http://example.com/article",
		URLNormalized: "http://example.com/article",
		ProcessedAt:   time.Now(),
	}
	if err := s.UpsertBookmark(rec); err != nil {
		t.Fatalf("UpsertBookmark (insert): %v", err)
	}

	rec.Title = "Updated Title"
	rec.WikiPath = "wiki/article-v2.md"
	if err := s.UpsertBookmark(rec); err != nil {
		t.Fatalf("UpsertBookmark (update): %v", err)
	}

	found, err := s.GetBookmarkByURL("http://example.com/article")
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if found.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", found.Title, "Updated Title")
	}
	if found.WikiPath != "wiki/article-v2.md" {
		t.Errorf("WikiPath = %q, want %q", found.WikiPath, "wiki/article-v2.md")
	}

	// Should still be only one record
	records, err := s.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	count := 0
	for _, r := range records {
		if r.URLNormalized == "http://example.com/article" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 record for URL, got %d", count)
	}
}

func TestListNotes(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/alpha.md", "hash1", "wiki/alpha.md"); err != nil {
		t.Fatalf("UpsertNote alpha: %v", err)
	}
	if err := s.UpsertNote("notes/beta.md", "hash2", "wiki/beta.md"); err != nil {
		t.Fatalf("UpsertNote beta: %v", err)
	}

	records, err := s.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	byPath := make(map[string]NoteRecord, len(records))
	for _, r := range records {
		byPath[r.Path] = r
	}

	if r, ok := byPath["notes/alpha.md"]; !ok {
		t.Error("missing notes/alpha.md in ListNotes result")
	} else {
		if r.ContentHash != "hash1" {
			t.Errorf("alpha ContentHash = %q, want %q", r.ContentHash, "hash1")
		}
		if r.LastProcessed.IsZero() {
			t.Error("alpha LastProcessed should not be zero")
		}
	}

	if r, ok := byPath["notes/beta.md"]; !ok {
		t.Error("missing notes/beta.md in ListNotes result")
	} else {
		if r.ContentHash != "hash2" {
			t.Errorf("beta ContentHash = %q, want %q", r.ContentHash, "hash2")
		}
		if r.LastProcessed.IsZero() {
			t.Error("beta LastProcessed should not be zero")
		}
	}
}
