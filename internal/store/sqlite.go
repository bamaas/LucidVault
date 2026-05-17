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
			source_id INTEGER UNIQUE,
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
	`)
	if err != nil {
		return fmt.Errorf("executing migrations: %w", err)
	}
	return nil
}

func (s *Store) IsProcessedBySourceID(sourceID int) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE source_id = ?", sourceID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking source_id %d: %w", sourceID, err)
	}
	return count > 0, nil
}

func (s *Store) IsProcessedByURL(normalizedURL string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM bookmarks WHERE url_normalized = ?", normalizedURL).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking url %q: %w", normalizedURL, err)
	}
	return count > 0, nil
}

func (s *Store) SaveBookmark(rec *BookmarkRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO bookmarks (source_id, wiki_path, raw_path, title, url, url_normalized, processed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.SourceID, rec.WikiPath, rec.RawPath, rec.Title, rec.URL, rec.URLNormalized,
		rec.ProcessedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("saving bookmark %d: %w", rec.SourceID, err)
	}
	return nil
}

func (s *Store) GetBookmarkBySourceID(sourceID int) (*BookmarkRecord, error) {
	row := s.db.QueryRow(
		`SELECT source_id, wiki_path, raw_path, title, url, url_normalized, processed_at
		 FROM bookmarks WHERE source_id = ? LIMIT 1`, sourceID,
	)

	var rec BookmarkRecord
	var processedAt string
	err := row.Scan(&rec.SourceID, &rec.WikiPath, &rec.RawPath, &rec.Title, &rec.URL, &rec.URLNormalized, &processedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying source_id %d: %w", sourceID, err)
	}

	rec.ProcessedAt, err = time.Parse(time.RFC3339, processedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing processed_at for source_id %d: %w", sourceID, err)
	}

	return &rec, nil
}

func (s *Store) UpdateBookmarkWikiPath(sourceID int, wikiPath string) error {
	_, err := s.db.Exec(
		`UPDATE bookmarks SET wiki_path = ?, processed_at = ? WHERE source_id = ?`,
		wikiPath, time.Now().UTC().Format(time.RFC3339), sourceID,
	)
	if err != nil {
		return fmt.Errorf("updating bookmark %d wiki_path: %w", sourceID, err)
	}
	return nil
}

func (s *Store) DeleteBySourceID(sourceID int) error {
	_, err := s.db.Exec("DELETE FROM bookmarks WHERE source_id = ?", sourceID)
	if err != nil {
		return fmt.Errorf("deleting source_id %d: %w", sourceID, err)
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

func (s *Store) UpsertNote(path, contentHash string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO notes (path, content_hash, last_processed) VALUES (?, ?, ?)`,
		path, contentHash, time.Now().UTC().Format(time.RFC3339),
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
	rows, err := s.db.Query("SELECT path, content_hash, last_processed FROM notes")
	if err != nil {
		return nil, fmt.Errorf("listing notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []NoteRecord
	for rows.Next() {
		var rec NoteRecord
		var lastProcessed string
		if err := rows.Scan(&rec.Path, &rec.ContentHash, &lastProcessed); err != nil {
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
	return s.db.Close()
}
