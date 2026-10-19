package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("record not found")

// Record stores metadata and payload for persisted API objects.
type Record struct {
	ID        string
	Kind      string
	Name      string
	Path      string
	Payload   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store persists snapshots, evidence, and analysis results locally.
type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func migrate(db *sql.DB) error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`PRAGMA journal_mode = WAL;`,
		`CREATE TABLE IF NOT EXISTS analyses (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT,
			path TEXT,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT,
			path TEXT,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS reports (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS verification_evidence (
			id TEXT PRIMARY KEY,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_analyses_created_at ON analyses(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_created_at ON snapshots(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_created_at ON reports(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_verification_evidence_created_at ON verification_evidence(created_at DESC);`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("execute migration %q: %w", statement, err)
		}
	}
	return nil
}

func (s *Store) SaveAnalysis(id, kind, name, path, payload string) error {
	return s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := tx.Exec(`
			INSERT OR REPLACE INTO analyses (id, kind, name, path, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, kind, name, path, payload, now, now,
		)
		return err
	})
}

func (s *Store) SaveSnapshot(id, kind, name, path, payload string) error {
	return s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := tx.Exec(`
			INSERT OR REPLACE INTO snapshots (id, kind, name, path, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, kind, name, path, payload, now, now,
		)
		return err
	})
}

func (s *Store) SaveReport(id, kind, name, payload string) error {
	return s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := tx.Exec(`
			INSERT OR REPLACE INTO reports (id, kind, name, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, kind, name, payload, now, now,
		)
		return err
	})
}

func (s *Store) SaveVerificationEvidence(id, payload string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO verification_evidence (id, payload, created_at) VALUES (?, ?, ?)`,
			id, payload, time.Now().UTC().Format(time.RFC3339Nano),
		)
		return err
	})
}

func (s *Store) GetVerificationEvidence(id string) (string, error) {
	var payload string
	err := s.DB.QueryRow(`SELECT payload FROM verification_evidence WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return payload, err
}

func (s *Store) ListVerificationEvidence(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.Query(`SELECT payload FROM verification_evidence ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, rows.Err()
}

func (s *Store) GetAnalysis(id string) (Record, error) {
	return s.getRecord("analyses", id)
}

func (s *Store) GetSnapshot(id string) (Record, error) {
	return s.getRecord("snapshots", id)
}

func (s *Store) ListAnalyses(limit int) ([]Record, error) {
	return s.listRecords("analyses", limit)
}

func (s *Store) ListSnapshots(limit int) ([]Record, error) {
	return s.listRecords("snapshots", limit)
}

func (s *Store) LastAnalyses(limit int) ([]map[string]string, error) {
	records, err := s.ListAnalyses(limit)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]string{"id": record.ID, "kind": record.Kind, "name": record.Name, "path": record.Path, "payload": record.Payload})
	}
	return out, nil
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func ExampleUsage() string {
	return fmt.Sprintf("sqlite persistence ready for analysis snapshots and evidence; use local file storage in %s", filepath.Join(os.TempDir(), "pci"))
}

func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) getRecord(table, id string) (Record, error) {
	row := s.DB.QueryRow(fmt.Sprintf("SELECT id, kind, name, path, payload, created_at, updated_at FROM %s WHERE id = ?", table), id)
	var rec Record
	var createdAt, updatedAt string
	if err := row.Scan(&rec.ID, &rec.Kind, &rec.Name, &rec.Path, &rec.Payload, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, err
	}
	rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return rec, nil
}

func (s *Store) listRecords(table string, limit int) ([]Record, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.Query(fmt.Sprintf("SELECT id, kind, name, path, payload, created_at, updated_at FROM %s ORDER BY created_at DESC LIMIT ?", table), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Record, 0)
	for rows.Next() {
		var rec Record
		var createdAt, updatedAt string
		if err := rows.Scan(&rec.ID, &rec.Kind, &rec.Name, &rec.Path, &rec.Payload, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}
