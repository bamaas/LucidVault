package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Edge represents a directional link between two vault pages.
type Edge struct {
	FromSlug string
	ToSlug   string
	Type     string
}

// RebuildEdges replaces all edges of the given type with the provided set.
// It deletes all existing edges matching edgeType, then bulk-inserts the new ones.
func (s *Store) RebuildEdges(edgeType string, edges []Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM edges WHERE type = ?", edgeType); err != nil {
		return fmt.Errorf("deleting edges of type %q: %w", edgeType, err)
	}

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO edges (from_slug, to_slug, type) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("preparing edge insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range edges {
		if _, err := stmt.Exec(e.FromSlug, e.ToSlug, e.Type); err != nil {
			return fmt.Errorf("inserting edge %q -> %q: %w", e.FromSlug, e.ToSlug, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing edge rebuild: %w", err)
	}
	return nil
}

// UpsertEdgesFrom replaces all outbound edges of the given type from fromSlug.
// Self-edges (fromSlug == toSlug) are filtered out.
func (s *Store) UpsertEdgesFrom(fromSlug, edgeType string, edges []Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM edges WHERE from_slug = ? AND type = ?", fromSlug, edgeType); err != nil {
		return fmt.Errorf("deleting edges from %q: %w", fromSlug, err)
	}

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO edges (from_slug, to_slug, type) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("preparing edge insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range edges {
		// Filter self-edges
		if e.FromSlug == e.ToSlug {
			continue
		}
		if _, err := stmt.Exec(e.FromSlug, e.ToSlug, e.Type); err != nil {
			return fmt.Errorf("inserting edge %q -> %q: %w", e.FromSlug, e.ToSlug, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing edge upsert: %w", err)
	}
	return nil
}

// DeleteEdgesInvolving removes all edges where slug appears as from_slug or to_slug.
func (s *Store) DeleteEdgesInvolving(slug string) error {
	_, err := s.db.Exec("DELETE FROM edges WHERE from_slug = ? OR to_slug = ?", slug, slug)
	if err != nil {
		return fmt.Errorf("deleting edges involving %q: %w", slug, err)
	}
	return nil
}

// DeleteEdge removes a single edge from fromSlug to toSlug (all types).
func (s *Store) DeleteEdge(fromSlug, toSlug string) error {
	_, err := s.db.Exec("DELETE FROM edges WHERE from_slug = ? AND to_slug = ?", fromSlug, toSlug)
	if err != nil {
		return fmt.Errorf("deleting edge %q -> %q: %w", fromSlug, toSlug, err)
	}
	return nil
}

// GetOutboundEdges returns all edges originating from the given slug.
func (s *Store) GetOutboundEdges(slug string) ([]Edge, error) {
	rows, err := s.db.Query("SELECT from_slug, to_slug, type FROM edges WHERE from_slug = ?", slug)
	if err != nil {
		return nil, fmt.Errorf("querying outbound edges for %q: %w", slug, err)
	}
	defer func() { _ = rows.Close() }()

	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.FromSlug, &e.ToSlug, &e.Type); err != nil {
			return nil, fmt.Errorf("scanning edge row: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating edge rows: %w", err)
	}
	return edges, nil
}

// GetInboundEdges returns all edges pointing to the given slug.
func (s *Store) GetInboundEdges(slug string) ([]Edge, error) {
	rows, err := s.db.Query("SELECT from_slug, to_slug, type FROM edges WHERE to_slug = ?", slug)
	if err != nil {
		return nil, fmt.Errorf("querying inbound edges for %q: %w", slug, err)
	}
	defer func() { _ = rows.Close() }()

	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.FromSlug, &e.ToSlug, &e.Type); err != nil {
			return nil, fmt.Errorf("scanning edge row: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating edge rows: %w", err)
	}
	return edges, nil
}

// EdgeCount returns the total number of edges in the store.
func (s *Store) EdgeCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM edges").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting edges: %w", err)
	}
	return count, nil
}

