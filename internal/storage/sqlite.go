package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var ErrNotFound = errors.New("record not found")
var ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
var ErrIdempotencyInProgress = errors.New("idempotent verification request is already in progress")

type VerificationRequestState struct {
	Complete bool
	Payload  string
}

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
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}

	// Single connection avoids concurrent write races with go-sqlite3; busy
	// timeout lets short-lived lock contention wait instead of failing immediately.
	dsn := path
	if !strings.Contains(path, "?") {
		dsn = path + "?_busy_timeout=5000"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = db.Close()
			return nil, fmt.Errorf("restrict sqlite file mode: %w", err)
		}
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
		`CREATE TABLE IF NOT EXISTS verification_requests (
			idempotency_key TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('in_progress', 'complete')),
			payload TEXT,
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
			`INSERT INTO verification_evidence (id, payload, created_at) VALUES (?, ?, ?)`,
			id, payload, time.Now().UTC().Format(time.RFC3339Nano),
		)
		return err
	})
}

func (s *Store) StartVerificationRequest(key, requestHash string) (VerificationRequestState, error) {
	if key == "" || requestHash == "" {
		return VerificationRequestState{}, errors.New("idempotency key and request hash are required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.DB.Exec(
		`INSERT OR IGNORE INTO verification_requests (idempotency_key, request_hash, state, created_at)
		 VALUES (?, ?, 'in_progress', ?)`,
		key, requestHash, now,
	)
	if err != nil {
		return VerificationRequestState{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return VerificationRequestState{}, err
	}
	if inserted == 1 {
		return VerificationRequestState{}, nil
	}

	var savedHash, state string
	var payload sql.NullString
	if err := s.DB.QueryRow(
		`SELECT request_hash, state, payload FROM verification_requests WHERE idempotency_key = ?`,
		key,
	).Scan(&savedHash, &state, &payload); err != nil {
		return VerificationRequestState{}, err
	}
	if savedHash != requestHash {
		return VerificationRequestState{}, ErrIdempotencyConflict
	}
	if state == "in_progress" {
		return VerificationRequestState{}, ErrIdempotencyInProgress
	}
	if !payload.Valid {
		return VerificationRequestState{}, errors.New("completed verification request has no evidence payload")
	}
	return VerificationRequestState{Complete: true, Payload: payload.String}, nil
}

func (s *Store) CompleteVerificationRequest(key, requestHash, id, payload string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO verification_evidence (id, payload, created_at) VALUES (?, ?, ?)`,
			id, payload, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
		result, err := tx.Exec(
			`UPDATE verification_requests SET state = 'complete', payload = ?
			 WHERE idempotency_key = ? AND request_hash = ? AND state = 'in_progress'`,
			payload, key, requestHash,
		)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return errors.New("verification request reservation is missing or no longer active")
		}
		return nil
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
