package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/store"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestConfigDefaults(t *testing.T) {
	c, err := loadConfig(env())
	if err != nil {
		t.Fatal(err)
	}
	if c.JDURL != "http://jdownloader:3128" || c.JDURLSource != "default" || c.Listen != ":7468" || c.DataDir != "/data" ||
		c.PUID != 99 || c.PGID != 100 || c.LogLevel != slog.LevelInfo || len(c.TrustedProxies) != 0 {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	c, err := loadConfig(env("JDWEB_JD_URL", "Host.Lan:3128/", "JDWEB_LISTEN", "127.0.0.1:9000", "JDWEB_DATA_DIR", "/srv/jdweb",
		"JDWEB_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.5", "JDWEB_LOG_LEVEL", "DEBUG", "PUID", "1000", "PGID", "1001"))
	if err != nil {
		t.Fatal(err)
	}
	if c.JDURL != "http://host.lan:3128" || c.JDURLSource != "env" || c.Listen != "127.0.0.1:9000" || c.DataDir != "/srv/jdweb" ||
		c.PUID != 1000 || c.PGID != 1001 || c.LogLevel != slog.LevelDebug || len(c.TrustedProxies) != 2 {
		t.Fatalf("config = %+v", c)
	}
}

// Every mistake names the variable.
func TestConfigErrorsNameTheVariable(t *testing.T) {
	for _, c := range []struct{ key, val, want string }{
		{"JDWEB_JD_URL", "ftp://host", "JDWEB_JD_URL"},
		{"JDWEB_JD_URL", "http://", "JDWEB_JD_URL"},
		{"JDWEB_LISTEN", "7468", "JDWEB_LISTEN"},
		{"JDWEB_LISTEN", ":99999", "JDWEB_LISTEN"},
		{"JDWEB_TRUSTED_PROXIES", "not-an-ip", "JDWEB_TRUSTED_PROXIES"},
		{"JDWEB_LOG_LEVEL", "loud", "JDWEB_LOG_LEVEL"},
		{"PUID", "abc", "PUID"},
		{"PGID", "-1", "PGID"},
	} {
		_, err := loadConfig(env(c.key, c.val))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%q: %v", c.key, c.val, err)
		}
	}
}

func TestHealthURL(t *testing.T) {
	for listen, want := range map[string]string{
		":7468": "http://127.0.0.1:7468/healthz", "0.0.0.0:80": "http://127.0.0.1:80/healthz", "[::]:7468": "http://127.0.0.1:7468/healthz",
		"127.0.0.1:9": "http://127.0.0.1:9/healthz", "192.168.1.5:7468": "http://192.168.1.5:7468/healthz",
		"[::1]:7468": "http://[::1]:7468/healthz", "garbage": "http://127.0.0.1:7468/healthz",
	} {
		if got := healthURL(listen); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", listen, got, want)
		}
	}
}

func TestVersionAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != version {
		t.Fatalf("version: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "reset-password") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	if code := run([]string{"frobnicate"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "Commands:") {
		t.Fatalf("unknown command: %d %q", code, errOut.String())
	}
}

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") }))
	defer ok.Close()
	var out bytes.Buffer
	if err := checkHealth(ok.URL+"/healthz", &out); err != nil || strings.TrimSpace(out.String()) != "ok" {
		t.Fatalf("healthy server: %v %q", err, out.String())
	}
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 503) }))
	defer sick.Close()
	if err := checkHealth(sick.URL, &out); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("sick server: %v", err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "<html>router</html>") }))
	defer other.Close()
	if err := checkHealth(other.URL, &out); err == nil {
		t.Fatal("another service on the port counts as healthy")
	}
	ok.Close()
	if err := checkHealth(ok.URL, &out); err == nil {
		t.Fatal("a closed server counts as healthy")
	}
	// And through the command: nothing listens on port 1.
	var o, e bytes.Buffer
	if code := runWithEnv(t, "JDWEB_LISTEN", "127.0.0.1:1", func() int { return run([]string{"healthcheck"}, &o, &e) }); code != 1 || !strings.Contains(e.String(), "unhealthy") {
		t.Fatalf("healthcheck exit = %d, %q", code, e.String())
	}
}

func runWithEnv(t *testing.T, key, val string, f func() int) int {
	t.Helper()
	old, had := os.LookupEnv(key)
	os.Setenv(key, val)
	defer func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	}()
	return f()
}

