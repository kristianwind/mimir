package db

import (
	"context"
	"database/sql"
	"errors"
)

// Setting reads an instance setting. A missing key returns the empty string,
// which callers must treat as distinct from an explicit "0": the difference
// between "never chosen" and "deliberately switched off" is the whole reason
// this table is key/value.
func Setting(ctx context.Context, conn *sql.DB, key string) string {
	var v string
	err := conn.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		return ""
	}
	return v
}

// SetSetting writes an instance setting.
func SetSetting(ctx context.Context, conn *sql.DB, key, value string) error {
	_, err := conn.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value)
	return err
}
