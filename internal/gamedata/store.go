package gamedata

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// ErrNoSnapshot is returned before the first successful sync.
var ErrNoSnapshot = errors.New("gamedata: no snapshot has been synced yet")

// Store holds the active snapshot and swaps it atomically.
//
// A sync writes a new version alongside the old one and flips a pointer, so a
// request in flight keeps reading a consistent snapshot and a bad upstream
// commit is rolled back by activating the previous row.
type Store struct {
	DB      *sql.DB
	current atomic.Pointer[Snapshot]
}

// NewStore returns a store backed by conn.
func NewStore(conn *sql.DB) *Store { return &Store{DB: conn} }

// Current returns the active snapshot.
func (s *Store) Current() (*Snapshot, error) {
	if snap := s.current.Load(); snap != nil {
		return snap, nil
	}
	return nil, ErrNoSnapshot
}

// Load reads the active snapshot from the database into memory.
func (s *Store) Load() error {
	var payload []byte
	err := s.DB.QueryRow(`SELECT payload FROM gamedata_versions WHERE active = 1 LIMIT 1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSnapshot
	}
	if err != nil {
		return err
	}
	snap, err := decode(payload)
	if err != nil {
		return err
	}
	s.current.Store(snap)
	return nil
}

// Save stores a snapshot and makes it active.
func (s *Store) Save(snap *Snapshot) error {
	if snap.Version == "" {
		return fmt.Errorf("gamedata: snapshot has no version")
	}
	payload, err := encode(snap)
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO gamedata_versions (version, payload, active) VALUES (?, ?, 0)
		 ON CONFLICT(version) DO UPDATE SET payload = excluded.payload, imported_at = datetime('now')`,
		snap.Version, payload,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE gamedata_versions SET active = 0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE gamedata_versions SET active = 1 WHERE version = ?`, snap.Version); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.current.Store(snap)
	return nil
}

// Versions lists the stored snapshots, newest first.
func (s *Store) Versions() ([]VersionInfo, error) {
	rows, err := s.DB.Query(
		`SELECT version, active, imported_at FROM gamedata_versions ORDER BY imported_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VersionInfo
	for rows.Next() {
		var v VersionInfo
		if err := rows.Scan(&v.Version, &v.Active, &v.ImportedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VersionInfo describes a stored snapshot.
type VersionInfo struct {
	Version    string `json:"version"`
	Active     bool   `json:"active"`
	ImportedAt string `json:"importedAt"`
}

// Activate rolls back (or forward) to a stored version.
func (s *Store) Activate(version string) error {
	var payload []byte
	if err := s.DB.QueryRow(
		`SELECT payload FROM gamedata_versions WHERE version = ?`, version).Scan(&payload); err != nil {
		return err
	}
	snap, err := decode(payload)
	if err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE gamedata_versions SET active = (version = ?)`, version); err != nil {
		return err
	}
	s.current.Store(snap)
	return nil
}

// The snapshot is a few megabytes of JSON; gzip in the blob keeps the SQLite
// file small enough that a backup is still a single manageable download.
func encode(snap *Snapshot) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(snap); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decode(payload []byte) (*Snapshot, error) {
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("gamedata: decompress snapshot: %w", err)
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("gamedata: decode snapshot: %w", err)
	}
	return &snap, nil
}
