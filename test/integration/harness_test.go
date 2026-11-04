//go:build integration

// Package integration tests JDWeb against a real JDownloader: the throwaway
// stack of scripts/teststack.sh. The tests start the real jdweb binary with a
// data directory of its own, set it up, log in and drive it through its HTTP
// API. What happened in JDownloader is checked through internal/jd, which does
// not go through JDWeb.
//
//	scripts/teststack.sh up
//	JDWEB_TEST_JD_URL=$(scripts/teststack.sh url) go test -tags integration ./test/integration/...
//
// The tests refuse to run unless the instance is the test stack
// (internal/testguard: host 127.0.0.1, the marker of scripts/teststack.sh, only
// packages with the prefix "jdweb-it-", no open dialog or captcha), so they
// cannot touch a JDownloader that holds real downloads. Never point
// JDWEB_TEST_JD_URL at one.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testguard"
)

const (
	// prefix is what the name of every package of these tests starts with.
	prefix = "jdweb-it-"
	// fixtureBase is the fixture server as JDownloader reaches it.
	fixtureBase = "http://jdweb-test-files:8000"
	// outputDir is where the tests let JDownloader save, inside its container.
	outputDir = "/output/jdweb-it"
	user      = "it-owner"
	password  = "integration-test-password-1"
)

var (
	jdURL   string // JDWEB_TEST_JD_URL, normalized
	binPath string // the jdweb binary built for this run
	root    string // the module root
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	raw := os.Getenv("JDWEB_TEST_JD_URL")
	if raw == "" {
		// A pipeline that forgot the URL must not report green without having tested anything.
		if os.Getenv("CI") != "" {
			fmt.Println("JDWEB_TEST_JD_URL is not set, and CI is: failing instead of skipping (see scripts/teststack.sh)")
			return 1
		}
		fmt.Println("JDWEB_TEST_JD_URL is not set: skipping the integration tests (see scripts/teststack.sh)")
		return 0
	}
	u, err := jd.NormalizeURL(raw)
	if err != nil {
		fmt.Println("JDWEB_TEST_JD_URL:", err)
		return 1
	}
	jdURL = u
	_, file, _, _ := runtime.Caller(0)
	root = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	dir, err := os.MkdirTemp("", "jdweb-it-bin-")
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "jdweb")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/jdweb")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Printf("cannot build jdweb: %v\n%s", err, out)
		return 1
	}
	if err := requireTestInstance(context.Background(), jd.New(jdURL)); err != nil {
		fmt.Println("REFUSING to run:", err)
		return 1
	}
	restore, err := tuneJD(context.Background(), jd.New(jdURL))
	if err != nil {
		fmt.Println("cannot prepare JDownloader:", err)
		return 1
	}
	code := m.Run()
	sharedStop()
	restore()
	return code
}

const linkgrabberSettings = "org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings"

// tuneJD changes the settings that would disturb the tests and returns the
// function that puts them back. LinkgrabberAutoStartEnabled (JDownloader's
// default: on) makes the move to the download list start the downloads, whatever
// the caller asked for (see docs/api.md, to-downloads).
func tuneJD(ctx context.Context, c *jd.Client) (restore func(), err error) {
	type setting struct {
		iface, key string
		old        json.RawMessage
		want       any
	}
	settings := []*setting{{iface: linkgrabberSettings, key: "LinkgrabberAutoStartEnabled", want: false}}
	for _, s := range settings {
		if s.old, err = c.ConfigGet(ctx, s.iface, "", s.key); err != nil {
			return nil, err
		}
		if err := c.ConfigSet(ctx, s.iface, "", s.key, s.want); err != nil {
			return nil, err
		}
	}
	return func() {
		for _, s := range settings {
			var v any
			_ = json.Unmarshal(s.old, &v)
			_ = c.ConfigSet(context.Background(), s.iface, "", s.key, v)
		}
	}, nil
}

// requireTestInstance refuses everything that is not the test stack (testguard.Rule).
func requireTestInstance(ctx context.Context, c *jd.Client) error {
	return testguard.Require(ctx, c, jdURL, prefix)
}

// syncBuffer is a writer that can be read while a process writes.
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

// app is a running jdweb process with a logged-in browser-like client.
type app struct {
	base   string
	origin string
	dir    string
	logs   *syncBuffer
	cmd    *exec.Cmd
	done   chan struct{}
	http   *http.Client
}

