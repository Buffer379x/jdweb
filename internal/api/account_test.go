package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
)

// setupToken reads the one-time token from the log, where an operator finds it.
func (e *env) setupToken() string {
	e.t.Helper()
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil {
			if tok, ok := rec["setup_token"].(string); ok {
				return tok
			}
		}
	}
	e.t.Fatalf("no setup_token in the log: %s", e.logs.String())
	return ""
}

func cookieOf(r result, name string) *http.Cookie {
	resp := http.Response{Header: r.header}
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// The first start: setup with the token from the log, then login, then use.
func TestSetupAndLoginFlow(t *testing.T) {
	e := newEnv(t, withDebugLog, withoutAccount)
	if err := e.auth.InitSetup(ctxBg); err != nil {
		t.Fatal(err)
	}
	var st struct{ Required bool }
	r := e.get("/api/v1/setup/status", noSession)
	e.expect(r, 200)
	r.json(t, &st)
	if !st.Required {
		t.Fatal("setup is not required on a fresh database")
	}
	const pw = "a-long-enough-password-1"

	// T-SEC-02: no token or a wrong one is refused.
	for _, tok := range []string{"", "wrong-token"} {
		r = e.post("/api/v1/setup", map[string]any{"token": tok, "username": "owner", "password": pw}, noSession)
		if r.status != 400 || r.errCode() != "bad_request" {
			t.Errorf("token %q: %d %s", tok, r.status, r.body)
		}
	}
	r = e.post("/api/v1/setup", map[string]any{"token": e.setupToken(), "username": "owner", "password": "short"}, noSession)
	e.expectErr(r, 400, "bad_request")
	r = e.post("/api/v1/setup", map[string]any{"token": e.setupToken(), "username": "owner", "password": pw}, noSession)
	e.expect(r, 204)

	// T-SEC-03: setup is closed afterwards.
	r = e.post("/api/v1/setup", map[string]any{"token": e.setupToken(), "username": "other", "password": pw + "x"}, noSession)
	e.expectErr(r, 409, "conflict")
	r = e.get("/api/v1/setup/status", noSession)
	r.json(t, &st)
	if st.Required {
		t.Fatal("setup is still required")
	}

	// Nothing works before the login.
	e.expectErr(e.get("/api/v1/session", noSession), 401, "unauthorized")
	r = e.post("/api/v1/login", map[string]any{"username": "owner", "password": "wrong password!!"}, noSession)
	e.expectErr(r, 401, "unauthorized")
	if cookieOf(r, auth.CookieName) != nil {
		t.Fatal("a failed login sets a cookie")
	}
	r = e.post("/api/v1/login", map[string]any{"username": "owner", "password": pw}, noSession)
	e.expect(r, 204)
	c := cookieOf(r, auth.CookieName)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || len(c.Value) < 40 {
		t.Fatalf("cookie = %+v", c)
	}

	e.token = c.Value
	r = e.get("/api/v1/session")
	e.expect(r, 200)
	var sess struct {
		Username    string
		TotpEnabled bool
		Version     string
	}
	r.json(t, &sess)
	if sess.Username != "owner" || sess.TotpEnabled || sess.Version != "1.2.3-test" {
		t.Fatalf("session = %s", r.body)
	}
	r = e.get("/api/v1/sessions")
	e.expect(r, 200)
	var sessions []sessionDTO
	r.json(t, &sessions)
	if len(sessions) != 1 || !sessions[0].Current || len(sessions[0].ID) != 16 {
		t.Fatalf("sessions = %s", r.body)
	}
	if strings.Contains(string(r.body), c.Value) {
		t.Fatal("the session list shows the token")
	}

	// Logout ends the session for good.
	r = e.post("/api/v1/logout", nil)
	e.expect(r, 204)
	if cc := cookieOf(r, auth.CookieName); cc == nil || cc.MaxAge >= 0 {
		t.Fatalf("logout cookie = %+v", cc)
	}
	e.expectErr(e.get("/api/v1/session"), 401, "unauthorized")

	// T-SEC-11: no password, token or cookie value in the log, debug included.
	logs := e.logs.String()
	for _, secret := range []string{pw, "wrong password!!", c.Value, "wrong-token"} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log holds %q", secret)
		}
	}
	if n := strings.Count(logs, e.setupToken()); n != 1 {
		t.Errorf("the setup token appears %d times in the log, want once", n)
	}
}

func TestSignOutOthersAndPasswordChange(t *testing.T) {
	e := newEnv(t)
	other := "second-session-token-" + strings.Repeat("y", 20)
	e.addSession(other)
	r := e.get("/api/v1/sessions")
	var list []sessionDTO
	r.json(t, &list)
	if len(list) != 2 {
		t.Fatalf("sessions = %s", r.body)
	}
	e.expect(e.do("DELETE", "/api/v1/sessions", nil), 204)
	e.expectErr(e.get("/api/v1/session", func(o *reqOpts) { o.cookie = other; o.noSession = true }), 401, "unauthorized")
	e.expect(e.get("/api/v1/session"), 200)

	// A wrong current password is 403, not 401: the browser must not take it for an expired session.
	r = e.post("/api/v1/password", map[string]any{"current": "not the password", "new": "a-brand-new-password"})
	e.expectErr(r, 403, "forbidden")
	e.expectErr(e.post("/api/v1/password", map[string]any{"current": testPass, "new": "short"}), 400, "bad_request")
	e.expect(e.post("/api/v1/password", map[string]any{"current": testPass, "new": "a-brand-new-password"}), 204)
	e.expect(e.get("/api/v1/session"), 200) // this session stays
}

