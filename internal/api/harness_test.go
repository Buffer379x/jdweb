package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/store"
	jdsync "github.com/Buffer379x/jdweb/internal/sync"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

const (
	testUser = "owner"
	testPass = "correct horse battery staple"
)

// passwordHash is the Argon2id hash of testPass at the production cost, made
// once: every test that needs an account uses it.
var (
	hashOnce sync.Once
	hashVal  string
)

func passwordHash(t testing.TB) string {
	t.Helper()
	hashOnce.Do(func() {
		h, err := auth.HashPassword(testPass)
		if err != nil {
			panic(err)
		}
		hashVal = h
	})
	return hashVal
}

// logBuffer is a log sink that can be read while the server writes.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type envOptions struct {
	noAccount bool
	run       bool // run the sync engine
	static    fs.FS
	keepalive time.Duration
	logLevel  slog.Level
	noJD      bool // no JDownloader address at all
	fastSync  bool
	notDocker bool
}

// env is a complete JDWeb in a test: fake JDownloader, engine, database, API
// on a test server, and a browser-like client with a session.
type env struct {
	t      *testing.T
	jd     *testjd.Server
	engine *jdsync.Engine
	mirror *state.Store
	hub    *sse.Hub
	auth   *auth.Service
	db     *store.DB
	srv    *Server
	ts     *httptest.Server
	logs   *logBuffer
	token  string // the session cookie value
	http   *http.Client
}

func newEnv(t *testing.T, opts ...func(*envOptions)) *env {
	t.Helper()
	var o envOptions
	for _, f := range opts {
		f(&o)
	}
	e := &env{t: t, logs: &logBuffer{}, mirror: state.New(), hub: sse.New()}
	e.jd = testjd.New(t)
	if o.notDocker {
		e.jd.SetDocker(false)
	}
	log := slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: o.logLevel}))

	db, err := store.Open(t.TempDir() + "/jdweb.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.W.Close(); db.R.Close() })
	e.db = db
	e.auth = auth.New(db, auth.Config{Logger: log})

	var client *jd.Client
	if !o.noJD {
		client = jd.New(e.jd.URL)
	}
	cfg := jdsync.Config{Logger: log}
	if o.fastSync {
		cfg = jdsync.Config{PatchInterval: 10 * time.Millisecond, Debounce: 5 * time.Millisecond, MinReloadGap: 20 * time.Millisecond,
			StructureCheck: time.Hour, PollAttached: time.Hour, PollDetached: time.Hour, Logger: log}
	}
	e.engine = jdsync.New(client, e.mirror, e.hub, cfg)
	if o.run {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = e.engine.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
	}

	e.srv = New(Options{
		Engine: e.engine, Store: e.mirror, Hub: e.hub, Auth: e.auth, DB: db, Logger: log, Version: "1.2.3-test",
		Static: o.static, Keepalive: o.keepalive, URLSource: "env",
	})
	e.ts = httptest.NewServer(e.srv.Handler())
	t.Cleanup(func() { e.srv.CloseStreams(); e.ts.Close() })
	e.http = &http.Client{Transport: &http.Transport{DisableCompression: true}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	if !o.noAccount {
		if err := db.CreateUser(context.Background(), testUser, passwordHash(t), time.Now()); err != nil {
			t.Fatal(err)
		}
		e.token = "test-session-token-" + strings.Repeat("x", 24)
		sum := sha256.Sum256([]byte(e.token))
		now := time.Now()
		err := db.CreateSession(context.Background(), store.Session{
			TokenHash: hex.EncodeToString(sum[:]), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(24 * time.Hour), UserAgent: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func withRun(o *envOptions)        { o.run = true }
func withFastSync(o *envOptions)   { o.fastSync = true }
func withoutAccount(o *envOptions) { o.noAccount = true }
func withDebugLog(o *envOptions)   { o.logLevel = slog.LevelDebug }

// result is a finished request.
type result struct {
	status int
	header http.Header
	body   []byte
}

func (r result) json(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("body is not JSON (%v): %s", err, r.body)
	}
}

// errCode returns the code of the error shape, "" if there is none.
func (r result) errCode() string {
	var b errorBody
	if json.Unmarshal(r.body, &b) != nil {
		return ""
	}
	return b.Error.Code
}

func (r result) errMessage() string {
	var b errorBody
	_ = json.Unmarshal(r.body, &b)
	return b.Error.Message
}

// reqOpts adjusts a request.
type reqOpts struct {
	noSession bool
	noHeader  bool
	origin    string // "" means the right one, "-" none
	header    map[string]string
	cookie    string
}

func (e *env) do(method, path string, body any, mods ...func(*reqOpts)) result {
	e.t.Helper()
	var ro reqOpts
	for _, m := range mods {
		m(&ro)
	}
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" && method != "HEAD" {
		if !ro.noHeader {
			req.Header.Set("X-JDWeb-Request", "1")
		}
		switch ro.origin {
		case "":
			req.Header.Set("Origin", e.ts.URL)
		case "-":
		default:
			req.Header.Set("Origin", ro.origin)
		}
	}
	if !ro.noSession && e.token != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: e.token})
	}
	if ro.cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ro.cookie})
	}
	for k, v := range ro.header {
		req.Header.Set(k, v)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return result{resp.StatusCode, resp.Header, data}
}

func (e *env) get(path string, mods ...func(*reqOpts)) result {
	e.t.Helper()
	return e.do("GET", path, nil, mods...)
}
func (e *env) post(path string, body any, mods ...func(*reqOpts)) result {
	e.t.Helper()
	return e.do("POST", path, body, mods...)
}

func noSession(o *reqOpts) { o.noSession = true }

func (e *env) expect(r result, status int) {
	e.t.Helper()
	if r.status != status {
		e.t.Fatalf("status = %d, want %d: %s", r.status, status, r.body)
	}
}

func (e *env) expectErr(r result, status int, code string) {
	e.t.Helper()
	if r.status != status || r.errCode() != code {
		e.t.Fatalf("answer = %d %q (%s), want %d %q", r.status, r.errCode(), r.body, status, code)
	}
}

// eventually polls until ok, for at most 10 s.
func eventually(t testing.TB, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within 10 s: %s", what)
		}
		time.Sleep(3 * time.Millisecond)
	}
}

func (e *env) waitConnected() {
	e.t.Helper()
	eventually(e.t, "the engine is connected", func() bool { return e.engine.Status().Connection == state.ConnConnected })
}

// jdCalls returns the calls to one JDownloader path.
func (e *env) jdCalls(path string) []testjd.Call { return e.jd.CallsTo(path) }

// argsJSON returns the params of a call as compact JSON.
func argsJSON(c testjd.Call) string { return string(c.Params) }

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// addSession inserts one more session with the given cookie value.
func (e *env) addSession(token string) {
	e.t.Helper()
	sum := sha256.Sum256([]byte(token))
	now := time.Now()
	err := e.db.CreateSession(context.Background(), store.Session{
		TokenHash: hex.EncodeToString(sum[:]), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), UserAgent: "other",
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

type fakeRecorder struct {
	h    http.Header
	code int
	body bytes.Buffer
}

func newRecorder() *fakeRecorder                    { return &fakeRecorder{h: http.Header{}, code: 200} }
func (f *fakeRecorder) Header() http.Header         { return f.h }
func (f *fakeRecorder) Write(b []byte) (int, error) { return f.body.Write(b) }
func (f *fakeRecorder) WriteHeader(c int)           { f.code = c }

func httptestRequest() *http.Request {
	return httptest.NewRequest("GET", "/x", nil)
}
