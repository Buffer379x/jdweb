package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// User is the single account.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	TOTPSecret   string // empty = two-factor off
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserCount returns 0 or 1.
func (db *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, "SELECT count(*) FROM app_user").Scan(&n)
	return n, err
}

// GetUser returns the account, or ErrNotFound before setup.
func (db *DB) GetUser(ctx context.Context) (User, error) {
	var u User
	var c, up int64
	err := db.R.QueryRowContext(ctx,
		"SELECT id, username, password_hash, ifnull(totp_secret, ''), created_at, updated_at FROM app_user WHERE id = 1").
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &c, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, u.UpdatedAt = time.Unix(c, 0), time.Unix(up, 0)
	return u, nil
}

// CreateUser inserts the account. A second call returns ErrExists.
func (db *DB) CreateUser(ctx context.Context, username, passwordHash string, now time.Time) error {
	_, err := db.W.ExecContext(ctx,
		"INSERT INTO app_user(id, username, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?, ?)",
		username, passwordHash, now.Unix(), now.Unix())
	if err != nil && strings.Contains(err.Error(), "constraint failed") && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	return err
}

func requireOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword replaces the password hash.
func (db *DB) UpdatePassword(ctx context.Context, passwordHash string, now time.Time) error {
	return requireOne(db.W.ExecContext(ctx,
		"UPDATE app_user SET password_hash = ?, updated_at = ? WHERE id = 1", passwordHash, now.Unix()))
}

// SetTOTPSecret stores the secret; an empty string switches two-factor off.
func (db *DB) SetTOTPSecret(ctx context.Context, secret string, now time.Time) error {
	var v any
	if secret != "" {
		v = secret
	}
	return requireOne(db.W.ExecContext(ctx,
		"UPDATE app_user SET totp_secret = ?, updated_at = ? WHERE id = 1", v, now.Unix()))
}

// ReplaceRecoveryCodes deletes all recovery codes and stores the given hashes as unused.
func (db *DB) ReplaceRecoveryCodes(ctx context.Context, hashes []string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_code"); err != nil {
			return err
		}
		for _, h := range hashes {
			if _, err := tx.ExecContext(ctx, "INSERT INTO recovery_code(code_hash) VALUES (?)", h); err != nil {
				return err
			}
		}
		return nil
	})
}

// UseRecoveryCode marks an unused code as used. It reports false for unknown
// and already used codes; a code therefore works exactly once.
func (db *DB) UseRecoveryCode(ctx context.Context, hash string, now time.Time) (bool, error) {
	res, err := db.W.ExecContext(ctx,
		"UPDATE recovery_code SET used_at = ? WHERE code_hash = ? AND used_at IS NULL", now.Unix(), hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UnusedRecoveryCodes counts the codes that are still valid.
func (db *DB) UnusedRecoveryCodes(ctx context.Context) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, "SELECT count(*) FROM recovery_code WHERE used_at IS NULL").Scan(&n)
	return n, err
}

// ResetCredentials replaces the password of the existing account, switches
// two-factor off, deletes recovery codes and all sessions, and consumes the
// setting consumeKey, all in one transaction. If consumeKey does not exist it
// returns ErrNotFound and changes nothing, so a reset token works only once
// even with concurrent requests.
func (db *DB) ResetCredentials(ctx context.Context, passwordHash string, now time.Time, consumeKey string) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if err := requireOne(tx.ExecContext(ctx, "DELETE FROM setting WHERE key = ?", consumeKey)); err != nil {
			return err
		}
		if err := requireOne(tx.ExecContext(ctx,
			"UPDATE app_user SET password_hash = ?, totp_secret = NULL, updated_at = ? WHERE id = 1", passwordHash, now.Unix())); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_code"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM session")
		return err
	})
}

// EnableTOTP stores the secret and replaces the recovery codes in one
// transaction, so two-factor is never on without recovery codes.
func (db *DB) EnableTOTP(ctx context.Context, secret string, codeHashes []string, now time.Time) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if err := requireOne(tx.ExecContext(ctx,
			"UPDATE app_user SET totp_secret = ?, updated_at = ? WHERE id = 1", secret, now.Unix())); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_code"); err != nil {
			return err
		}
		for _, h := range codeHashes {
			if _, err := tx.ExecContext(ctx, "INSERT INTO recovery_code(code_hash) VALUES (?)", h); err != nil {
				return err
			}
		}
		return nil
	})
}

// DisableTOTP clears the secret and deletes the recovery codes in one transaction.
func (db *DB) DisableTOTP(ctx context.Context, now time.Time) error {
	return db.Tx(ctx, func(tx *sql.Tx) error {
		if err := requireOne(tx.ExecContext(ctx,
			"UPDATE app_user SET totp_secret = NULL, updated_at = ? WHERE id = 1", now.Unix())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM recovery_code")
		return err
	})
}
