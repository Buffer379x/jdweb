package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session is a login session. TokenHash is the hex SHA-256 of the cookie
// token; the token itself is never stored.
type Session struct {
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
}

const sessionCols = "token_hash, created_at, last_seen_at, expires_at, ifnull(user_agent, '')"

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var s Session
	var c, l, e int64
	if err := row.Scan(&s.TokenHash, &c, &l, &e, &s.UserAgent); err != nil {
		return Session{}, err
	}
	s.CreatedAt, s.LastSeenAt, s.ExpiresAt = time.Unix(c, 0), time.Unix(l, 0), time.Unix(e, 0)
	return s, nil
}

// CreateSession inserts s.
func (db *DB) CreateSession(ctx context.Context, s Session) error {
	_, err := db.W.ExecContext(ctx,
		"INSERT INTO session(token_hash, created_at, last_seen_at, expires_at, user_agent) VALUES (?,?,?,?,?)",
		s.TokenHash, s.CreatedAt.Unix(), s.LastSeenAt.Unix(), s.ExpiresAt.Unix(), s.UserAgent)
	return err
}

// GetSession returns the session with the given token hash, or ErrNotFound.
func (db *DB) GetSession(ctx context.Context, tokenHash string) (Session, error) {
	s, err := scanSession(db.R.QueryRowContext(ctx, "SELECT "+sessionCols+" FROM session WHERE token_hash = ?", tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return s, err
}

// TouchSession records activity and moves the expiry.
func (db *DB) TouchSession(ctx context.Context, tokenHash string, lastSeen, expires time.Time) error {
	_, err := db.W.ExecContext(ctx, "UPDATE session SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?",
		lastSeen.Unix(), expires.Unix(), tokenHash)
	return err
}

// ListSessions returns all sessions, most recently used first.
func (db *DB) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := db.R.QueryContext(ctx, "SELECT "+sessionCols+" FROM session ORDER BY last_seen_at DESC, token_hash")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSession removes one session. Deleting a missing one is not an error.
func (db *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := db.W.ExecContext(ctx, "DELETE FROM session WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteOtherSessions removes every session except keep and returns how many.
func (db *DB) DeleteOtherSessions(ctx context.Context, keep string) (int64, error) {
	res, err := db.W.ExecContext(ctx, "DELETE FROM session WHERE token_hash <> ?", keep)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteExpiredSessions removes sessions that expired before now.
func (db *DB) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := db.W.ExecContext(ctx, "DELETE FROM session WHERE expires_at < ?", now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
