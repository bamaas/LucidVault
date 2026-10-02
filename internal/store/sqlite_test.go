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

func TestGetNote_Found(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/foo.md", "abc123", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	rec, ok, err := s.GetNote("notes/foo.md")
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if !ok {
		t.Fatal("expected ok = true for existing note")
	}
	if rec.Path != "notes/foo.md" || rec.ContentHash != "abc123" || rec.WikiPath != "wiki/foo.md" {
		t.Errorf("rec = %+v, want path=notes/foo.md hash=abc123 wikiPath=wiki/foo.md", rec)
	}
	if rec.LastProcessed.IsZero() {
		t.Error("expected LastProcessed to be set for an existing note")
	}
}

func TestGetNote_NotFound(t *testing.T) {
	s := newTestStore(t)

	rec, ok, err := s.GetNote("notes/nonexistent.md")
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if ok {
		t.Error("expected ok = false for missing note")
	}
	if rec != (NoteRecord{}) {
		t.Errorf("expected zero-value NoteRecord, got %+v", rec)
	}
}

func TestNoteWikiPathShared_NotShared(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	shared, err := s.NoteWikiPathShared("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NoteWikiPathShared: %v", err)
	}
	if shared {
		t.Error("expected shared = false when no other note record shares the wiki_path")
	}
}

func TestNoteWikiPathShared_Shared(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/foo.md", "hash-b", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}

	shared, err := s.NoteWikiPathShared("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NoteWikiPathShared: %v", err)
	}
	if !shared {
		t.Error("expected shared = true when another note record has the same wiki_path")
	}
}

func TestNoteWikiPathShared_DifferentWikiPath(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/bar.md", "hash-b", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}

	shared, err := s.NoteWikiPathShared("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NoteWikiPathShared: %v", err)
	}
	if shared {
		t.Error("expected shared = false when the other record has a different wiki_path")
	}
}

func TestNoteWikiPathShared_EmptyWikiPath(t *testing.T) {
	s := newTestStore(t)

	shared, err := s.NoteWikiPathShared("notes/a/foo.md", "")
	if err != nil {
		t.Fatalf("NoteWikiPathShared: %v", err)
	}
	if shared {
		t.Error("expected shared = false for an empty wiki_path")
	}
}

func TestNotesSharingWikiPath_EmptyWikiPath(t *testing.T) {
	s := newTestStore(t)

	sharers, err := s.NotesSharingWikiPath("notes/a/foo.md", "")
	if err != nil {
		t.Fatalf("NotesSharingWikiPath: %v", err)
	}
	if len(sharers) != 0 {
		t.Errorf("expected no sharers for an empty wiki_path, got %v", sharers)
	}
}

func TestNotesSharingWikiPath_ExcludesSelf(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	sharers, err := s.NotesSharingWikiPath("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NotesSharingWikiPath: %v", err)
	}
	if len(sharers) != 0 {
		t.Errorf("expected the passed-in path to be excluded from its own sharers, got %v", sharers)
	}
}

func TestNotesSharingWikiPath_ReturnsMultipleSharers(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/foo.md", "hash-b", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}
	if err := s.UpsertNote("notes/c/foo.md", "hash-c", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote c: %v", err)
	}

	sharers, err := s.NotesSharingWikiPath("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NotesSharingWikiPath: %v", err)
	}
	want := map[string]bool{"notes/b/foo.md": true, "notes/c/foo.md": true}
	if len(sharers) != len(want) {
		t.Fatalf("sharers = %v, want exactly %v", sharers, want)
	}
	for _, p := range sharers {
		if !want[p] {
			t.Errorf("unexpected sharer %q, want one of %v", p, want)
		}
	}
}

func TestNotesSharingWikiPath_DifferentWikiPathNotMatched(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/bar.md", "hash-b", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}

	sharers, err := s.NotesSharingWikiPath("notes/a/foo.md", "wiki/foo.md")
	if err != nil {
		t.Fatalf("NotesSharingWikiPath: %v", err)
	}
	if len(sharers) != 0 {
		t.Errorf("expected a record with a different wiki_path not to be matched, got %v", sharers)
	}
}

func TestNotesSharingWikiPath_AfterClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.NotesSharingWikiPath("notes/a/foo.md", "wiki/foo.md")
	if err == nil {
		t.Error("expected an error from NotesSharingWikiPath on a closed store")
	}
}

func TestRepairDuplicateNoteWikiPaths_ClearsAllInGroup(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/b/foo.md", "hash-b", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}
	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}

	repaired, err := s.RepairDuplicateNoteWikiPaths()
	if err != nil {
		t.Fatalf("RepairDuplicateNoteWikiPaths: %v", err)
	}
	if repaired != 2 {
		t.Fatalf("repaired = %d, want 2 (every record in the duplicate group, not all-but-one)", repaired)
	}

	recA, _, err := s.GetNote("notes/a/foo.md")
	if err != nil {
		t.Fatalf("GetNote a: %v", err)
	}
	if recA.ContentHash != "" {
		t.Errorf("expected a's content_hash to be cleared too, got %q", recA.ContentHash)
	}

	recB, _, err := s.GetNote("notes/b/foo.md")
	if err != nil {
		t.Fatalf("GetNote b: %v", err)
	}
	if recB.ContentHash != "" {
		t.Errorf("expected the duplicate's content_hash to be cleared, got %q", recB.ContentHash)
	}
}

