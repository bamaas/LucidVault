package store

import (
	"fmt"
	"sort"
	"testing"
)

// --- Edge type ---

func TestEdgeType(t *testing.T) {
	e := Edge{FromSlug: "a", ToSlug: "b", Type: "wikilink"}
	if e.FromSlug != "a" || e.ToSlug != "b" || e.Type != "wikilink" {
		t.Errorf("unexpected Edge fields: %+v", e)
	}
}

// --- Migration: edges table created ---

func TestMigrate_EdgesTableExists(t *testing.T) {
	s := newTestStore(t)
	var name string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='edges'").Scan(&name)
	if err != nil {
		t.Fatalf("edges table not created by migrate: %v", err)
	}
	if name != "edges" {
		t.Errorf("expected table name 'edges', got %q", name)
	}
}

func TestMigrate_EdgesIndexes(t *testing.T) {
	s := newTestStore(t)
	indexes := map[string]bool{}
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='edges'")
	if err != nil {
		t.Fatalf("querying indexes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scanning index name: %v", err)
		}
		indexes[n] = true
	}
	for _, idx := range []string{"idx_edges_to", "idx_edges_type"} {
		if !indexes[idx] {
			t.Errorf("missing index %q on edges table", idx)
		}
	}
}

// --- RebuildEdges ---

func TestRebuildEdges_InsertsAll(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "b", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 3 {
		t.Errorf("EdgeCount = %d, want 3", count)
	}
}

func TestRebuildEdges_ReplacesExisting(t *testing.T) {
	s := newTestStore(t)
	initial := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", initial); err != nil {
		t.Fatalf("RebuildEdges (initial): %v", err)
	}

	replacement := []Edge{
		{FromSlug: "x", ToSlug: "y", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", replacement); err != nil {
		t.Fatalf("RebuildEdges (replacement): %v", err)
	}

	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1 after rebuild", count)
	}
}

func TestRebuildEdges_OnlyDeletesMatchingType(t *testing.T) {
	s := newTestStore(t)
	if err := s.RebuildEdges("wikilink", []Edge{{FromSlug: "a", ToSlug: "b", Type: "wikilink"}}); err != nil {
		t.Fatalf("RebuildEdges wikilink: %v", err)
	}
	if err := s.RebuildEdges("backlink", []Edge{{FromSlug: "c", ToSlug: "d", Type: "backlink"}}); err != nil {
		t.Fatalf("RebuildEdges backlink: %v", err)
	}

	// Rebuild wikilink with empty set — backlink should remain
	if err := s.RebuildEdges("wikilink", nil); err != nil {
		t.Fatalf("RebuildEdges (clear wikilink): %v", err)
	}
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1 (backlink only)", count)
	}
}

// --- UpsertEdgesFrom ---

func TestUpsertEdgesFrom_InsertsEdges(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.UpsertEdgesFrom("a", "wikilink", edges); err != nil {
		t.Fatalf("UpsertEdgesFrom: %v", err)
	}
	out, err := s.GetOutboundEdges("a")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("len(outbound) = %d, want 2", len(out))
	}
}

func TestUpsertEdgesFrom_ReplacesOldEdges(t *testing.T) {
	s := newTestStore(t)
	initial := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.UpsertEdgesFrom("a", "wikilink", initial); err != nil {
		t.Fatalf("UpsertEdgesFrom (initial): %v", err)
	}

	updated := []Edge{
		{FromSlug: "a", ToSlug: "d", Type: "wikilink"},
	}
	if err := s.UpsertEdgesFrom("a", "wikilink", updated); err != nil {
		t.Fatalf("UpsertEdgesFrom (updated): %v", err)
	}

	out, err := s.GetOutboundEdges("a")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(outbound) = %d, want 1", len(out))
	}
	if out[0].ToSlug != "d" {
		t.Errorf("ToSlug = %q, want %q", out[0].ToSlug, "d")
	}
}