type appOpts struct {
	jdURL string // JDWEB_JD_URL; the test stack when empty
	env   []string
	// noSetup leaves the account out.
	noSetup bool
}

func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

// startApp starts the binary, creates the account with the setup token from its
// log and logs in. The process ends with the test.
func startApp(t testing.TB, o appOpts) *app {
	t.Helper()
	addr, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	url := o.jdURL
	if url == "" {
		url = jdURL
	}
	a := &app{base: "http://" + addr, origin: "http://" + addr, dir: dir, logs: &syncBuffer{}, done: make(chan struct{})}
	a.cmd = exec.Command(binPath, "serve")
	a.cmd.Env = append(os.Environ(), "JDWEB_LISTEN="+addr, "JDWEB_DATA_DIR="+dir, "JDWEB_JD_URL="+url, "JDWEB_LOG_LEVEL=debug")
	a.cmd.Env = append(a.cmd.Env, o.env...)
	a.cmd.Stdout, a.cmd.Stderr = a.logs, a.logs
	if err := a.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = a.cmd.Wait(); close(a.done) }()
	t.Cleanup(a.stop)

	jar, _ := cookiejar.New(nil)
	a.http = &http.Client{Jar: jar, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	waitFor(t, "jdweb to answer /healthz", 20*time.Second, func() bool {
		resp, err := a.http.Get(a.base + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})
	if !o.noSetup {
		a.setup(t)
	}
	return a
}

func (a *app) stop() {
	select {
	case <-a.done:
		return
	default:
	}
	_ = a.cmd.Process.Signal(os.Interrupt)
	select {
	case <-a.done:
	case <-time.After(15 * time.Second):
		_ = a.cmd.Process.Kill()
		<-a.done
	}
}

// setupToken reads the one-time token from the log.
func (a *app) setupToken(t testing.TB) string {
	t.Helper()
	var tok string
	waitFor(t, "the setup token in the log", 10*time.Second, func() bool {
		sc := bufio.NewScanner(strings.NewReader(a.logs.String()))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) == nil {
				if v, ok := line["setup_token"].(string); ok {
					tok = v
					return true
				}
			}
		}
		return false
	})
	return tok
}

func (a *app) setup(t testing.TB) {
	t.Helper()
	a.must(t, "POST", "/api/v1/setup", map[string]string{"token": a.setupToken(t), "username": user, "password": password}, nil, 204)
	a.must(t, "POST", "/api/v1/login", map[string]string{"username": user, "password": password}, nil, 204)
}

// call sends a request and returns the status and the body.
func (a *app) call(method, path string, in any) (int, []byte) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			panic(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		panic(err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-JDWeb-Request", "1")
	req.Header.Set("Origin", a.origin)
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// must sends a request, requires the status and decodes the answer into out.
func (a *app) must(t testing.TB, method, path string, in, out any, want int) []byte {
	t.Helper()
	status, b := a.call(method, path, in)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, clip(b))
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, clip(b))
		}
	}
	return b
}

// code returns the error code of an API answer.
func errorCode(b []byte) string {
	var e struct{ Error struct{ Code string } }
	_ = json.Unmarshal(b, &e)
	return e.Error.Code
}

func clip(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "..."
	}
	return string(b)
}

func waitFor(t testing.TB, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if ok() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not within %v: %s", timeout, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// shared is the one jdweb most tests use. The first test that needs it starts
// it; it ends with the run.
var (
	sharedMu  sync.Mutex
	sharedApp *app
)

func shared(t *testing.T) *app {
	t.Helper()
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedApp == nil {
		d, err := os.MkdirTemp("", "jdweb-it-data-")
		if err != nil {
			t.Fatal(err)
		}
		sharedApp = startApp(&keepT{TB: t, dir: d}, appOpts{})
	}
	return sharedApp
}

func sharedStop() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedApp != nil {
		sharedApp.stop()
		_ = os.RemoveAll(sharedApp.dir)
	}
}

// keepT lets the shared process outlive the test that started it: its cleanups
// and its temporary directory belong to the run.
type keepT struct {
	testing.TB
	dir string
}

func (k *keepT) TempDir() string { return k.dir }
func (k *keepT) Cleanup(func())  {}

// fx builds a fixture URL.
func fx(path string) string { return fixtureBase + path }

// directory is the save-to folder for a test, below outputDir.
func directory(name string) string { return outputDir + "/" + name }