// FindOrphans returns slugs that have zero inbound edges.
// It considers:
// 1. Slugs in from_slug that never appear in to_slug (outbound-only nodes)
// 2. Bookmarks and notes in the DB that have no edges at all
func (s *Store) FindOrphans() ([]string, error) {
	// Find all slugs that appear in from_slug but not in to_slug
	query := `
		SELECT DISTINCT from_slug FROM edges
		WHERE from_slug NOT IN (SELECT DISTINCT to_slug FROM edges)
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("finding orphan edges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orphanSet := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scanning orphan slug: %w", err)
		}
		orphanSet[slug] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating orphan rows: %w", err)
	}

	// Find bookmarks with no edges at all
	bRows, err := s.db.Query(`
		SELECT wiki_path FROM bookmarks
		WHERE wiki_path IS NOT NULL AND wiki_path != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("querying bookmarks for orphans: %w", err)
	}
	defer func() { _ = bRows.Close() }()

	for bRows.Next() {
		var wikiPath string
		if err := bRows.Scan(&wikiPath); err != nil {
			return nil, fmt.Errorf("scanning bookmark wiki_path: %w", err)
		}
		slug := slugFromWikiPath(wikiPath)
		if slug == "" {
			continue
		}
		// TODO: optimize with set-based query to avoid N+1 (one COUNT per slug)
		var edgeCount int
		err := s.db.QueryRow(
			"SELECT COUNT(*) FROM edges WHERE from_slug = ? OR to_slug = ?", slug, slug,
		).Scan(&edgeCount)
		if err != nil {
			return nil, fmt.Errorf("checking edges for bookmark %q: %w", slug, err)
		}
		if edgeCount == 0 {
			orphanSet[slug] = true
		}
	}
	if err := bRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating bookmark rows: %w", err)
	}

	// Find notes with no edges at all
	nRows, err := s.db.Query(`
		SELECT wiki_path FROM notes
		WHERE wiki_path IS NOT NULL AND wiki_path != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("querying notes for orphans: %w", err)
	}
	defer func() { _ = nRows.Close() }()

	for nRows.Next() {
		var wikiPath string
		if err := nRows.Scan(&wikiPath); err != nil {
			return nil, fmt.Errorf("scanning note wiki_path: %w", err)
		}
		slug := slugFromWikiPath(wikiPath)
		if slug == "" {
			continue
		}
		var edgeCount int
		err := s.db.QueryRow(
			"SELECT COUNT(*) FROM edges WHERE from_slug = ? OR to_slug = ?", slug, slug,
		).Scan(&edgeCount)
		if err != nil {
			return nil, fmt.Errorf("checking edges for note %q: %w", slug, err)
		}
		if edgeCount == 0 {
			orphanSet[slug] = true
		}
	}
	if err := nRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating note rows: %w", err)
	}

	orphans := make([]string, 0, len(orphanSet))
	for slug := range orphanSet {
		orphans = append(orphans, slug)
	}
	sort.Strings(orphans)
	return orphans, nil
}

// ExpandGraph returns all slugs reachable from the seed slugs within the given
// number of hops, using a recursive CTE over the edges table.
// It traverses both directions (outbound and inbound edges).
// Seeds themselves are excluded from results. maxHops is capped at 5.
func (s *Store) ExpandGraph(seeds []string, maxHops int) ([]string, error) {
	if len(seeds) == 0 {
		return []string{}, nil
	}
	if maxHops > 5 {
		maxHops = 5
	}
	if maxHops < 1 {
		maxHops = 2
	}

	// Build seed VALUES clause and NOT IN placeholders
	valuesClauses := make([]string, len(seeds))
	notInPlaceholders := make([]string, len(seeds))
	args := make([]any, 0, len(seeds)*2+1)

	for i, seed := range seeds {
		valuesClauses[i] = "(?)"
		notInPlaceholders[i] = "?"
		args = append(args, seed)
	}

	// Add maxHops parameter
	args = append(args, maxHops)

	// Add seeds again for the NOT IN clause
	for _, seed := range seeds {
		args = append(args, seed)
	}

	// TODO: For dense graphs, the CTE can explore exponentially since UNION deduplicates
	// the final result but not intermediate paths. Consider iterative BFS with visited set
	// if performance becomes an issue.
	query := fmt.Sprintf(`
		WITH RECURSIVE reachable(slug, depth) AS (
			SELECT column1, 0
			FROM (VALUES %s)
			UNION
			SELECT CASE
				WHEN e.from_slug = r.slug THEN e.to_slug
				ELSE e.from_slug
			END, r.depth + 1
			FROM edges e
			JOIN reachable r ON (e.from_slug = r.slug OR e.to_slug = r.slug)
			WHERE r.depth < ?
		)
		SELECT DISTINCT slug FROM reachable
		WHERE slug NOT IN (%s)
		AND depth > 0
	`, strings.Join(valuesClauses, ", "), strings.Join(notInPlaceholders, ", "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("expanding graph from %v: %w", seeds, err)
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scanning reachable slug: %w", err)
		}
		result = append(result, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating reachable rows: %w", err)
	}

	if result == nil {
		result = []string{}
	}
	sort.Strings(result)
	return result, nil
}

// slugFromWikiPath extracts the slug from a wiki path like "wiki/my-slug.md" -> "my-slug".
func slugFromWikiPath(wikiPath string) string {
	base := filepath.Base(wikiPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// SlugFromWikiRelPath extracts a slug that preserves subdirectory structure relative to wiki/.
// e.g. "wiki/foo.md" -> "foo", "wiki/notes/bar.md" -> "notes/bar".
func SlugFromWikiRelPath(wikiRelPath string) string {
	// Strip "wiki/" prefix
	trimmed := strings.TrimPrefix(wikiRelPath, "wiki/")
	// Remove .md extension
	return strings.TrimSuffix(trimmed, filepath.Ext(trimmed))
}

// FindBrokenEdges returns edges where the target slug does not exist on disk.
// The exists function is called for each unique to_slug to check file presence.
func (s *Store) FindBrokenEdges(exists func(string) bool) ([]Edge, error) {
	rows, err := s.db.Query("SELECT from_slug, to_slug, type FROM edges")
	if err != nil {
		return nil, fmt.Errorf("querying edges for broken check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Cache exists results to avoid repeated disk checks for same slug
	existsCache := map[string]bool{}
	var broken []Edge

	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.FromSlug, &e.ToSlug, &e.Type); err != nil {
			return nil, fmt.Errorf("scanning edge row: %w", err)
		}
		found, ok := existsCache[e.ToSlug]
		if !ok {
			found = exists(e.ToSlug)
			existsCache[e.ToSlug] = found
		}
		if !found {
			broken = append(broken, e)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating edge rows: %w", err)
	}
	return broken, nil
}

// WithFileLock executes fn while holding a SQLite exclusive lock.
// This provides cross-process write safety: BEGIN EXCLUSIVE immediately acquires
// a write lock that blocks other writers (including other OS processes) until
// the transaction completes.
// Note: fn may use s.db for reads/writes — they run on a separate connection
// from the lock holder, but SQLite serializes them safely.
func (s *Store) WithFileLock(fn func() error) error {
	// Use a dedicated connection so BEGIN EXCLUSIVE is not managed by database/sql's
	// pool, which only supports BEGIN (deferred).
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection for lock: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("beginning exclusive transaction: %w", err)
	}

	if err := fn(); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("committing exclusive transaction: %w", err)
	}
	return nil
}

// DeleteBookmarkByWikiPath deletes a bookmark by its wiki_path column.
func (s *Store) DeleteBookmarkByWikiPath(wikiPath string) error {
	_, err := s.db.Exec("DELETE FROM bookmarks WHERE wiki_path = ?", wikiPath)
	if err != nil {
		return fmt.Errorf("deleting bookmark by wiki_path %q: %w", wikiPath, err)
	}
	return nil
}