func TestTOTPRoutes(t *testing.T) {
	e := newEnv(t)
	e.expectErr(e.post("/api/v1/totp/enroll", map[string]any{"password": "wrong"}), 403, "forbidden")
	r := e.post("/api/v1/totp/enroll", map[string]any{"password": testPass})
	e.expect(r, 200)
	var en struct{ Secret, URI, QrSvg string }
	r.json(t, &en)
	if en.Secret == "" || !strings.HasPrefix(en.URI, "otpauth://totp/") || !strings.HasPrefix(en.QrSvg, "<svg") || strings.Contains(en.QrSvg, "<script") {
		t.Fatalf("enroll = %s", r.body)
	}
	e.expectErr(e.post("/api/v1/totp/confirm", map[string]any{"code": "000000"}), 400, "bad_request")
	e.expectErr(e.post("/api/v1/totp/disable", map[string]any{"password": testPass}), 409, "conflict")
}

// T-SEC-05: from the fifth failure the answer is 429 with Retry-After.
func TestLoginRateLimitAnswers429(t *testing.T) {
	e := newEnv(t, withoutAccount)
	if err := e.db.CreateUser(ctxBg, testUser, passwordHash(t), time.Now()); err != nil {
		t.Fatal(err)
	}
	var last result
	for i := 0; i < 8; i++ {
		last = e.post("/api/v1/login", map[string]any{"username": testUser, "password": "definitely wrong"}, noSession)
		if last.status == 429 {
			break
		}
	}
	e.expectErr(last, 429, "rate_limited")
	secs := last.header.Get("Retry-After")
	if secs == "" || secs == "0" {
		t.Fatalf("Retry-After = %q", secs)
	}
	// Even the right password waits.
	e.expectErr(e.post("/api/v1/login", map[string]any{"username": testUser, "password": testPass}, noSession), 429, "rate_limited")
}

func TestRateLimitAndBusyMapTo429WithRetryAfter(t *testing.T) {
	for name, err := range map[string]error{"limit": &auth.RateLimitError{RetryAfter: 2500 * time.Millisecond}, "busy": auth.ErrBusy} {
		ae, _ := toAPIError(err)
		if ae.Status != 429 || ae.Code != "rate_limited" || ae.RetryAfter <= 0 {
			t.Errorf("%s: %+v", name, ae)
		}
	}
	rec := newRecorder()
	writeError(rec, &apiError{Status: 429, Code: "rate_limited", Message: "x", RetryAfter: 2500 * time.Millisecond})
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After = %q, want 3 (rounded up)", got)
	}
}

// Logins and setup go through the Origin check as well.
func TestPublicPostsNeedOriginAndHeader(t *testing.T) {
	e := newEnv(t, withoutAccount)
	for _, p := range []string{"/api/v1/login", "/api/v1/setup"} {
		for _, mod := range []func(*reqOpts){
			func(o *reqOpts) { o.noHeader = true }, func(o *reqOpts) { o.origin = "https://evil.example" }, func(o *reqOpts) { o.origin = "-" },
		} {
			e.expectErr(e.post(p, map[string]any{"username": "a", "password": "b", "token": "c"}, noSession, mod), 403, "forbidden")
		}
	}
}

// The access log is for debugging only and holds no query, body or secret.
func TestAccessLogOnlyAtDebug(t *testing.T) {
	quiet := newEnv(t)
	quiet.get("/api/v1/status")
	if strings.Contains(quiet.logs.String(), `"msg":"request"`) {
		t.Error("an access log line at info level")
	}
	loud := newEnv(t, withDebugLog)
	loud.get("/api/v1/config?q=secretish-filter")
	loud.post("/api/v1/accounts", map[string]any{"hoster": "h", "username": "u", "password": "body-password-123"})
	logs := loud.logs.String()
	if !strings.Contains(logs, `"msg":"request"`) || !strings.Contains(logs, `"path":"/api/v1/status"`) && !strings.Contains(logs, `"path":"/api/v1/config"`) {
		t.Errorf("no access log at debug level: %s", logs)
	}
	for _, leak := range []string{"secretish-filter", "body-password-123", loud.token} {
		if strings.Contains(logs, leak) {
			t.Errorf("the access log holds %q", leak)
		}
	}
}

func TestPanicInAHandlerIsA500AndLogged(t *testing.T) {
	e := newEnv(t)
	h := e.srv.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom with /secret/path") }))
	rec := newRecorder()
	h.ServeHTTP(rec, httptestRequest())
	if rec.code != 500 || strings.Contains(rec.body.String(), "boom") || !strings.Contains(rec.body.String(), `"internal"`) {
		t.Fatalf("answer = %d %s", rec.code, rec.body.String())
	}
	if !strings.Contains(e.logs.String(), "panic in a handler") {
		t.Error("the panic was not logged")
	}
}
