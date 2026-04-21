package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type SyncState struct {
	LastSourceID int
	LastSyncAt   time.Time
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
		CREATE TABLE IF NOT EXISTS sync_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			last_source_id INTEGER,
			last_sync_at TEXT
		);

		INSERT OR IGNORE INTO sync_state (id, last_source_id, last_sync_at)
		VALUES (1, 0, '1970-01-01T00:00:00Z');

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
	`)
	if err != nil {
		return fmt.Errorf("executing migrations: %w", err)
	}
	return nil
}

func (s *Store) GetSyncState() (*SyncState, error) {
	var lastID int
	var lastSync string
	err := s.db.QueryRow("SELECT last_source_id, last_sync_at FROM sync_state WHERE id = 1").
		Scan(&lastID, &lastSync)
	if err != nil {
		return nil, fmt.Errorf("querying sync state: %w", err)
	}

	t, err := time.Parse(time.RFC3339, lastSync)
	if err != nil {
		return nil, fmt.Errorf("parsing last_sync_at %q: %w", lastSync, err)
	}

	return &SyncState{LastSourceID: lastID, LastSyncAt: t}, nil
}

func (s *Store) UpdateSyncState(lastID int, syncAt time.Time) error {
	_, err := s.db.Exec(
		"UPDATE sync_state SET last_source_id = ?, last_sync_at = ? WHERE id = 1",
		lastID, syncAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("updating sync state: %w", err)
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

func (s *Store) Close() error {
	return s.db.Close()
}
