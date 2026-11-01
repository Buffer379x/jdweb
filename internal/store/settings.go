package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// GetJSON decodes the setting key into v. A missing key returns ErrNotFound.
func (db *DB) GetJSON(ctx context.Context, key string, v any) error {
	var raw string
	err := db.R.QueryRowContext(ctx, "SELECT value FROM setting WHERE key = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), v)
}

// SetJSON stores v as JSON under key, replacing any previous value.
func (db *DB) SetJSON(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx,
		"INSERT INTO setting(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, string(raw))
	return err
}

// DeleteSetting removes key. Deleting a missing key is not an error.
func (db *DB) DeleteSetting(ctx context.Context, key string) error {
	_, err := db.W.ExecContext(ctx, "DELETE FROM setting WHERE key = ?", key)
	return err
}
