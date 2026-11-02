package api

import (
	"strings"
	"testing"
	"testing/fstest"
)

func appFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>JDWeb</title>")},
		"assets/app-9f3a.js":   {Data: []byte("console.log(1)")},
		"assets/app-9f3a.css":  {Data: []byte("body{}")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"manifest.webmanifest": {Data: []byte("{}")},
	}
}

// Static files and the app need no session: the login page must load.
func TestStaticFilesWithoutASession(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.static = appFS() })
	for _, c := range []struct{ path, ctype, cache, contains string }{
		{"/", "text/html", "no-cache", "JDWeb"},
		{"/index.html", "text/html", "no-cache", "JDWeb"},
		{"/assets/app-9f3a.js", "javascript", "public, max-age=31536000, immutable", "console.log"},
		{"/assets/app-9f3a.css", "text/css", "public, max-age=31536000, immutable", "body"},
		{"/favicon.svg", "image/svg+xml", "no-cache", "<svg"},
		// The app routes these itself.
		{"/downloads", "text/html", "no-cache", "JDWeb"},
		{"/settings/accounts", "text/html", "no-cache", "JDWeb"},
		{"/login", "text/html", "no-cache", "JDWeb"},
	} {
		r := e.get(c.path, noSession)
		if r.status != 200 || !strings.Contains(r.header.Get("Content-Type"), c.ctype) || !strings.Contains(string(r.body), c.contains) {
			t.Errorf("%s: %d %q %q", c.path, r.status, r.header.Get("Content-Type"), r.body)
		}
		if cc := r.header.Get("Cache-Control"); cc != c.cache {
			t.Errorf("%s: Cache-Control = %q, want %q", c.path, cc, c.cache)
		}
	}
}

// A missing file is a 404, not the app: HTML where a script was expected hides errors.
func TestMissingFilesAre404NotTheApp(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.static = appFS() })
	for _, p := range []string{"/assets/gone.js", "/gone.png", "/.env", "/assets/"} {
		r := e.get(p, noSession)
		if r.status != 404 || strings.Contains(string(r.body), "JDWeb") {
			t.Errorf("%s: %d %s", p, r.status, r.body)
		}
	}
	// Traversal does not leave the app.
	for _, p := range []string{"/../../etc/passwd", "/%2e%2e/%2e%2e/etc/passwd", "/assets/..%2f..%2fgo.mod"} {
		r := e.get(p, noSession)
		if strings.Contains(string(r.body), "module ") || strings.Contains(string(r.body), "root:") {
			t.Errorf("%s leaks a file: %s", p, r.body)
		}
	}
}

// The fallback never swallows the API: an unknown API path is a JSON 404.
func TestUnknownAPIPathIsJSON404(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.static = appFS() })
	for _, p := range []string{"/api", "/api/", "/api/v1", "/api/v1/nope", "/api/v2/status", "/api/v1/downloads/actions"} {
		for _, m := range []func(*reqOpts){noSession, nil} {
			var r result
			if m == nil {
				r = e.get(p)
			} else {
				r = e.get(p, m)
			}
			if r.status != 404 || r.errCode() != "not_found" || !strings.Contains(r.header.Get("Content-Type"), "json") {
				t.Errorf("GET %s: %d %q %s", p, r.status, r.header.Get("Content-Type"), r.body)
			}
		}
	}
}

// Without a built web app the answer is plain text that says what is missing.
func TestNoWebAppGivesAClearPlainAnswer(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{"nil": nil, "empty dist": {".gitkeep": {}}} {
		e := newEnv(t, func(o *envOptions) {
			if name != "nil" {
				o.static = fsys
			}
		})
		for _, p := range []string{"/", "/downloads"} {
			r := e.get(p, noSession)
			if r.status != 404 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/plain") ||
				!strings.Contains(string(r.body), "web interface is not part of this build") || strings.Contains(string(r.body), "<") {
				t.Errorf("%s %s: %d %q %q", name, p, r.status, r.header.Get("Content-Type"), r.body)
			}
		}
		// The API still works.
		e.expect(e.get("/healthz", noSession), 200)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t, withoutAccount)
	r := e.get("/healthz", noSession)
	if r.status != 200 || strings.TrimSpace(string(r.body)) != "ok" {
		t.Fatalf("healthz = %d %q", r.status, r.body)
	}
	if r := e.do("HEAD", "/healthz", nil, noSession); r.status != 200 {
		t.Fatalf("HEAD healthz = %d", r.status)
	}
}