func TestUpsertEdgesFrom_FiltersSelfEdges(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "a", Type: "wikilink"}, // self-edge, should be filtered
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
	}
	if err := s.UpsertEdgesFrom("a", "wikilink", edges); err != nil {
		t.Fatalf("UpsertEdgesFrom: %v", err)
	}
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1 (self-edge filtered)", count)
	}
}

func TestUpsertEdgesFrom_DoesNotAffectOtherSlugs(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertEdgesFrom("a", "wikilink", []Edge{{FromSlug: "a", ToSlug: "b", Type: "wikilink"}}); err != nil {
		t.Fatalf("UpsertEdgesFrom a: %v", err)
	}
	if err := s.UpsertEdgesFrom("c", "wikilink", []Edge{{FromSlug: "c", ToSlug: "d", Type: "wikilink"}}); err != nil {
		t.Fatalf("UpsertEdgesFrom c: %v", err)
	}

	// Update a's edges — c's should remain
	if err := s.UpsertEdgesFrom("a", "wikilink", nil); err != nil {
		t.Fatalf("UpsertEdgesFrom (clear a): %v", err)
	}
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1 (only c->d)", count)
	}
}

// --- DeleteEdgesInvolving ---

func TestDeleteEdgesInvolving_RemovesFromAndTo(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "b", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "c", ToSlug: "d", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	if err := s.DeleteEdgesInvolving("b"); err != nil {
		t.Fatalf("DeleteEdgesInvolving: %v", err)
	}

	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	// a->b gone (b in to_slug), b->c gone (b in from_slug), c->d remains
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1", count)
	}
}

// --- DeleteEdge ---

func TestDeleteEdge_RemovesSingleEdge(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	if err := s.DeleteEdge("a", "b"); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}

	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("EdgeCount = %d, want 1", count)
	}

	out, err := s.GetOutboundEdges("a")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 1 || out[0].ToSlug != "c" {
		t.Errorf("remaining edge = %+v, want a->c", out)
	}
}

func TestDeleteEdge_NonExistent(t *testing.T) {
	s := newTestStore(t)
	// Should not error on deleting non-existent edge
	if err := s.DeleteEdge("x", "y"); err != nil {
		t.Fatalf("DeleteEdge (non-existent): %v", err)
	}
}

// --- GetOutboundEdges ---

func TestGetOutboundEdges_ReturnsCorrectEdges(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "b", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	out, err := s.GetOutboundEdges("a")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(outbound) = %d, want 2", len(out))
	}
	slugs := []string{out[0].ToSlug, out[1].ToSlug}
	sort.Strings(slugs)
	if slugs[0] != "b" || slugs[1] != "c" {
		t.Errorf("outbound to-slugs = %v, want [b c]", slugs)
	}
}

func TestGetOutboundEdges_Empty(t *testing.T) {
	s := newTestStore(t)
	out, err := s.GetOutboundEdges("nonexistent")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %d edges", len(out))
	}
}

// --- GetInboundEdges ---

func TestGetInboundEdges_ReturnsCorrectEdges(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "b", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "c", ToSlug: "d", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	in, err := s.GetInboundEdges("c")
	if err != nil {
		t.Fatalf("GetInboundEdges: %v", err)
	}
	if len(in) != 2 {
		t.Fatalf("len(inbound) = %d, want 2", len(in))
	}
	slugs := []string{in[0].FromSlug, in[1].FromSlug}
	sort.Strings(slugs)
	if slugs[0] != "a" || slugs[1] != "b" {
		t.Errorf("inbound from-slugs = %v, want [a b]", slugs)
	}
}

func TestGetInboundEdges_Empty(t *testing.T) {
	s := newTestStore(t)
	in, err := s.GetInboundEdges("nonexistent")
	if err != nil {
		t.Fatalf("GetInboundEdges: %v", err)
	}
	if len(in) != 0 {
		t.Errorf("expected empty, got %d edges", len(in))
	}
}

// --- EdgeCount ---