// Review Focus 5: an unwritable data directory is one clear line naming chown.
func TestUnwritableDataDirNamesTheChownCommand(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	c := config{DataDir: dir, PUID: 99, PGID: 100}
	_, err := openData(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("no error for a folder nobody can write to")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") || !strings.Contains(msg, dir) || !strings.Contains(msg, "chown -R 99:100") || !strings.Contains(msg, "--user 0:0") {
		t.Fatalf("message = %q", msg)
	}
	// Through the command: exit 1 and the message on stderr, no stack trace.
	var out, errOut bytes.Buffer
	code := runWithEnv(t, "JDWEB_DATA_DIR", dir, func() int { return run([]string{"reset-password"}, &out, &errOut) })
	if code != 1 || !strings.Contains(errOut.String(), "chown -R 99:100") || strings.Contains(errOut.String(), "goroutine") {
		t.Fatalf("exit %d: %q", code, errOut.String())
	}
}

func TestMissingDataDirIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "data")
	db, err := openData(config{DataDir: dir, PUID: 99, PGID: 100}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Join(dir, "jdweb.db")); err != nil {
		t.Fatal(err)
	}
}

func TestDamagedDatabaseGivesAMessage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "jdweb.db"), bytes.Repeat([]byte("not a database "), 200), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runWithEnv(t, "JDWEB_DATA_DIR", dir, func() int { return run([]string{"reset-password"}, &out, &errOut) })
	if code != 1 || !strings.Contains(errOut.String(), "jdweb.db") || strings.Contains(errOut.String(), "goroutine") {
		t.Fatalf("exit %d: %q", code, errOut.String())
	}
}

// The address saved in the web interface wins over JDWEB_JD_URL.
func TestSavedURLWinsOverTheEnvironment(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "jdweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := config{JDURL: "http://from-env:3128", JDURLSource: "env"}
	url, src, err := chooseURL(db, c, log)
	if err != nil || url != "http://from-env:3128" || src != "env" {
		t.Fatalf("nothing saved: %s %s %v", url, src, err)
	}
	if err := db.SetJSON(context.Background(), "jd.url", "http://from-ui:3128"); err != nil {
		t.Fatal(err)
	}
	url, src, err = chooseURL(db, c, log)
	if err != nil || url != "http://from-ui:3128" || src != "ui" {
		t.Fatalf("saved: %s %s %v", url, src, err)
	}
	// A saved address that cannot be used is ignored, the environment applies.
	if err := db.SetJSON(context.Background(), "jd.url", "ftp://nonsense"); err != nil {
		t.Fatal(err)
	}
	url, src, _ = chooseURL(db, c, log)
	if url != "http://from-env:3128" || src != "env" {
		t.Fatalf("invalid saved: %s %s", url, src)
	}
}

