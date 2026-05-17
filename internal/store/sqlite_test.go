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

func seedBookmark(t *testing.T, s *Store, sourceID int) {
	t.Helper()
	err := s.SaveBookmark(&BookmarkRecord{
		SourceID:      sourceID,
		WikiPath:      "wiki/test-article.md",
		RawPath:       "raw/2024-01-01-test-article.md",
		Title:         "Test Article",
		URL:           "http://example.com/test",
		URLNormalized: "http://example.com/test",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("SaveBookmark: %v", err)
	}
}

func TestGetBookmarkBySourceID_Found(t *testing.T) {
	s := newTestStore(t)
	seedBookmark(t, s, 42)

	rec, err := s.GetBookmarkBySourceID(42)
	if err != nil {
		t.Fatalf("GetBookmarkBySourceID: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record, got nil")
	}
	if rec.SourceID != 42 {
		t.Errorf("SourceID = %d, want 42", rec.SourceID)
	}
	if rec.WikiPath != "wiki/test-article.md" {
		t.Errorf("WikiPath = %q, want %q", rec.WikiPath, "wiki/test-article.md")
	}
}

func TestGetBookmarkBySourceID_NotFound(t *testing.T) {
	s := newTestStore(t)

	rec, err := s.GetBookmarkBySourceID(999)
	if err != nil {
		t.Fatalf("GetBookmarkBySourceID: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil, got %+v", rec)
	}
}

func TestDeleteBySourceID(t *testing.T) {
	s := newTestStore(t)
	seedBookmark(t, s, 42)

	if err := s.DeleteBySourceID(42); err != nil {
		t.Fatalf("DeleteBySourceID: %v", err)
	}

	exists, err := s.IsProcessedBySourceID(42)
	if err != nil {
		t.Fatalf("IsProcessedBySourceID: %v", err)
	}
	if exists {
		t.Error("expected record to be deleted")
	}
}

func TestDeleteBySourceID_Idempotent(t *testing.T) {
	s := newTestStore(t)

	// Deleting a non-existent record should not error
	if err := s.DeleteBySourceID(999); err != nil {
		t.Fatalf("DeleteBySourceID on missing record: %v", err)
	}
}

func TestUpsertNote_New(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/foo.md", "abc123"); err != nil {
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

	if err := s.UpsertNote("notes/foo.md", "abc123"); err != nil {
		t.Fatalf("UpsertNote (initial): %v", err)
	}

	if err := s.UpsertNote("notes/foo.md", "def456"); err != nil {
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

	if err := s.UpsertNote("notes/foo.md", "abc123"); err != nil {
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

	// Save two bookmarks with distinct paths
	err := s.SaveBookmark(&BookmarkRecord{
		SourceID:      100,
		WikiPath:      "wiki/alpha.md",
		RawPath:       "raw/2024-01-01-alpha.md",
		Title:         "Alpha",
		URL:           "http://example.com/alpha",
		URLNormalized: "http://example.com/alpha",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("SaveBookmark alpha: %v", err)
	}

	err = s.SaveBookmark(&BookmarkRecord{
		SourceID:      200,
		WikiPath:      "wiki/beta.md",
		RawPath:       "raw/2024-01-02-beta.md",
		Title:         "Beta",
		URL:           "http://example.com/beta",
		URLNormalized: "http://example.com/beta",
		ProcessedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("SaveBookmark beta: %v", err)
	}

	records, err := s.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}

	byID := make(map[int]BookmarkRecord, len(records))
	for _, r := range records {
		byID[r.SourceID] = r
	}

	if r, ok := byID[100]; !ok {
		t.Error("missing source_id 100 in ListBookmarks result")
	} else if r.WikiPath != "wiki/alpha.md" {
		t.Errorf("alpha WikiPath = %q, want %q", r.WikiPath, "wiki/alpha.md")
	}

	if r, ok := byID[200]; !ok {
		t.Error("missing source_id 200 in ListBookmarks result")
	} else if r.WikiPath != "wiki/beta.md" {
		t.Errorf("beta WikiPath = %q, want %q", r.WikiPath, "wiki/beta.md")
	}
}

func TestListNotes(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/alpha.md", "hash1"); err != nil {
		t.Fatalf("UpsertNote alpha: %v", err)
	}
	if err := s.UpsertNote("notes/beta.md", "hash2"); err != nil {
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
