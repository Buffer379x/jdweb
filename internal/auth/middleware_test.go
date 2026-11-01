package auth

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func mustProxies(t *testing.T, s string) []netip.Prefix {
	t.Helper()
	p, err := ParseTrustedProxies(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 10.0.0.0/8 , 192.168.1.5,::1, fd00::/8 ")
	if err != nil || len(got) != 4 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[1].String() != "192.168.1.5/32" || got[2].String() != "::1/128" {
		t.Fatalf("single addresses must become host prefixes: %v", got)
	}
	if got, err := ParseTrustedProxies(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"nonsense", "10.0.0.0/33", "10.0.0.1/8x"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "http://jdweb.local:7468/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	plain := newEnv(t, Config{}).svc
	proxied := newEnv(t, Config{TrustedProxies: mustProxies(t, "10.0.0.0/8,172.16.0.1")}).svc
	cases := []struct {
		name string
		svc  *Service
		r    *http.Request
		want string
	}{
		{"direct", plain, req("203.0.113.9:5555", nil), "203.0.113.9"},
		{"untrusted peer cannot spoof", plain, req("203.0.113.9:5555", map[string]string{"X-Forwarded-For": "1.2.3.4"}), "203.0.113.9"},
		{"untrusted peer with proxies configured", proxied, req("203.0.113.9:5555", map[string]string{"X-Forwarded-For": "1.2.3.4"}), "203.0.113.9"},
		{"trusted proxy", proxied, req("10.1.2.3:4000", map[string]string{"X-Forwarded-For": "198.51.100.7"}), "198.51.100.7"},
		{"client-supplied prefix is ignored", proxied, req("10.1.2.3:4000", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7"}), "198.51.100.7"},
		{"two trusted hops", proxied, req("10.1.2.3:4000", map[string]string{"X-Forwarded-For": "198.51.100.7, 172.16.0.1"}), "198.51.100.7"},
		{"header split over lines", proxied, func() *http.Request {
			r := req("10.1.2.3:4000", nil)
			r.Header.Add("X-Forwarded-For", "6.6.6.6")
			r.Header.Add("X-Forwarded-For", "198.51.100.7")
			return r
		}(), "198.51.100.7"},
		{"trusted proxy without header", proxied, req("10.1.2.3:4000", nil), "10.1.2.3"},
		{"garbage header", proxied, req("10.1.2.3:4000", map[string]string{"X-Forwarded-For": "not-an-ip"}), "10.1.2.3"},
		{"all hops trusted", proxied, req("10.1.2.3:4000", map[string]string{"X-Forwarded-For": "10.9.9.9"}), "10.9.9.9"},
		{"ipv6", plain, req("[2001:db8::1]:80", nil), "2001:db8::1"},
		{"no port", plain, req("203.0.113.9", nil), "203.0.113.9"},
	}
	for _, c := range cases {
		if got := c.svc.ClientIP(c.r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsSecure(t *testing.T) {
	plain := newEnv(t, Config{}).svc
	proxied := newEnv(t, Config{TrustedProxies: mustProxies(t, "10.0.0.0/8")}).svc
	tlsReq := req("203.0.113.9:1", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	cases := []struct {
		name string
		svc  *Service
		r    *http.Request
		want bool
	}{
		{"plain http", plain, req("203.0.113.9:1", nil), false},
		{"direct TLS", plain, tlsReq, true},
		{"forwarded proto from untrusted peer", proxied, req("203.0.113.9:1", map[string]string{"X-Forwarded-Proto": "https"}), false},
		{"forwarded proto without any proxies", plain, req("10.1.1.1:1", map[string]string{"X-Forwarded-Proto": "https"}), false},
		{"forwarded https from trusted proxy", proxied, req("10.1.1.1:1", map[string]string{"X-Forwarded-Proto": "https"}), true},
		{"forwarded HTTPS list", proxied, req("10.1.1.1:1", map[string]string{"X-Forwarded-Proto": "HTTPS, http"}), true},
		{"forwarded http from trusted proxy", proxied, req("10.1.1.1:1", map[string]string{"X-Forwarded-Proto": "http"}), false},
	}
	for _, c := range cases {
		if got := c.svc.IsSecure(c.r); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	e := newEnv(t, Config{TrustedProxies: mustProxies(t, "10.0.0.0/8")})
	exp := e.clk.Now().Add(SessionTTL)

	w := httptest.NewRecorder()
	e.svc.SetSessionCookie(w, req("203.0.113.9:1", nil), "tok", exp)
	c := w.Result().Cookies()[0]
	if c.Name != "jdweb_session" || c.Value != "tok" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode ||
		c.Path != "/" || c.Secure || c.MaxAge != int(SessionTTL/time.Second) {
		t.Fatalf("cookie = %+v", c)
	}

	w = httptest.NewRecorder()
	e.svc.SetSessionCookie(w, req("10.1.1.1:1", map[string]string{"X-Forwarded-Proto": "https"}), "tok", exp)
	if c := w.Result().Cookies()[0]; !c.Secure {
		t.Fatal("Secure flag missing behind a trusted HTTPS proxy")
	}

	w = httptest.NewRecorder()
	e.svc.SetSessionCookie(w, req("203.0.113.9:1", nil), "tok", e.clk.Now())
	if c := w.Result().Cookies()[0]; c.MaxAge >= 0 {
		t.Fatalf("an expired session must delete the cookie, MaxAge = %d", c.MaxAge)
	}

	w = httptest.NewRecorder()
	e.svc.ClearSessionCookie(w, req("203.0.113.9:1", nil))
	c = w.Result().Cookies()[0]
	if c.Name != "jdweb_session" || c.MaxAge >= 0 || c.Value != "" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("clearing cookie = %+v", c)
	}
}

// run sends r through Middleware and RequireAuth and returns the recorder and the session the handler saw.
func run(svc *Service, r *http.Request) (*httptest.ResponseRecorder, *CurrentSession) {
	var seen *CurrentSession
	h := svc.Middleware(svc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := SessionFromContext(r.Context()); ok {
			seen = &s
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w, seen
}

func withCookie(r *http.Request, v string) *http.Request {
	r.AddCookie(&http.Cookie{Name: "jdweb_session", Value: v})
	return r
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (%s)", w.Code, status, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("body = %s (%v), want code %q", w.Body, err, code)
	}
}

func TestMiddlewareAndRequireAuth(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	res := e.mustLogin()

	w, seen := run(e.svc, req("203.0.113.9:1", nil))
	assertAPIError(t, w, 401, "unauthorized")
	if seen != nil {
		t.Fatal("handler ran without a session")
	}
	for _, bad := range []string{"garbage", strings.Repeat("a", 500), "", res.Token + "x"} {
		w, seen = run(e.svc, withCookie(req("203.0.113.9:1", nil), bad))
		assertAPIError(t, w, 401, "unauthorized")
		if seen != nil {
			t.Fatalf("cookie %q accepted", bad)
		}
	}

	w, seen = run(e.svc, withCookie(req("203.0.113.9:1", nil), res.Token))
	if w.Code != 204 || seen == nil {
		t.Fatalf("valid cookie: %d", w.Code)
	}
	if seen.ID != sha256hex(res.Token)[:16] || seen.TokenHash != sha256hex(res.Token) {
		t.Fatalf("session in context = %+v", seen)
	}
	if _, ok := SessionFromContext(req("1.1.1.1:1", nil).Context()); ok {
		t.Fatal("empty context has a session")
	}
}

func TestMiddlewarePassesThroughWithoutSession(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	reached := false
	h := e.svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	h.ServeHTTP(httptest.NewRecorder(), req("1.1.1.1:1", nil))
	if !reached {
		t.Fatal("Middleware alone must not reject (public routes)")
	}
}

func TestExpiredSessionIsRejectedAndRemoved(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	res := e.mustLogin()
	e.clk.Advance(SessionTTL + time.Second)
	w, seen := run(e.svc, withCookie(req("1.1.1.1:1", nil), res.Token))
	assertAPIError(t, w, 401, "unauthorized")
	if seen != nil {
		t.Fatal("expired session accepted")
	}
	if s, _ := e.db.ListSessions(e.ctx); len(s) != 0 {
		t.Fatal("expired session not deleted")
	}
}

func TestSessionSlidesAndCookieIsRefreshed(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	res := e.mustLogin()

	// Within the touch interval nothing is written.
	e.clk.Advance(time.Minute)
	w, _ := run(e.svc, withCookie(req("1.1.1.1:1", nil), res.Token))
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("cookie refreshed on every request")
	}
	s, _ := e.db.GetSession(e.ctx, sha256hex(res.Token))
	if !s.ExpiresAt.Equal(res.ExpiresAt) {
		t.Fatal("expiry moved too early")
	}

	e.clk.Advance(touchInterval)
	w, _ = run(e.svc, withCookie(req("1.1.1.1:1", nil), res.Token))
	s, _ = e.db.GetSession(e.ctx, sha256hex(res.Token))
	if want := e.clk.Now().Add(SessionTTL); !s.ExpiresAt.Equal(want) || !s.LastSeenAt.Equal(e.clk.Now()) {
		t.Fatalf("session after touch = %+v, want expiry %v", s, want)
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Value != res.Token || cs[0].MaxAge != int(SessionTTL/time.Second) {
		t.Fatalf("refreshed cookie = %+v", cs)
	}
}

func TestSignedOutSessionStopsWorking(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	a, b := e.mustLogin(), e.mustLogin()
	e.svc.Logout(e.ctx, sha256hex(a.Token))
	if w, _ := run(e.svc, withCookie(req("1.1.1.1:1", nil), a.Token)); w.Code != 401 {
		t.Fatal("logged-out session still works")
	}
	e.svc.SignOutOthers(e.ctx, sha256hex(a.Token))
	if w, _ := run(e.svc, withCookie(req("1.1.1.1:1", nil), b.Token)); w.Code != 401 {
		t.Fatal("session survived sign-out of others")
	}
}

func post(method, origin string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, "http://jdweb.local:7468/api/v1/x", nil)
	r.RemoteAddr = "203.0.113.9:1"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func runMutation(svc *Service, r *http.Request) *httptest.ResponseRecorder {
	h := svc.RequireMutation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRequireMutation(t *testing.T) {
	svc := newEnv(t, Config{}).svc
	ok := map[string]string{"X-JDWeb-Request": "1"}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if w := runMutation(svc, post(m, "http://jdweb.local:7468", ok)); w.Code != 204 {
			t.Errorf("%s with header and matching Origin: %d", m, w.Code)
		}
		w := runMutation(svc, post(m, "http://jdweb.local:7468", nil))
		assertAPIError(t, w, 403, "forbidden")
		for _, v := range []string{"0", "true", "", " 1"} {
			w := runMutation(svc, post(m, "http://jdweb.local:7468", map[string]string{"X-JDWeb-Request": v}))
			assertAPIError(t, w, 403, "forbidden")
		}
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		if w := runMutation(svc, post(m, "", nil)); w.Code != 204 {
			t.Errorf("%s must pass without header and Origin: %d", m, w.Code)
		}
	}
}

func TestRequireMutationOrigin(t *testing.T) {
	svc := newEnv(t, Config{}).svc
	ok := map[string]string{"X-JDWeb-Request": "1"}
	bad := []string{
		"",                        // missing
		"null",                    // sandboxed iframe, file://
		"https://evil.example",    // foreign site
		"http://jdweb.local:9999", // same host, other port
		"http://jdweb.local",      // port missing
		"http://jdweb.local:7468.evil.example",
		"http://evil.example/http://jdweb.local:7468",
		"jdweb.local:7468", // not an origin
		"http://",
	}
	for _, o := range bad {
		assertAPIError(t, runMutation(svc, post("POST", o, ok)), 403, "forbidden")
	}
	// Scheme does not matter for the host match, case does not either.
	for _, o := range []string{"https://jdweb.local:7468", "http://JDWEB.local:7468"} {
		if w := runMutation(svc, post("POST", o, ok)); w.Code != 204 {
			t.Errorf("Origin %q rejected: %d", o, w.Code)
		}
	}
}

func TestRequireMutationForwardedHostOnlyFromTrustedProxy(t *testing.T) {
	svc := newEnv(t, Config{TrustedProxies: mustProxies(t, "10.0.0.0/8")}).svc
	hdr := map[string]string{"X-JDWeb-Request": "1", "X-Forwarded-Host": "jd.example.com"}

	// From a trusted proxy: Origin must match the forwarded host.
	r := post("POST", "https://jd.example.com", hdr)
	r.RemoteAddr = "10.1.1.1:5000"
	if w := runMutation(svc, r); w.Code != 204 {
		t.Fatalf("forwarded host via trusted proxy: %d", w.Code)
	}
	r = post("POST", "http://jdweb.local:7468", hdr)
	r.RemoteAddr = "10.1.1.1:5000"
	assertAPIError(t, runMutation(svc, r), 403, "forbidden")

	// From anyone else the header is ignored: it cannot make a foreign Origin match.
	r = post("POST", "https://jd.example.com", hdr)
	assertAPIError(t, runMutation(svc, r), 403, "forbidden")
	r = post("POST", "http://jdweb.local:7468", hdr)
	if w := runMutation(svc, r); w.Code != 204 {
		t.Fatalf("untrusted peer, own Origin: %d", w.Code)
	}
}