func TestResetPasswordPrintsATokenThatSetupAccepts(t *testing.T) {
	dir := t.TempDir()
	// No account yet: a clear message, no token.
	var out, errOut bytes.Buffer
	code := runWithEnv(t, "JDWEB_DATA_DIR", dir, func() int { return run([]string{"reset-password"}, &out, &errOut) })
	if code != 1 || !strings.Contains(errOut.String(), "no account yet") || out.Len() != 0 {
		t.Fatalf("without an account: %d out=%q err=%q", code, out.String(), errOut.String())
	}

	db, err := store.Open(filepath.Join(dir, "jdweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("the old password 123")
	if err := db.CreateUser(context.Background(), "owner", hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out.Reset()
	errOut.Reset()
	code = runWithEnv(t, "JDWEB_DATA_DIR", dir, func() int { return run([]string{"reset-password"}, &out, &errOut) })
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var token string
	for _, line := range strings.Split(out.String(), "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.Contains(l, " ") {
			token = l
		}
	}
	if len(token) < 20 {
		t.Fatalf("no token in %q", out.String())
	}
	// The server side takes it: setup is required again and the token works once.
	db, err = store.Open(filepath.Join(dir, "jdweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := auth.New(db, auth.Config{})
	if req, _ := svc.SetupRequired(context.Background()); !req {
		t.Fatal("setup is not required after reset-password")
	}
	err = svc.Setup(context.Background(), auth.SetupRequest{Token: token, Username: "ignored", Password: "a brand new password 1", IP: "1.1.1.1"})
	if err != nil {
		t.Fatalf("setup with the reset token: %v", err)
	}
	if _, err := svc.Login(context.Background(), auth.LoginRequest{Username: "owner", Password: "a brand new password 1", IP: "1.1.1.1"}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The whole server in one process: start, setup with the token from the log,
// login, an action reaching the fake JDownloader, graceful shutdown.
func TestServeEndToEnd(t *testing.T) {
	fake := testjd.New(t)
	_, links := fake.AddPackage(testjd.Downloads, testjd.Package{Name: "p"}, testjd.Link{Name: "a.bin"})
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	c := config{JDURL: fake.URL, JDURLSource: "env", Listen: "127.0.0.1:0", DataDir: t.TempDir(), PUID: 99, PGID: 100}

	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- serveWith(ctx, c, log, func(a string) { addr <- a }) }()
	var base string
	select {
	case a := <-addr:
		base = "http://" + a
	case err := <-done:
		t.Fatalf("serve ended: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not start")
	}

	call := func(method, path string, body any, cookie string) (int, string, http.Header) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, rd)
		if method != "GET" {
			req.Header.Set("X-JDWeb-Request", "1")
			req.Header.Set("Origin", base)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "jdweb_session", Value: cookie})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}

	if st, body, _ := call("GET", "/healthz", nil, ""); st != 200 || strings.TrimSpace(body) != "ok" {
		t.Fatalf("healthz = %d %q", st, body)
	}
	if st, body, h := call("GET", "/", nil, ""); st != 404 || !strings.Contains(body, "not part of this build") || h.Get("Content-Security-Policy") == "" {
		t.Fatalf("/ without a build = %d %q", st, body)
	}
	if st, _, _ := call("GET", "/api/v1/status", nil, ""); st != 401 {
		t.Fatalf("status without a session = %d", st)
	}
	var token string
	for _, line := range strings.Split(logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["setup_token"] != nil {
			token = rec["setup_token"].(string)
		}
	}
	if token == "" {
		t.Fatalf("no setup token in the log: %s", logs.String())
	}
	const pw = "an end to end password"
	if st, body, _ := call("POST", "/api/v1/setup", map[string]string{"token": token, "username": "owner", "password": pw}, ""); st != 204 {
		t.Fatalf("setup = %d %s", st, body)
	}
	st, body, h := call("POST", "/api/v1/login", map[string]string{"username": "owner", "password": pw}, "")
	if st != 204 {
		t.Fatalf("login = %d %s", st, body)
	}
	var session string
	for _, ck := range (&http.Response{Header: h}).Cookies() {
		session = ck.Value
	}
	if session == "" {
		t.Fatal("no session cookie")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, body, _ := call("GET", "/api/v1/downloads", nil, session)
		if strings.Contains(body, "a.bin") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the list never loaded: %s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st, body, _ := call("POST", "/api/v1/downloads/actions/disable", map[string]any{"linkIds": []string{fmt.Sprint(links[0])}}, session); st != 204 {
		t.Fatalf("action = %d %s", st, body)
	}
	if len(fake.CallsTo("/downloadsV2/setEnabled")) != 1 {
		t.Fatalf("the action did not reach JDownloader: %v", fake.Calls())
	}

	// SIGTERM: ends within the grace time even with an open event stream.
	req, _ := http.NewRequest("GET", base+"/api/v1/events", nil)
	req.AddCookie(&http.Cookie{Name: "jdweb_session", Value: session})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no shutdown within 15 s with an event stream open")
	}
	if strings.Contains(logs.String(), pw) {
		t.Error("the password is in the log")
	}
}

func TestCheckDataDirRefusesTheRootAndEmpty(t *testing.T) {
	for _, dir := range []string{"", "  ", "/", "//", "/."} {
		if err := checkDataDir(dir); err == nil {
			t.Errorf("checkDataDir(%q) accepted", dir)
		}
	}
	// A link to / is the root too.
	link := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink("/", link); err == nil {
		if err := checkDataDir(link); err == nil {
			t.Error("a symbolic link to / was accepted")
		}
	}
	for _, dir := range []string{"/data", t.TempDir(), "relative/data"} {
		if err := checkDataDir(dir); err != nil {
			t.Errorf("checkDataDir(%q): %v", dir, err)
		}
	}
	// openData stops before it touches anything.
	if _, err := openData(config{DataDir: "/", PUID: 99, PGID: 100}, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil ||
		!strings.Contains(err.Error(), "root of the file system") {
		t.Fatalf("openData(/) = %v", err)
	}
}

func TestLoggerMasksSecretLookingAttributes(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(config{}, &buf)
	log.Info("x", "password", "hunter2", "api_key", "k-1", "setup_token", "shown", "path", "/p")
	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "k-1") || !strings.Contains(out, "shown") || !strings.Contains(out, "/p") {
		t.Fatalf("log = %s", out)
	}
}
