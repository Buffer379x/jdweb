package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// keepBackups is the number of jdweb.db.pre-<version> copies that survive.
const keepBackups = 3

type migration struct {
	Version int
	SQL     string
}

// migrations are the embedded files migrations/<version>_<name>.sql in order.
var migrations = loadMigrations()

func loadMigrations() []migration {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		panic(err)
	}
	var out []migration
	for _, f := range files {
		n, err := strconv.Atoi(strings.SplitN(filepath.Base(f), "_", 2)[0])
		if err != nil {
			panic(fmt.Sprintf("store: migration file %s: %v", f, err))
		}
		body, err := migrationFS.ReadFile(f)
		if err != nil {
			panic(err)
		}
		out = append(out, migration{Version: n, SQL: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			panic(fmt.Sprintf("store: migration versions must be 1..n without gaps, found %d at position %d", m.Version, i+1))
		}
	}
	return out
}

func (db *DB) migrate(ctx context.Context, migs []migration, current int) error {
	latest := len(migs)
	if current > latest {
		return fmt.Errorf("%w: %s has schema version %d, this JDWeb understands up to %d. Update JDWeb to the newer image, or restore an older backup of the data folder", ErrSchemaTooNew, db.Path, current, latest)
	}
	if current == latest {
		return nil
	}
	if current > 0 {
		if err := db.backup(ctx, latest); err != nil {
			return fmt.Errorf("back up %s before migrating: %w", db.Path, err)
		}
	}
	// All pending migrations run in one transaction: either the database is
	// at the new version, or nothing changed. The version is read again inside
	// the transaction (which holds the write lock), because another process,
	// such as the reset-password CLI, may have migrated in the meantime.
	err := db.Tx(ctx, func(tx *sql.Tx) error {
		var v int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
			return err
		}
		if v > latest {
			return fmt.Errorf("%w: %s has schema version %d, this JDWeb understands up to %d. Update JDWeb to the newer image, or restore an older backup of the data folder", ErrSchemaTooNew, db.Path, v, latest)
		}
		if v == latest {
			return nil
		}
		for _, m := range migs[v:] {
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return fmt.Errorf("migration %d: %w", m.Version, err)
			}
		}
		_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", latest))
		return err
	})
	if errors.Is(err, ErrSchemaTooNew) {
		return err
	}
	if err != nil {
		return fmt.Errorf("migrate %s from version %d to %d (database left unchanged): %w", db.Path, current, latest, err)
	}
	return nil
}

// backup writes a consistent copy as <path>.pre-<target> and prunes old ones.
func (db *DB) backup(ctx context.Context, target int) error {
	dst := fmt.Sprintf("%s.pre-%d", db.Path, target)
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	// VACUUM INTO accepts an empty existing file. Creating it first keeps the
	// copy private: it holds the password hash and the session hashes.
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	f.Close()
	if _, err := db.W.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		os.Remove(dst)
		return err
	}
	matches, err := filepath.Glob(db.Path + ".pre-*")
	if err != nil {
		return err
	}
	type bk struct {
		n    int
		path string
	}
	var all []bk
	for _, m := range matches {
		n, err := strconv.Atoi(strings.TrimPrefix(m, db.Path+".pre-"))
		if err != nil {
			continue // not ours
		}
		all = append(all, bk{n, m})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	for _, b := range all[min(keepBackups, len(all)):] {
		os.Remove(b.path)
	}
	return nil
}
