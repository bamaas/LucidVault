package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type NoteRecord struct {
	Path          string
	ContentHash   string
	WikiPath      string
	LastProcessed time.Time
}

type BookmarkRecord struct {
	SourceID      int
	WikiPath      string
	RawPath       string
	Title         string
	URL           string
	URLNormalized string
	ProcessedAt   time.Time
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS bookmarks (
			id INTEGER PRIMARY KEY,
			source_id INTEGER,
			wiki_path TEXT,
			raw_path TEXT,
			title TEXT,
			url TEXT,
			url_normalized TEXT,
			processed_at TEXT
		);
		CREATE TABLE IF NOT EXISTS notes (
			path TEXT PRIMARY KEY,
			content_hash TEXT NOT NULL,
			last_processed TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_bookmarks_url_normalized ON bookmarks(url_normalized);
		CREATE TABLE IF NOT EXISTS edges (
			from_slug TEXT NOT NULL,
			to_slug   TEXT NOT NULL,
			type      TEXT NOT NULL DEFAULT 'wikilink',
			PRIMARY KEY (from_slug, to_slug, type)
		);
		CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_slug);
		CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);
	`)
	if err != nil {
		return fmt.Errorf("executing migrations: %w", err)
	}

	// Add wiki_path column to notes table (idempotent — ignores if already exists)
	_, _ = s.db.Exec(`ALTER TABLE notes ADD COLUMN wiki_path TEXT NOT NULL DEFAULT ''`)

	// Read current schema version to avoid running destructive migrations on every startup.
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading user_version: %w", err)
	}

	if version < 1 {
		// Deduplicate url_normalized before creating unique index (keeps newest row).
		// Exclude NULL values to avoid grouping unrelated rows.
		_, err = s.db.Exec(`
			DELETE FROM bookmarks WHERE url_normalized IS NOT NULL AND id NOT IN (
				SELECT MAX(id) FROM bookmarks WHERE url_normalized IS NOT NULL GROUP BY url_normalized
			)
		`)
		if err != nil {
			return fmt.Errorf("deduplicating bookmarks: %w", err)
		}

		// Add unique index on url_normalized for upsert support.
		// Drop the old non-unique index first, then create unique one.
		_, err = s.db.Exec(`
			DROP INDEX IF EXISTS idx_bookmarks_url_normalized;
			CREATE UNIQUE INDEX IF NOT EXISTS idx_bookmarks_url_normalized ON bookmarks(url_normalized);
		`)
		if err != nil {
			return fmt.Errorf("creating unique url index: %w", err)
		}

		// Mark migration as complete so dedup does not run again.
		if _, err := s.db.Exec("PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("setting user_version: %w", err)
		}
	}

	return nil
}

func (s *Store) IsProcessedByURL(normalizedURL string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE url_normalized = ?", normalizedURL).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking url %q: %w", normalizedURL, err)
	}
	return count > 0, nil
}

func (s *Store) GetBookmarkByURL(normalizedURL string) (*BookmarkRecord, error) {
	row := s.db.QueryRow(
		`SELECT source_id, wiki_path, raw_path, title, url, url_normalized, processed_at
		 FROM bookmarks WHERE url_normalized = ? LIMIT 1`, normalizedURL,
	)

	var rec BookmarkRecord
	var processedAt string
	err := row.Scan(&rec.SourceID, &rec.WikiPath, &rec.RawPath, &rec.Title, &rec.URL, &rec.URLNormalized, &processedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying url %q: %w", normalizedURL, err)
	}

	rec.ProcessedAt, err = time.Parse(time.RFC3339, processedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing processed_at for url %q: %w", normalizedURL, err)
	}

	return &rec, nil
}

func (s *Store) UpsertBookmark(rec *BookmarkRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO bookmarks (source_id, wiki_path, raw_path, title, url, url_normalized, processed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(url_normalized) DO UPDATE SET
		   wiki_path = excluded.wiki_path,
		   raw_path = excluded.raw_path,
		   title = excluded.title,
		   url = excluded.url,
		   processed_at = excluded.processed_at`,
		rec.SourceID, rec.WikiPath, rec.RawPath, rec.Title, rec.URL, rec.URLNormalized,
		rec.ProcessedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting bookmark %q: %w", rec.URLNormalized, err)
	}
	return nil
}

func (s *Store) GetNoteHash(path string) (string, error) {
	var hash string
	err := s.db.QueryRow("SELECT content_hash FROM notes WHERE path = ?", path).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying note hash for %q: %w", path, err)
	}
	return hash, nil
}

func (s *Store) UpsertNote(path, contentHash, wikiPath string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO notes (path, content_hash, wiki_path, last_processed) VALUES (?, ?, ?, ?)`,
		path, contentHash, wikiPath, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upserting note %q: %w", path, err)
	}
	return nil
}

func (s *Store) DeleteNote(path string) error {
	_, err := s.db.Exec("DELETE FROM notes WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("deleting note %q: %w", path, err)
	}
	return nil
}

func (s *Store) ListBookmarks() ([]BookmarkRecord, error) {
	rows, err := s.db.Query("SELECT source_id, wiki_path, raw_path, title, url, url_normalized, processed_at FROM bookmarks")
	if err != nil {
		return nil, fmt.Errorf("listing bookmarks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []BookmarkRecord
	for rows.Next() {
		var rec BookmarkRecord
		var processedAt string
		if err := rows.Scan(&rec.SourceID, &rec.WikiPath, &rec.RawPath, &rec.Title, &rec.URL, &rec.URLNormalized, &processedAt); err != nil {
			return nil, fmt.Errorf("scanning bookmark row: %w", err)
		}
		rec.ProcessedAt, err = time.Parse(time.RFC3339, processedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing processed_at for bookmark %d: %w", rec.SourceID, err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating bookmark rows: %w", err)
	}
	return records, nil
}

func (s *Store) ListNotes() ([]NoteRecord, error) {
	rows, err := s.db.Query("SELECT path, content_hash, wiki_path, last_processed FROM notes")
	if err != nil {
		return nil, fmt.Errorf("listing notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []NoteRecord
	for rows.Next() {
		var rec NoteRecord
		var lastProcessed string
		if err := rows.Scan(&rec.Path, &rec.ContentHash, &rec.WikiPath, &lastProcessed); err != nil {
			return nil, fmt.Errorf("scanning note row: %w", err)
		}
		rec.LastProcessed, err = time.Parse(time.RFC3339, lastProcessed)
		if err != nil {
			return nil, fmt.Errorf("parsing last_processed for note %q: %w", rec.Path, err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating note rows: %w", err)
	}
	return records, nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing database: %w", err)
	}
	return nil
}
