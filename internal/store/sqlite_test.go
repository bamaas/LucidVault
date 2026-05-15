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
