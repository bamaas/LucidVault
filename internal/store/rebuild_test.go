package store

import (
	"path/filepath"
	"testing"
)

func TestSlugFromWikiRelPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"wiki/foo.md", "foo"},
		{"wiki/notes/bar.md", "notes/bar"},
		{"wiki/deep/nested/baz.md", "deep/nested/baz"},
		// Edge cases
		{"foo.md", "foo"},        // no wiki/ prefix
		{"wiki/bar", "bar"},      // no .md extension
		{"", ""},                 // empty string
		{"wiki/.md", ""},         // hidden file style (empty slug)
	}
	for _, tc := range tests {
		got := SlugFromWikiRelPath(tc.input)
		if got != tc.want {
			t.Errorf("SlugFromWikiRelPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRebuildEdges_Integration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert some edges manually
	edges := []Edge{
		{FromSlug: "page-a", ToSlug: "page-b", Type: "wikilink"},
		{FromSlug: "page-a", ToSlug: "page-c", Type: "wikilink"},
	}
	if err := db.RebuildEdges("wikilink", edges); err != nil {
		t.Fatalf("RebuildEdges: %v", err)
	}

	count, err := db.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 edges, got %d", count)
	}

	// Rebuild with new set — should replace
	newEdges := []Edge{
		{FromSlug: "page-x", ToSlug: "page-y", Type: "wikilink"},
	}
	if err := db.RebuildEdges("wikilink", newEdges); err != nil {
		t.Fatalf("RebuildEdges (second): %v", err)
	}

	count, err = db.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 edge after rebuild, got %d", count)
	}
}

func TestRebuildEdges_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	edges := []Edge{
		{FromSlug: "a", ToSlug: "b", Type: "wikilink"},
		{FromSlug: "a", ToSlug: "c", Type: "wikilink"},
		{FromSlug: "b", ToSlug: "c", Type: "wikilink"},
	}

	// Run twice with same data
	for i := 0; i < 2; i++ {
		if err := db.RebuildEdges("wikilink", edges); err != nil {
			t.Fatalf("RebuildEdges (iteration %d): %v", i, err)
		}
	}

	count, err := db.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 edges after idempotent rebuild, got %d", count)
	}
}

func TestUpsertEdgesFrom_Incremental(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set up initial edges from two different pages
	if err := db.UpsertEdgesFrom("page-a", "wikilink", []Edge{
		{FromSlug: "page-a", ToSlug: "page-b", Type: "wikilink"},
		{FromSlug: "page-a", ToSlug: "page-c", Type: "wikilink"},
	}); err != nil {
		t.Fatalf("UpsertEdgesFrom page-a: %v", err)
	}
	if err := db.UpsertEdgesFrom("page-b", "wikilink", []Edge{
		{FromSlug: "page-b", ToSlug: "page-c", Type: "wikilink"},
	}); err != nil {
		t.Fatalf("UpsertEdgesFrom page-b: %v", err)
	}

	// Now update page-a edges — should replace only page-a's edges
	if err := db.UpsertEdgesFrom("page-a", "wikilink", []Edge{
		{FromSlug: "page-a", ToSlug: "page-d", Type: "wikilink"},
	}); err != nil {
		t.Fatalf("UpsertEdgesFrom page-a (update): %v", err)
	}

	// page-a should now only link to page-d
	outA, err := db.GetOutboundEdges("page-a")
	if err != nil {
		t.Fatalf("GetOutboundEdges page-a: %v", err)
	}
	if len(outA) != 1 || outA[0].ToSlug != "page-d" {
		t.Errorf("expected page-a -> [page-d], got %v", outA)
	}

	// page-b edges should be untouched
	outB, err := db.GetOutboundEdges("page-b")
	if err != nil {
		t.Fatalf("GetOutboundEdges page-b: %v", err)
	}
	if len(outB) != 1 || outB[0].ToSlug != "page-c" {
		t.Errorf("expected page-b -> [page-c], got %v", outB)
	}
}

func TestUpsertEdgesFrom_EmptyClearsOutbound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert edges from page-a
	if err := db.UpsertEdgesFrom("page-a", "wikilink", []Edge{
		{FromSlug: "page-a", ToSlug: "page-b", Type: "wikilink"},
		{FromSlug: "page-a", ToSlug: "page-c", Type: "wikilink"},
	}); err != nil {
		t.Fatalf("UpsertEdgesFrom: %v", err)
	}

	// Upsert with empty slice — should clear all outbound edges for page-a
	if err := db.UpsertEdgesFrom("page-a", "wikilink", nil); err != nil {
		t.Fatalf("UpsertEdgesFrom (empty): %v", err)
	}

	out, err := db.GetOutboundEdges("page-a")
	if err != nil {
		t.Fatalf("GetOutboundEdges: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected 0 outbound edges after empty upsert, got %d", len(out))
	}
}

func TestEdgeCount_EmptyTriggersRebuild(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = db.Close() }()

	count, err := db.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 edges in fresh db, got %d", count)
	}
}