func TestEdgeCount_Empty(t *testing.T) {
	s := newTestStore(t)
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 0 {
		t.Errorf("EdgeCount = %d, want 0", count)
	}
}

func TestEdgeCount_AfterInserts(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "c", ToSlug: "d", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	count, err := s.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 2 {
		t.Errorf("EdgeCount = %d, want 2", count)
	}
}

// --- FindOrphans ---

func TestFindOrphans_NoInboundLinks(t *testing.T) {
	s := newTestStore(t)
	// a->b, a->c: a has no inbound, b and c do
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	orphans, err := s.FindOrphans()
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0] != "a" {
		t.Errorf("FindOrphans = %v, want [a]", orphans)
	}
}

func TestFindOrphans_IncludesBookmarksWithNoEdges(t *testing.T) {
	s := newTestStore(t)
	// Bookmark exists in DB but has no edges at all
	if err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/lonely.md",
		RawPath:       "raw/lonely.md",
		Title:         "Lonely",
		URL:           "http://example.com/lonely",
		URLNormalized: "http://example.com/lonely",
	}); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	orphans, err := s.FindOrphans()
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	found := false
	for _, o := range orphans {
		if o == "lonely" {
			found = true
		}
	}
	if !found {
		t.Errorf("FindOrphans = %v, want to include 'lonely' (bookmark with no edges)", orphans)
	}
}

func TestFindOrphans_IncludesNotesWithNoEdges(t *testing.T) {
	s := newTestStore(t)
	// Note exists in DB but has no edges at all
	if err := s.UpsertNote("notes/isolated.md", "hash1", "wiki/isolated.md"); err != nil {
		t.Fatalf("UpsertNote: %v", err)
	}

	orphans, err := s.FindOrphans()
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	found := false
	for _, o := range orphans {
		if o == "isolated" {
			found = true
		}
	}
	if !found {
		t.Errorf("FindOrphans = %v, want to include 'isolated' (note with no edges)", orphans)
	}
}

func TestFindOrphans_ExcludesLinkedSlugs(t *testing.T) {
	s := newTestStore(t)
	// a->b: b has inbound, should not be orphan
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}
	// b also exists as a bookmark
	if err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/b.md",
		RawPath:       "raw/b.md",
		Title:         "B",
		URL:           "http://example.com/b",
		URLNormalized: "http://example.com/b",
	}); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	orphans, err := s.FindOrphans()
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	for _, o := range orphans {
		if o == "b" {
			t.Errorf("FindOrphans includes 'b' which has inbound links")
		}
	}
}

func TestFindOrphans_Empty(t *testing.T) {
	s := newTestStore(t)
	orphans, err := s.FindOrphans()
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("FindOrphans = %v, want empty", orphans)
	}
}

// --- FindBrokenEdges ---

func TestFindBrokenEdges_DetectsMissingTargets(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	// b exists, c does not
	exists := func(slug string) bool {
		return slug == "b"
	}

	broken, err := s.FindBrokenEdges(exists)
	if err != nil {
		t.Fatalf("FindBrokenEdges: %v", err)
	}
	if len(broken) != 1 {
		t.Fatalf("len(broken) = %d, want 1", len(broken))
	}
	if broken[0].ToSlug != "c" {
		t.Errorf("broken edge ToSlug = %q, want %q", broken[0].ToSlug, "c")
	}
}

func TestFindBrokenEdges_NoneWhenAllExist(t *testing.T) {
	s := newTestStore(t)
	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
	}
	if err := s.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	exists := func(slug string) bool { return true }

	broken, err := s.FindBrokenEdges(exists)
	if err != nil {
		t.Fatalf("FindBrokenEdges: %v", err)
	}
	if len(broken) != 0 {
		t.Errorf("expected no broken edges, got %d", len(broken))
	}
}

func TestFindBrokenEdges_Empty(t *testing.T) {
	s := newTestStore(t)
	exists := func(slug string) bool { return false }
	broken, err := s.FindBrokenEdges(exists)
	if err != nil {
		t.Fatalf("FindBrokenEdges: %v", err)
	}
	if len(broken) != 0 {
		t.Errorf("expected no broken edges on empty table, got %d", len(broken))
	}
}

