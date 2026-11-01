package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "jdweb.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func columns(t *testing.T, db *DB, table string) []string {
	t.Helper()
	rows, err := db.R.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestSchemaMatchesConcept(t *testing.T) {
	db := openTemp(t)
	want := map[string]string{
		"app_user":      "id,username,password_hash,totp_secret,created_at,updated_at",
		"recovery_code": "code_hash,used_at",
		"session":       "token_hash,created_at,last_seen_at,expires_at,user_agent",
		"setting":       "key,value",
	}
	for table, cols := range want {
		if got := strings.Join(columns(t, db, table), ","); got != cols {
			t.Errorf("table %s: columns %q, want %q", table, got, cols)
		}
	}
	v, err := db.Version(context.Background())
	if err != nil || v != len(migrations) {
		t.Fatalf("Version = %d, %v; want %d", v, err, len(migrations))
	}
}

func TestPragmas(t *testing.T) {
	db := openTemp(t)
	for _, c := range []struct{ pragma, want string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	} {
		var got string
		if err := db.W.QueryRow("PRAGMA " + c.pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
	if st := db.W.Stats(); st.MaxOpenConnections != 1 {
		t.Errorf("writer pool max open = %d, want 1", st.MaxOpenConnections)
	}
}

func TestOnlyOneAccountRow(t *testing.T) {
	db := openTemp(t)
	if _, err := db.W.Exec("INSERT INTO app_user(id,username,password_hash,created_at,updated_at) VALUES (2,'x','h',0,0)"); err == nil {
		t.Fatal("app_user accepted id 2")
	}
}

func TestReopenKeepsDataAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetJSON(context.Background(), "k", "v"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	var v string
	if err := db.GetJSON(context.Background(), "k", &v); err != nil || v != "v" {
		t.Fatalf("GetJSON = %q, %v", v, err)
	}
	if matches, _ := filepath.Glob(path + ".pre-*"); len(matches) != 0 {
		t.Fatalf("backups without migration: %v", matches)
	}
}

func TestRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = Open(path)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	for _, s := range []string{path, "999", "newer"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("message %q lacks %q", err, s)
		}
	}
}

func TestCorruptDatabaseGivesClearError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("this is not a database file ", 400)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	for _, s := range []string{path, "backup"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("message %q lacks %q", err, s)
		}
	}
}

func TestTruncatedDatabaseGivesClearError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the header but damage the page contents.
	for i := 100; i < len(data); i++ {
		data[i] = 0xAB
	}
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestUnwritableDataDirGivesClearError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	_, err := Open(filepath.Join(dir, "jdweb.db"))
	if !errors.Is(err, ErrNotWritable) {
		t.Fatalf("err = %v, want ErrNotWritable", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("message %q lacks the directory", err)
	}
}

func TestNewDatabaseFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, p := range []string{path, path + "-wal"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %v, want no group/other access", p, fi.Mode().Perm())
		}
	}
}

func TestMissingDataDirIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data", "jdweb.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.Close()
}

func testMigrations(n int) []migration {
	var out []migration
	for i := 1; i <= n; i++ {
		out = append(out, migration{Version: i, SQL: fmt.Sprintf("CREATE TABLE t%d (x INTEGER); INSERT INTO t%d VALUES (%d);", i, i, i)})
	}
	return out
}

func TestMigrationBackupKeepsLastThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := openWith(path, testMigrations(1))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if matches, _ := filepath.Glob(path + ".pre-*"); len(matches) != 0 {
		t.Fatalf("fresh database must not be backed up: %v", matches)
	}
	for n := 2; n <= 6; n++ {
		db, err := openWith(path, testMigrations(n))
		if err != nil {
			t.Fatalf("migrate to %d: %v", n, err)
		}
		db.Close()
	}
	matches, _ := filepath.Glob(path + ".pre-*")
	var got []string
	for _, m := range matches {
		got = append(got, filepath.Base(m))
	}
	sort.Strings(got)
	want := []string{"jdweb.db.pre-4", "jdweb.db.pre-5", "jdweb.db.pre-6"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("backups = %v, want %v", got, want)
	}
	// pre-6 is the state before migrating to 6: version 5, no table t6.
	b, err := openWith(path+".pre-6", testMigrations(5))
	if err != nil {
		t.Fatalf("backup is not a usable database: %v", err)
	}
	defer b.Close()
	if v, _ := b.Version(context.Background()); v != 5 {
		t.Errorf("backup version = %d, want 5", v)
	}
	if len(columns(t, b, "t6")) != 0 {
		t.Error("backup contains t6")
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := openWith(path, testMigrations(1))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	bad := append(testMigrations(1), migration{Version: 2, SQL: "CREATE TABLE half (x); INSERT INTO nonexistent VALUES (1);"})
	if _, err := openWith(path, bad); err == nil {
		t.Fatal("expected migration error")
	}
	db, err = openWith(path, testMigrations(1))
	if err != nil {
		t.Fatalf("database unusable after failed migration: %v", err)
	}
	defer db.Close()
	if v, _ := db.Version(context.Background()); v != 1 {
		t.Errorf("version = %d, want 1", v)
	}
	if len(columns(t, db, "half")) != 0 {
		t.Error("partial migration was not rolled back")
	}
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	if n, err := db.UserCount(ctx); err != nil || n != 0 {
		t.Fatalf("UserCount = %d, %v", n, err)
	}
	if _, err := db.GetUser(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUser on empty = %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	if err := db.CreateUser(ctx, "alice", "hash1", now); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(ctx, "bob", "hash2", now); !errors.Is(err, ErrExists) {
		t.Fatalf("second CreateUser = %v, want ErrExists", err)
	}
	u, err := db.GetUser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "alice" || u.PasswordHash != "hash1" || u.TOTPSecret != "" || !u.CreatedAt.Equal(now) {
		t.Fatalf("user = %+v", u)
	}
	later := now.Add(time.Hour)
	if err := db.UpdatePassword(ctx, "hash3", later); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTOTPSecret(ctx, "SECRET", later); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(ctx)
	if u.PasswordHash != "hash3" || u.TOTPSecret != "SECRET" || !u.UpdatedAt.Equal(later) {
		t.Fatalf("user = %+v", u)
	}
	if err := db.SetTOTPSecret(ctx, "", later); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(ctx)
	if u.TOTPSecret != "" {
		t.Fatalf("secret not cleared: %+v", u)
	}
}

func TestUpdatesOnMissingUserFail(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	if err := db.UpdatePassword(ctx, "h", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdatePassword = %v", err)
	}
	if err := db.SetTOTPSecret(ctx, "s", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTOTPSecret = %v", err)
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	now := time.Unix(1000, 0)
	if err := db.ReplaceRecoveryCodes(ctx, []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 3 {
		t.Fatalf("unused = %d", n)
	}
	if ok, err := db.UseRecoveryCode(ctx, "b", now); err != nil || !ok {
		t.Fatalf("first use = %v, %v", ok, err)
	}
	if ok, _ := db.UseRecoveryCode(ctx, "b", now); ok {
		t.Fatal("recovery code accepted twice")
	}
	if ok, _ := db.UseRecoveryCode(ctx, "zzz", now); ok {
		t.Fatal("unknown code accepted")
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 2 {
		t.Fatalf("unused = %d", n)
	}
	if err := db.ReplaceRecoveryCodes(ctx, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.UseRecoveryCode(ctx, "a", now); ok {
		t.Fatal("old code survived replace")
	}
	if ok, _ := db.UseRecoveryCode(ctx, "b", now); !ok {
		t.Fatal("replaced code should be fresh again")
	}
}

func TestResetCredentialsIsAtomicAndConsumesKey(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	now := time.Unix(1000, 0)
	if err := db.CreateUser(ctx, "alice", "old", now); err != nil {
		t.Fatal(err)
	}
	db.SetTOTPSecret(ctx, "SECRET", now)
	db.ReplaceRecoveryCodes(ctx, []string{"a"})
	db.CreateSession(ctx, Session{TokenHash: "s1", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)})

	// The reset key is missing: nothing may change.
	if err := db.ResetCredentials(ctx, "new", now, "reset"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResetCredentials without key = %v, want ErrNotFound", err)
	}
	if u, _ := db.GetUser(ctx); u.PasswordHash != "old" || u.TOTPSecret == "" {
		t.Fatalf("user changed despite failure: %+v", u)
	}

	db.SetJSON(ctx, "reset", "x")
	if err := db.ResetCredentials(ctx, "new", now, "reset"); err != nil {
		t.Fatal(err)
	}
	u, _ := db.GetUser(ctx)
	if u.PasswordHash != "new" || u.TOTPSecret != "" || u.Username != "alice" {
		t.Fatalf("user = %+v", u)
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 0 {
		t.Errorf("recovery codes survived: %d", n)
	}
	if ss, _ := db.ListSessions(ctx); len(ss) != 0 {
		t.Errorf("sessions survived: %d", len(ss))
	}
	if err := db.GetJSON(ctx, "reset", new(string)); !errors.Is(err, ErrNotFound) {
		t.Errorf("reset key survived: %v", err)
	}
	if err := db.ResetCredentials(ctx, "again", now, "reset"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second reset = %v, want ErrNotFound", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	mk := func(h string, ttl time.Duration) Session {
		return Session{TokenHash: h, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(ttl), UserAgent: "UA " + h}
	}
	for _, s := range []Session{mk("a", time.Hour), mk("b", 2*time.Hour), mk("c", -time.Hour)} {
		if err := db.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.GetSession(ctx, "b")
	if err != nil || got.UserAgent != "UA b" || !got.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	if _, err := db.GetSession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession unknown = %v", err)
	}
	if err := db.TouchSession(ctx, "a", now.Add(time.Minute), now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetSession(ctx, "a")
	if !got.LastSeenAt.Equal(now.Add(time.Minute)) || !got.ExpiresAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("after touch: %+v", got)
	}
	all, err := db.ListSessions(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListSessions = %d, %v", len(all), err)
	}
	if n, err := db.DeleteExpiredSessions(ctx, now); err != nil || n != 1 {
		t.Fatalf("DeleteExpiredSessions = %d, %v", n, err)
	}
	if n, err := db.DeleteOtherSessions(ctx, "a"); err != nil || n != 1 {
		t.Fatalf("DeleteOtherSessions = %d, %v", n, err)
	}
	if _, err := db.GetSession(ctx, "a"); err != nil {
		t.Fatalf("kept session gone: %v", err)
	}
	if err := db.DeleteSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSession(ctx, "a"); err != nil {
		t.Fatalf("deleting twice must not fail: %v", err)
	}
	if all, _ := db.ListSessions(ctx); len(all) != 0 {
		t.Fatalf("sessions left: %d", len(all))
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	var s string
	if err := db.GetJSON(ctx, "jd.url", &s); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key = %v", err)
	}
	type defaults struct {
		Dense bool     `json:"dense"`
		Cols  []string `json:"cols"`
	}
	in := defaults{Dense: true, Cols: []string{"name", "speed"}}
	if err := db.SetJSON(ctx, "ui.defaults", in); err != nil {
		t.Fatal(err)
	}
	var out defaults
	if err := db.GetJSON(ctx, "ui.defaults", &out); err != nil || !out.Dense || len(out.Cols) != 2 {
		t.Fatalf("GetJSON = %+v, %v", out, err)
	}
	if err := db.SetJSON(ctx, "ui.defaults", defaults{}); err != nil {
		t.Fatal(err)
	}
	db.GetJSON(ctx, "ui.defaults", &out)
	if out.Dense {
		t.Fatal("SetJSON did not overwrite")
	}
	if err := db.DeleteSetting(ctx, "ui.defaults"); err != nil {
		t.Fatal(err)
	}
	if err := db.GetJSON(ctx, "ui.defaults", &out); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete = %v", err)
	}
	if err := db.SetJSON(ctx, "bad", make(chan int)); err == nil {
		t.Fatal("unmarshalable value accepted")
	}
}

func TestConcurrentWritersDoNotLock(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- db.SetJSON(ctx, fmt.Sprintf("k%d", i%5), i)
			var v int
			db.GetJSON(ctx, "k0", &v)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnableAndDisableTOTPAreAtomic(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	now := time.Unix(1000, 0)
	if err := db.EnableTOTP(ctx, "S", []string{"a"}, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("EnableTOTP without user = %v", err)
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 0 {
		t.Fatal("recovery codes written although the user is missing")
	}
	db.CreateUser(ctx, "alice", "h", now)
	if err := db.EnableTOTP(ctx, "SECRET", []string{"a", "b"}, now); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.GetUser(ctx); u.TOTPSecret != "SECRET" {
		t.Fatalf("user = %+v", u)
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 2 {
		t.Fatalf("codes = %d", n)
	}
	if err := db.DisableTOTP(ctx, now); err != nil {
		t.Fatal(err)
	}
	if u, _ := db.GetUser(ctx); u.TOTPSecret != "" {
		t.Fatalf("user = %+v", u)
	}
	if n, _ := db.UnusedRecoveryCodes(ctx); n != 0 {
		t.Fatalf("codes = %d", n)
	}
}

func TestMigrationBackupIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := openWith(path, testMigrations(1))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = openWith(path, testMigrations(2))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	fi, err := os.Stat(path + ".pre-2")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("backup has mode %v, want no group/other access", fi.Mode().Perm())
	}
	// Contents must be a real copy, written by VACUUM INTO into the pre-created file.
	b, err := openWith(path+".pre-2", testMigrations(1))
	if err != nil {
		t.Fatalf("backup is not a database: %v", err)
	}
	b.Close()
}

// Two processes (server and CLI) may open the database at the same time. The
// loser sees a version that the winner already raised; it must not fail.
func TestMigrationRereadsVersionInsideTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jdweb.db")
	db, err := openWith(path, testMigrations(3))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.migrate(context.Background(), testMigrations(3), 1); err != nil {
		t.Fatalf("stale version made the migration fail: %v", err)
	}
	if v, _ := db.Version(context.Background()); v != 3 {
		t.Fatalf("version = %d", v)
	}
	// A stale view must not hide a newer schema either.
	err = db.migrate(context.Background(), testMigrations(2), 1)
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}
