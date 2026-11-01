// Package store wraps the SQLite database: connections, migrations, backups
// and the queries behind the account, sessions and settings.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
)

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrExists means a row that may exist only once is already there.
	ErrExists = errors.New("already exists")
	// ErrSchemaTooNew means the database was written by a newer JDWeb.
	ErrSchemaTooNew = errors.New("database schema is newer than this JDWeb")
	// ErrCorrupt means the database file cannot be read as a SQLite database.
	ErrCorrupt = errors.New("database is damaged")
	// ErrNotWritable means the data directory does not accept new files.
	ErrNotWritable = errors.New("data directory is not writable")
)

// DB holds a single-connection writer pool and a small read pool. Writers run
// one after another, so SQLITE_BUSY cannot occur between them.
type DB struct {
	W, R *sql.DB
	Path string
}

const pragmas = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)"

// Open opens or creates the database at path and brings the schema up to date.
// Before a migration of an existing database it writes path.pre-<version> and
// keeps the last three such copies.
func Open(path string) (*DB, error) {
	return openWith(path, migrations)
}

func openWith(path string, migs []migration) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = abs
	if err := checkWritable(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Create the file ourselves so it is private: it holds the password hash
	// and session hashes. SQLite gives the -wal and -shm files the same mode.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		f.Close()
	}
	dsn := func(extra string) string {
		u := &url.URL{Scheme: "file", Path: path, RawQuery: pragmas + extra}
		return u.String()
	}
	w, err := sql.Open("sqlite", dsn("&_txlock=immediate"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxLifetime(0)
	r, err := sql.Open("sqlite", dsn("&_pragma=query_only(1)"))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	db := &DB{W: w, R: r, Path: path}
	if err := db.prepare(context.Background(), migs); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// checkWritable proves that the directory exists (creating it if needed) and
// accepts new files. SQLite needs that for the -wal and -shm files.
func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("%w: cannot create %s: %v. Make the folder writable for the user JDWeb runs as, or set PUID/PGID", ErrNotWritable, dir, err)
	}
	f, err := os.CreateTemp(dir, ".jdweb-write-check-*")
	if err != nil {
		return fmt.Errorf("%w: cannot write to %s: %v. Make the folder writable for the user JDWeb runs as, or set PUID/PGID", ErrNotWritable, dir, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func (db *DB) corrupt(cause error) error {
	return fmt.Errorf("%w: %s cannot be read (%v). Restore a backup of the data folder, for example one of the %s.pre-<version> copies, or move the file away to start with a fresh database", ErrCorrupt, db.Path, cause, filepath.Base(db.Path))
}

// isCorruption reports whether err means "this file is not a healthy database".
func isCorruption(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 11, 26: // SQLITE_CORRUPT, SQLITE_NOTADB
			return true
		}
	}
	s := err.Error()
	return strings.Contains(s, "file is not a database") || strings.Contains(s, "malformed")
}

// prepare verifies the file, then migrates it.
func (db *DB) prepare(ctx context.Context, migs []migration) error {
	var version int
	err := db.W.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if err == nil {
		var check string
		err = db.W.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check)
		if err == nil && check != "ok" {
			err = fmt.Errorf("integrity check: %s", check)
			return db.corrupt(err)
		}
	}
	if err != nil {
		if isCorruption(err) {
			return db.corrupt(err)
		}
		return fmt.Errorf("open database %s: %w", db.Path, err)
	}
	return db.migrate(ctx, migs, version)
}

// Version returns the schema version (PRAGMA user_version).
func (db *DB) Version(ctx context.Context) (int, error) {
	var v int
	err := db.R.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// Tx runs fn in a write transaction (BEGIN IMMEDIATE).
func (db *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Close flushes the WAL and closes all connections.
func (db *DB) Close() error {
	db.W.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	db.W.Exec("PRAGMA optimize")
	return errors.Join(db.R.Close(), db.W.Close())
}