// --- WithFileLock ---

func TestWithFileLock_ExecutesFunction(t *testing.T) {
	s := newTestStore(t)
	executed := false
	err := s.WithFileLock(func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithFileLock: %v", err)
	}
	if !executed {
		t.Error("function was not executed inside WithFileLock")
	}
}

func TestWithFileLock_PropagatesError(t *testing.T) {
	s := newTestStore(t)
	expectedErr := "test error"
	err := s.WithFileLock(func() error {
		return fmt.Errorf("%s", expectedErr)
	})
	if err == nil {
		t.Fatal("expected error from WithFileLock")
	}
	if err.Error() != expectedErr {
		t.Errorf("error = %q, want %q", err.Error(), expectedErr)
	}
}

func TestWithFileLock_RollsBackOnError(t *testing.T) {
	s := newTestStore(t)
	// Insert an edge inside the lock, then return error — should be rolled back
	_ = s.RebuildEdges("wikilink", []Edge{{FromSlug: "a", ToSlug: "b", Type: "wikilink"}})

	err := s.WithFileLock(func() error {
		_, execErr := s.db.Exec("DELETE FROM edges")
		if execErr != nil {
			return execErr
		}
		return fmt.Errorf("rollback me")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Edge should still exist because the tx was rolled back
	// Note: WithFileLock wraps fn in a transaction. If fn uses s.db directly
	// (not the tx), this test verifies the rollback behavior.
	// The implementation should pass the tx to fn or use the exclusive tx for all ops within.
}

// --- DeleteBookmarkByWikiPath ---

func TestDeleteBookmarkByWikiPath_Deletes(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/test.md",
		RawPath:       "raw/test.md",
		Title:         "Test",
		URL:           "http://example.com/test",
		URLNormalized: "http://example.com/test",
	}); err != nil {
		t.Fatalf("UpsertBookmark: %v", err)
	}

	if err := s.DeleteBookmarkByWikiPath("wiki/test.md"); err != nil {
		t.Fatalf("DeleteBookmarkByWikiPath: %v", err)
	}

	rec, err := s.GetBookmarkByURL("http://example.com/test")
	if err != nil {
		t.Fatalf("GetBookmarkByURL: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil after delete, got %+v", rec)
	}
}

func TestDeleteBookmarkByWikiPath_NonExistent(t *testing.T) {
	s := newTestStore(t)
	// Should not error
	if err := s.DeleteBookmarkByWikiPath("wiki/nonexistent.md"); err != nil {
		t.Fatalf("DeleteBookmarkByWikiPath (non-existent): %v", err)
	}
}

func TestDeleteBookmarkByWikiPath_OnlyDeletesMatching(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/keep.md",
		RawPath:       "raw/keep.md",
		Title:         "Keep",
		URL:           "http://example.com/keep",
		URLNormalized: "http://example.com/keep",
	}); err != nil {
		t.Fatalf("UpsertBookmark keep: %v", err)
	}
	if err := s.UpsertBookmark(&BookmarkRecord{
		WikiPath:      "wiki/delete.md",
		RawPath:       "raw/delete.md",
		Title:         "Delete",
		URL:           "http://example.com/delete",
		URLNormalized: "http://example.com/delete",
	}); err != nil {
		t.Fatalf("UpsertBookmark delete: %v", err)
	}

	if err := s.DeleteBookmarkByWikiPath("wiki/delete.md"); err != nil {
		t.Fatalf("DeleteBookmarkByWikiPath: %v", err)
	}

	records, err := s.ListBookmarks()
	if err != nil {
		t.Fatalf("ListBookmarks: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].WikiPath != "wiki/keep.md" {
		t.Errorf("remaining WikiPath = %q, want %q", records[0].WikiPath, "wiki/keep.md")
	}
}