func TestRepairDuplicateNoteWikiPaths_ThreeRecordsSharingOnePath(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/foo.md", "hash-b", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}
	if err := s.UpsertNote("notes/c/foo.md", "hash-c", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote c: %v", err)
	}

	repaired, err := s.RepairDuplicateNoteWikiPaths()
	if err != nil {
		t.Fatalf("RepairDuplicateNoteWikiPaths: %v", err)
	}
	if repaired != 3 {
		t.Fatalf("repaired = %d, want 3", repaired)
	}

	for _, path := range []string{"notes/a/foo.md", "notes/b/foo.md", "notes/c/foo.md"} {
		rec, _, err := s.GetNote(path)
		if err != nil {
			t.Fatalf("GetNote %s: %v", path, err)
		}
		if rec.ContentHash != "" {
			t.Errorf("expected content_hash cleared for %s, got %q", path, rec.ContentHash)
		}
	}
}

func TestRepairDuplicateNoteWikiPaths_TwoSeparateGroups(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a1/foo.md", "hash-a1", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a1: %v", err)
	}
	if err := s.UpsertNote("notes/a2/foo.md", "hash-a2", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a2: %v", err)
	}
	if err := s.UpsertNote("notes/b1/bar.md", "hash-b1", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote b1: %v", err)
	}
	if err := s.UpsertNote("notes/b2/bar.md", "hash-b2", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote b2: %v", err)
	}
	if err := s.UpsertNote("notes/unique.md", "hash-u", "wiki/unique.md"); err != nil {
		t.Fatalf("UpsertNote unique: %v", err)
	}

	repaired, err := s.RepairDuplicateNoteWikiPaths()
	if err != nil {
		t.Fatalf("RepairDuplicateNoteWikiPaths: %v", err)
	}
	if repaired != 4 {
		t.Fatalf("repaired = %d, want 4 (two separate duplicate groups of two)", repaired)
	}

	for _, path := range []string{"notes/a1/foo.md", "notes/a2/foo.md", "notes/b1/bar.md", "notes/b2/bar.md"} {
		rec, _, err := s.GetNote(path)
		if err != nil {
			t.Fatalf("GetNote %s: %v", path, err)
		}
		if rec.ContentHash != "" {
			t.Errorf("expected content_hash cleared for %s, got %q", path, rec.ContentHash)
		}
	}

	recUnique, _, err := s.GetNote("notes/unique.md")
	if err != nil {
		t.Fatalf("GetNote unique: %v", err)
	}
	if recUnique.ContentHash != "hash-u" {
		t.Errorf("expected the unrelated unique record's hash to survive untouched, got %q", recUnique.ContentHash)
	}
}

func TestRepairDuplicateNoteWikiPaths_EmptyWikiPathsNotTreatedAsDuplicates(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a.md", "hash-a", ""); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b.md", "hash-b", ""); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}

	repaired, err := s.RepairDuplicateNoteWikiPaths()
	if err != nil {
		t.Fatalf("RepairDuplicateNoteWikiPaths: %v", err)
	}
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0 — two empty wiki_path values must not count as a duplicate", repaired)
	}

	for _, path := range []string{"notes/a.md", "notes/b.md"} {
		rec, _, err := s.GetNote(path)
		if err != nil {
			t.Fatalf("GetNote %s: %v", path, err)
		}
		if rec.ContentHash == "" {
			t.Errorf("expected content_hash to survive untouched for %s, got empty", path)
		}
	}
}

func TestRepairDuplicateNoteWikiPaths_NoopWhenUnique(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertNote("notes/a/foo.md", "hash-a", "wiki/foo.md"); err != nil {
		t.Fatalf("UpsertNote a: %v", err)
	}
	if err := s.UpsertNote("notes/b/bar.md", "hash-b", "wiki/bar.md"); err != nil {
		t.Fatalf("UpsertNote b: %v", err)
	}

	repaired, err := s.RepairDuplicateNoteWikiPaths()
	if err != nil {
		t.Fatalf("RepairDuplicateNoteWikiPaths: %v", err)
	}
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0 when wiki_path values are already unique", repaired)
	}

	recA, _, err := s.GetNote("notes/a/foo.md")
	if err != nil {
		t.Fatalf("GetNote a: %v", err)
	}
	if recA.ContentHash != "hash-a" {
		t.Errorf("expected hash-a to survive a no-op repair, got %q", recA.ContentHash)
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

// --- Error paths: operations against a closed store ---

func TestGetNote_AfterClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, _, err := s.GetNote("notes/foo.md")
	if err == nil {
		t.Error("expected an error from GetNote on a closed store")
	}
}

func TestNoteWikiPathShared_AfterClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.NoteWikiPathShared("notes/a/foo.md", "wiki/foo.md")
	if err == nil {
		t.Error("expected an error from NoteWikiPathShared on a closed store")
	}
}

func TestRepairDuplicateNoteWikiPaths_AfterClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := s.RepairDuplicateNoteWikiPaths()
	if err == nil {
		t.Error("expected an error from RepairDuplicateNoteWikiPaths on a closed store")
	}
}
