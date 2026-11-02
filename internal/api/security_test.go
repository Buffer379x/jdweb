package api

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Buffer379x/jdweb/internal/auth"
)

// pathFor fills the parameters of a route path.
func pathFor(p string) string {
	for {
		i := strings.Index(p, "{")
		if i < 0 {
			return p
		}
		j := strings.Index(p, "}")
		p = p[:i] + "1" + p[j+1:]
	}
}

var publicRoutes = map[string]bool{
	"GET /healthz": true, "GET /api/v1/setup/status": true, "POST /api/v1/setup": true, "POST /api/v1/login": true,
}

// T-SEC-01: every registered route answers 401 without a session, except the
// public ones. The test walks the route table, so a new route is covered the
// moment it is registered.
func TestEveryRouteNeedsASession(t *testing.T) {
	e := newEnv(t, withoutAccount)
	routes := e.srv.Routes()
	if len(routes) < 60 {
		t.Fatalf("only %d routes in the table", len(routes))
	}
	seen := map[string]bool{}
	for _, rt := range routes {
		if seen[rt.Pattern()] {
			t.Errorf("route %s is registered twice", rt.Pattern())
		}
		seen[rt.Pattern()] = true
		if rt.Public != publicRoutes[rt.Pattern()] {
			t.Errorf("route %s: Public = %v, want %v", rt.Pattern(), rt.Public, publicRoutes[rt.Pattern()])
		}
		// With the mutation headers, so that only the missing session can be the reason.
		r := e.do(rt.Method, pathFor(rt.Path), nil)
		if rt.Public {
			if r.status == 401 {
				t.Errorf("public route %s answers 401", rt.Pattern())
			}
			continue
		}
		if r.status != 401 || r.errCode() != "unauthorized" {
			t.Errorf("%s without a session: %d %s, want 401 unauthorized", rt.Pattern(), r.status, r.body)
		}
		// Without the headers too: 401 comes first, the answer must not depend on them.
		r = e.do(rt.Method, pathFor(rt.Path), nil, noSession, func(o *reqOpts) { o.noHeader = true })
		if r.status != 401 {
			t.Errorf("%s without a session and headers: %d, want 401", rt.Pattern(), r.status)
		}
	}
	for p := range publicRoutes {
		if !seen[p] {
			t.Errorf("public route %s is not registered", p)
		}
	}
}

func TestSessionOfAnotherCookieIsRefused(t *testing.T) {
	e := newEnv(t)
	for _, c := range []string{"nonsense", strings.Repeat("a", 500), "test-session-token-"} {
		r := e.get("/api/v1/status", noSession, func(o *reqOpts) { o.cookie = c })
		e.expectErr(r, 401, "unauthorized")
	}
	e.expect(e.get("/api/v1/status"), 200)
}

// T-SEC-04: a foreign Origin or a missing header on a change is refused, on
// every route that is not a read.
func TestEveryChangeNeedsHeaderAndOrigin(t *testing.T) {
	e := newEnv(t)
	n := 0
	for _, rt := range e.srv.Routes() {
		if rt.Method == "GET" {
			continue
		}
		n++
		path := pathFor(rt.Path)
		for name, mod := range map[string]func(*reqOpts){
			"no header":       func(o *reqOpts) { o.noHeader = true },
			"foreign origin":  func(o *reqOpts) { o.origin = "https://evil.example" },
			"no origin":       func(o *reqOpts) { o.origin = "-" },
			"port differs":    func(o *reqOpts) { o.origin = "http://127.0.0.1:1" },
			"wrong header":    func(o *reqOpts) { o.header = map[string]string{auth.RequestHeader: "true"} },
			"origin with dir": func(o *reqOpts) { o.origin = e.ts.URL + "/x" },
		} {
			r := e.do(rt.Method, path, "{}", mod)
			if r.status != 403 || r.errCode() != "forbidden" {
				t.Errorf("%s (%s): %d %s, want 403 forbidden", rt.Pattern(), name, r.status, r.body)
			}
		}
	}
	if n < 40 {
		t.Fatalf("only %d change routes walked", n)
	}
	// Nothing reached JDownloader through any of those requests.
	if calls := e.jd.Calls(); len(calls) != 0 {
		t.Errorf("%d calls reached JDownloader: %v", len(calls), calls[0].Path)
	}
}

// No state-changing route answers a GET, and a GET route only reads: every call
// it makes to JDownloader is a read.
func TestGETRoutesOnlyRead(t *testing.T) {
	e := newEnv(t)
	readVerbs := []string{"get", "list", "query", "is", "ping", "uptime"}
	for _, rt := range e.srv.Routes() {
		if rt.Method != "GET" || rt.Path == "/api/v1/events" {
			continue
		}
		e.jd.ResetCalls()
		e.get(pathFor(rt.Path))
		for _, c := range e.jd.Calls() {
			name := c.Path[strings.LastIndex(c.Path, "/")+1:]
			ok := false
			for _, v := range readVerbs {
				ok = ok || strings.HasPrefix(name, v)
			}
			if !ok {
				t.Errorf("GET %s called %s, which is not a read", rt.Path, c.Path)
			}
		}
	}
	// And a GET on a route that only exists for POST is not an action.
	for _, p := range []string{"/api/v1/login", "/api/v1/logout", "/api/v1/controller/start", "/api/v1/downloads/actions/remove",
		"/api/v1/system/restart", "/api/v1/reconnect", "/api/v1/setup"} {
		r := e.get(p)
		e.expectErr(r, 404, "not_found")
	}
	if n := len(e.jd.Calls()); n != 0 {
		t.Errorf("GET of a POST-only path reached JDownloader (%d calls)", n)
	}
}

// Concept 9.3: headers on every answer, errors and files included.
func TestSecurityHeadersEverywhere(t *testing.T) {
	e := newEnv(t, func(o *envOptions) {
		o.static = fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	})
	const csp = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
		"connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	for _, c := range []struct {
		method, path string
		mods         []func(*reqOpts)
	}{
		{"GET", "/", nil}, {"GET", "/downloads", nil}, {"GET", "/healthz", nil}, {"GET", "/api/v1/status", nil},
		{"GET", "/api/v1/status", []func(*reqOpts){noSession}}, {"GET", "/api/v1/nope", nil},
		{"POST", "/api/v1/login", []func(*reqOpts){func(o *reqOpts) { o.noHeader = true }}}, {"GET", "/missing.js", nil},
		{"GET", "/api/v1/setup/status", nil}, {"POST", "/", nil},
	} {
		r := e.do(c.method, c.path, nil, c.mods...)
		h := r.header
		if got := h.Get("Content-Security-Policy"); got != csp {
			t.Errorf("%s %s: CSP = %q", c.method, c.path, got)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s %s: nosniff = %q, referrer = %q", c.method, c.path, h.Get("X-Content-Type-Options"), h.Get("Referrer-Policy"))
		}
	}
}

func TestAPIAnswersAreNotCached(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/v1/status", "/api/v1/session", "/api/v1/nope", "/api/v1/downloads"} {
		if cc := e.get(p).header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", p, cc)
		}
	}
}
