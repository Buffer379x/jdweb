package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/store"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

func TestCaptchas(t *testing.T) {
	e := newEnv(t)
	img := e.jd.AddCaptcha(testjd.CaptchaJob{Hoster: "example.com", Type: "text", Category: "image captcha", Timeout: 60000, Created: 1700000000000,
		Image: "data:image/png;base64,iVBORw0KGgo="})
	rc := e.jd.AddCaptcha(testjd.CaptchaJob{Hoster: "other.com", Type: "RecaptchaV2Challenge", ChallengeType: "recaptchav2", Explain: "Solve on the site"})

	r := e.get("/api/v1/captchas")
	e.expect(r, 200)
	var list []captchaDTO
	r.json(t, &list)
	if len(list) != 2 || list[0].ID != fmt.Sprint(img) || list[0].Kind != "image" || list[1].Kind != "interactive" || list[0].Hoster != "example.com" {
		t.Fatalf("list = %+v", list)
	}
	if strings.Contains(string(r.body), "imageDataUrl") {
		t.Error("the list carries the pictures")
	}

	r = e.get("/api/v1/captchas/" + fmt.Sprint(img))
	e.expect(r, 200)
	var one captchaDTO
	r.json(t, &one)
	if one.Kind != "image" || one.ImageDataURL != "data:image/png;base64,iVBORw0KGgo=" || one.Timeout != 60000 {
		t.Fatalf("captcha = %+v", one)
	}
	r = e.get("/api/v1/captchas/" + fmt.Sprint(rc))
	e.expect(r, 200)
	one = captchaDTO{}
	r.json(t, &one)
	if one.Kind != "interactive" || one.ImageDataURL != "" || one.Explain != "Solve on the site" {
		t.Fatalf("interactive captcha = %+v", one)
	}

	e.expect(e.post("/api/v1/captchas/"+fmt.Sprint(img)+"/solve", map[string]any{"result": "XK42"}), 204)
	if got := argsJSON(e.lastCall("/captcha/solve")); got != fmt.Sprintf(`[%d,"XK42"]`, img) {
		t.Errorf("solve params = %s", got)
	}
	for _, how := range []string{"SINGLE", "BLOCK_HOSTER", "BLOCK_ALL_CAPTCHAS", "BLOCK_PACKAGE", "REFRESH", "STOP_CURRENT_ACTION", "TIMEOUT"} {
		e.jd.AddCaptcha(testjd.CaptchaJob{ID: 5000})
		e.jd.ResetCalls()
		e.expect(e.post("/api/v1/captchas/5000/skip", map[string]any{"type": how}), 204)
		if got := argsJSON(e.lastCall("/captcha/skip")); got != fmt.Sprintf(`[5000,"%s"]`, how) {
			t.Errorf("skip %s params = %s", how, got)
		}
	}
	e.jd.ResetCalls()
	for name, c := range map[string]struct {
		path string
		body any
	}{
		"skip type unknown": {"/api/v1/captchas/1/skip", map[string]any{"type": "FOREVER"}},
		"skip no type":      {"/api/v1/captchas/1/skip", map[string]any{}},
		"solve empty":       {"/api/v1/captchas/1/solve", map[string]any{"result": ""}},
		"solve bad id":      {"/api/v1/captchas/x/solve", map[string]any{"result": "a"}},
	} {
		if r := e.post(c.path, c.body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	if len(e.jd.Calls()) != 0 {
		t.Errorf("invalid requests reached JDownloader: %v", e.jd.Calls())
	}
}

func TestCaptchaWithAHostileImageDataURLIsNotPassedOn(t *testing.T) {
	e := newEnv(t)
	id := e.jd.AddCaptcha(testjd.CaptchaJob{Hoster: "h", Image: "javascript:alert(1)"})
	r := e.get("/api/v1/captchas/" + fmt.Sprint(id))
	e.expect(r, 200)
	if strings.Contains(string(r.body), "javascript") {
		t.Fatalf("answer = %s", r.body)
	}
}

func TestDialogs(t *testing.T) {
	e := newEnv(t)
	first := e.jd.AddDialog(testjd.Dialog{Type: "org.appwork.uio.ConfirmDialogInterface", Properties: map[string]string{"title": "<b>Sure?</b>", "message": "Really"}})
	e.jd.AddDialog(testjd.Dialog{Type: "org.jdownloader.IfFileExistsDialogInterface", Properties: map[string]string{"file": "a.bin"}})
	r := e.get("/api/v1/dialogs")
	e.expect(r, 200)
	var list []dialogDTO
	r.json(t, &list)
	if len(list) != 2 || list[0].ID != fmt.Sprint(first) || list[0].Properties["title"] != "<b>Sure?</b>" {
		t.Fatalf("dialogs = %+v", list)
	}
	r = e.get("/api/v1/dialogs/" + fmt.Sprint(first))
	e.expect(r, 200)
	var one dialogDTO
	r.json(t, &one)
	if one.Answer["closereason"] == "" {
		t.Fatalf("dialog = %+v, want the answer fields", one)
	}
	e.expect(e.post("/api/v1/dialogs/"+fmt.Sprint(first)+"/answer", map[string]any{"data": map[string]any{"closereason": "OK"}}), 204)
	if got := argsJSON(e.lastCall("/dialogs/answer")); got != fmt.Sprintf(`[%d,{"closereason":"OK"}]`, first) {
		t.Errorf("answer params = %s", got)
	}
	e.jd.ResetCalls()
	for name, body := range map[string]any{
		"no data":    map[string]any{},
		"nested":     map[string]any{"data": map[string]any{"a": map[string]any{"b": 1}}},
		"list value": map[string]any{"data": map[string]any{"a": []int{1}}},
		"long text":  map[string]any{"data": map[string]any{"a": strings.Repeat("x", 5000)}},
	} {
		if r := e.post("/api/v1/dialogs/1/answer", body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	if len(e.jd.Calls()) != 0 {
		t.Errorf("invalid answers reached JDownloader")
	}
	// JDownloader asks its questions in order.
	e.jd.FailNext("/dialogs/answer", 400, `{"src":"DEVICE","type":"Answer Dialog 12 first","data":null}`)
	r = e.post("/api/v1/dialogs/13/answer", map[string]any{"data": map[string]any{"closereason": "OK"}})
	if r.status != 409 || r.errCode() != "conflict" || !strings.Contains(r.errMessage(), "first") {
		t.Errorf("out of order: %d %s", r.status, r.body)
	}
}

func TestConnectionSettings(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	var c connectionDTO
	r := e.get("/api/v1/connection")
	e.expect(r, 200)
	r.json(t, &c)
	if c.URL != e.jd.URL || c.Source != "env" || c.Connection != "connected" {
		t.Fatalf("connection = %+v", c)
	}

	// What people type is cleaned: no scheme, a slash at the end, capitals.
	second := testjd.New(t)
	typed := strings.ToUpper(strings.TrimPrefix(second.URL, "http://")) + "/"
	r = e.do("PUT", "/api/v1/connection", map[string]any{"url": "  " + typed + "  "})
	e.expect(r, 200)
	r.json(t, &c)
	if c.URL != second.URL || c.Source != "ui" {
		t.Fatalf("after PUT: %+v, want %s", c, second.URL)
	}
	var saved string
	if err := e.db.GetJSON(ctxBg, settingJDURL, &saved); err != nil || saved != second.URL {
		t.Fatalf("saved = %q, %v", saved, err)
	}
	e.waitConnected()
	if e.engine.Client().BaseURL() != second.URL {
		t.Fatal("the engine did not switch")
	}
	if len(second.CallsTo("/device/ping")) == 0 {
		t.Fatal("the new JDownloader was not asked")
	}

	// Bad addresses: a clear message, nothing changes.
	for typedURL, hint := range map[string]string{
		"":                     "empty",
		"ftp://host:3128":      "scheme",
		"http://":              "no host",
		"http://host:99999":    "port",
		"http://user:pw@host":  "user and password",
		"http://host:3128?x=1": "query",
		"http://exa mple.com":  "invalid",
	} {
		r := e.do("PUT", "/api/v1/connection", map[string]any{"url": typedURL})
		if r.status != 400 || r.errCode() != "bad_request" || !strings.Contains(strings.ToLower(r.errMessage()), hint) {
			t.Errorf("%q: %d %s, want 400 with %q", typedURL, r.status, r.body, hint)
		}
	}
	if e.engine.Client().BaseURL() != second.URL {
		t.Error("a bad address changed the connection")
	}
	if strings.Contains(e.logs.String(), "user:pw") || strings.Contains(string(r.body), "pw@") {
		t.Error("credentials of an address leak")
	}
}

func TestConnectionToTheWrongServiceShowsAHint(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	other := newOtherService(t)
	e.expect(e.do("PUT", "/api/v1/connection", map[string]any{"url": other}), 200)
	var st state.Status
	eventually(t, "misconfigured", func() bool {
		r := e.get("/api/v1/connection")
		var c connectionDTO
		r.json(t, &c)
		return c.Connection == "misconfigured" && strings.Contains(c.Detail, "another service")
	})
	r := e.get("/api/v1/status")
	r.json(t, &st)
	if st.Connection != state.ConnMisconfigured {
		t.Fatalf("status = %+v", st)
	}
}

func TestDiagnosticsAndAcknowledge(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/v1/diagnostics")
	e.expect(r, 200)
	var d struct {
		Checks       []struct{ ID, Status string }
		Acknowledged bool
	}
	r.json(t, &d)
	if len(d.Checks) != 4 || d.Acknowledged || d.Checks[0].ID != "api-reachable" || d.Checks[0].Status != "ok" {
		t.Fatalf("diagnostics = %s", r.body)
	}
	r = e.post("/api/v1/diagnostics/acknowledge", nil)
	e.expect(r, 200)
	var ack bool
	if err := e.db.GetJSON(ctxBg, settingAcknowledged, &ack); err != nil || !ack {
		t.Fatalf("stored = %v, %v", ack, err)
	}
	r = e.get("/api/v1/diagnostics")
	r.json(t, &d)
	if !d.Acknowledged {
		t.Fatal("not acknowledged after the confirmation")
	}
	// A new address deserves a new look.
	e.expect(e.do("PUT", "/api/v1/connection", map[string]any{"url": e.jd.URL}), 200)
	r = e.get("/api/v1/diagnostics")
	r.json(t, &d)
	if d.Acknowledged {
		t.Fatal("still acknowledged after the address changed")
	}
}

func TestIconsAreProxiedAndCached(t *testing.T) {
	e := newEnv(t)
	for _, kind := range []string{"hoster", "file"} {
		path := "/api/v1/icons/" + kind + "/example.com"
		e.jd.ResetCalls()
		first := e.get(path)
		second := e.get(path)
		e.expect(first, 200)
		if first.header.Get("Content-Type") != "image/png" || first.header.Get("Cache-Control") != "private, max-age=86400" {
			t.Errorf("%s: %q, %q", kind, first.header.Get("Content-Type"), first.header.Get("Cache-Control"))
		}
		if string(first.body) != string(second.body) || len(first.body) < 20 {
			t.Errorf("%s: icons differ or are empty", kind)
		}
		n := len(e.jdCalls("/contentV2/getFavIcon")) + len(e.jdCalls("/contentV2/getFileIcon"))
		if n != 1 {
			t.Errorf("%s: %d calls to JDownloader for two requests", kind, n)
		}
	}
	for _, bad := range []string{"%00", "a%0Ab", strings.Repeat("a", 300)} {
		if r := e.get("/api/v1/icons/hoster/" + bad); r.status != 400 {
			t.Errorf("icon %q: %d", bad, r.status)
		}
	}
}

func TestIconCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := newIconCache(3, 1<<20)
	for _, k := range []string{"a", "b", "c"} {
		c.put(k, icon{data: []byte(k)})
	}
	c.get("a") // a is fresh again
	c.put("d", icon{data: []byte("d")})
	if _, ok := c.get("b"); ok {
		t.Error("b should have been evicted")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s was evicted", k)
		}
	}
	for i := 0; i < 5000; i++ {
		c.put(fmt.Sprint(i), icon{})
	}
	if c.len() != 3 {
		t.Errorf("cache holds %d entries, max is 3", c.len())
	}
	if big := newIconCache(iconCacheEntries, iconCacheBytes); big.max != 512 || big.maxBytes != 32<<20 {
		t.Errorf("the default cache size is %d, want 512", big.max)
	}
}

func TestPrefs(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/v1/prefs")
	e.expect(r, 200)
	if strings.TrimSpace(string(r.body)) != "{}" {
		t.Fatalf("empty prefs = %s", r.body)
	}
	e.expect(e.do("PUT", "/api/v1/prefs", `{"theme":"dark","columns":["name","size"],"n":1}`), 204)
	r = e.get("/api/v1/prefs")
	if strings.TrimSpace(string(r.body)) != `{"theme":"dark","columns":["name","size"],"n":1}` {
		t.Fatalf("prefs = %s", r.body)
	}
	for _, bad := range []string{`[1]`, `"x"`, `3`, `{`, `null`} {
		if r := e.do("PUT", "/api/v1/prefs", bad); r.status != 400 {
			t.Errorf("prefs %s: %d", bad, r.status)
		}
	}
	// A rejected write keeps the old value.
	if r := e.get("/api/v1/prefs"); !strings.Contains(string(r.body), "dark") {
		t.Fatal("prefs lost")
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{jdTimeout(), 503, "jd_offline"},
		{fmt.Errorf("wrapped: %w", errNoJD), 503, "jd_offline"},
		{&jd.Error{Type: jd.TypeInternal, Status: 500}, 502, "jd_error"},
		{&jd.Error{Type: jd.TypeFileNotFound, Status: 404}, 404, "jd_error"},
		{&jd.Error{Type: jd.TypeNotImplemented, Status: 501}, 502, "jd_error"},
		{&jd.Error{Type: "Answer Dialog 1 first", Status: 400}, 409, "conflict"},
		{store.ErrNotFound, 404, "not_found"},
		// Plain context errors are no JDownloader outage: the browser left, or a database call ran out of time.
		{context.DeadlineExceeded, 500, "internal"},
		{fmt.Errorf("db: %w", context.DeadlineExceeded), 500, "internal"},
		{context.Canceled, 503, "internal"},
		{fmt.Errorf("disk on fire"), 500, "internal"},
	}
	for _, c := range cases {
		ae, _ := toAPIError(c.err)
		if ae.Status != c.status || ae.Code != c.code {
			t.Errorf("%v: %d %s, want %d %s", c.err, ae.Status, ae.Code, c.status, c.code)
		}
	}
	// An internal error never shows its text.
	if ae, logged := toAPIError(fmt.Errorf("sqlite: secret path /data/jdweb.db")); strings.Contains(ae.Message, "sqlite") || logged == nil {
		t.Errorf("internal error: %q, logged %v", ae.Message, logged)
	}
}

func jdTimeout() error { return &jd.Error{Type: jd.TypeConnection, Raw: "dial tcp: i/o timeout"} }

func TestHTTPServerSettings(t *testing.T) {
	e := newEnv(t)
	hs := e.srv.HTTPServer(":0")
	if hs.ReadHeaderTimeout != 5*time.Second || hs.ReadTimeout != 30*time.Second || hs.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts: header %v read %v idle %v", hs.ReadHeaderTimeout, hs.ReadTimeout, hs.IdleTimeout)
	}
	// Longer than the 5 s throttle of a login under pressure, shorter than forever.
	if hs.WriteTimeout < 30*time.Second || hs.WriteTimeout != 60*time.Second {
		t.Errorf("WriteTimeout = %v, want 60s", hs.WriteTimeout)
	}
	if hs.MaxHeaderBytes <= 0 || hs.MaxHeaderBytes > 1<<20 {
		t.Errorf("MaxHeaderBytes = %d", hs.MaxHeaderBytes)
	}
}

func TestLimitListener(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := LimitListener(base, 2)
	defer l.Close()
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	dial := func() net.Conn {
		c, err := net.Dial("tcp", base.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	var clients []net.Conn
	for i := 0; i < 3; i++ {
		clients = append(clients, dial())
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	var servers []net.Conn
	for i := 0; i < 2; i++ {
		select {
		case c := <-accepted:
			servers = append(servers, c)
		case <-time.After(2 * time.Second):
			t.Fatal("the first two connections were not accepted")
		}
	}
	select {
	case <-accepted:
		t.Fatal("a third connection was accepted over the limit of 2")
	case <-time.After(150 * time.Millisecond):
	}
	servers[0].Close()
	select {
	case c := <-accepted:
		c.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting connection was not accepted after one closed")
	}
	servers[0].Close() // closing twice must not free a second slot
	servers[1].Close()
	if _, ok := l.(interface{ Addr() net.Addr }); !ok {
		t.Fatal("not a listener")
	}
}

var _ = http.StatusOK

var ctxBg = context.Background()

// newOtherService is a web server that is not JDownloader.
func newOtherService(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Router login</body></html>"))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestIconCacheIsBoundedInBytes(t *testing.T) {
	c := newIconCache(1000, 10_000)
	blob := make([]byte, 3000)
	for i := 0; i < 20; i++ {
		c.put(fmt.Sprint(i), icon{data: blob, contentType: "image/png"})
	}
	if c.size() > 10_000 || c.len() > 3 {
		t.Fatalf("cache holds %d bytes in %d entries, max 10000", c.size(), c.len())
	}
	if _, ok := c.get("19"); !ok {
		t.Fatal("the newest icon was evicted")
	}
}

func TestIconTypesAreWhitelisted(t *testing.T) {
	for ct, want := range map[string]bool{
		"image/png": true, "image/PNG; charset=x": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/x-icon": true,
		"image/svg+xml": false, "text/html": false, "application/octet-stream": false, "": false, "image/bmp": false,
	} {
		if _, ok := iconType(ct); ok != want {
			t.Errorf("iconType(%q) = %v, want %v", ct, ok, want)
		}
	}
}

// A failed lookup is remembered for five minutes, a connection problem is not.
func TestIconFailuresAreRememberedBriefly(t *testing.T) {
	e := newEnv(t)
	clock := time.Now()
	e.srv.icons.now = func() time.Time { return clock }
	const path = "/api/v1/icons/hoster/hostile%3Cname"
	e.jd.FailNext("/contentV2/getFavIcon", 400, `{"src":"DEVICE","type":"BAD_PARAMETERS","data":null}`)
	first := e.get(path)
	if first.status != 502 || first.errCode() != "jd_error" {
		t.Fatalf("first = %d %s", first.status, first.body)
	}
	second := e.get(path)
	if second.status != 502 || len(e.jdCalls("/contentV2/getFavIcon")) != 1 {
		t.Fatalf("second = %d, %d calls to JDownloader, want the failure from the cache", second.status, len(e.jdCalls("/contentV2/getFavIcon")))
	}
	clock = clock.Add(5*time.Minute + time.Second)
	if r := e.get(path); r.status != 200 || len(e.jdCalls("/contentV2/getFavIcon")) != 2 {
		t.Fatalf("after five minutes: %d, %d calls", r.status, len(e.jdCalls("/contentV2/getFavIcon")))
	}
	// Offline is transient and never remembered.
	e.jd.Down()
	if r := e.get("/api/v1/icons/file/x.bin"); r.status != 503 {
		t.Fatalf("offline = %d", r.status)
	}
	e.jd.Up()
	if r := e.get("/api/v1/icons/file/x.bin"); r.status != 200 {
		t.Fatalf("after Up = %d %s", r.status, r.body)
	}
}

// PUT /connection: when the engine refuses, the saved address does not stay.
func TestConnectionNotKeptWhenTheSwitchFails(t *testing.T) {
	e := newEnv(t)
	e.srv.setURL = func(string) error { return fmt.Errorf("engine refuses") }
	r := e.do("PUT", "/api/v1/connection", map[string]any{"url": "http://other.host:3128"})
	e.expectErr(r, 500, "internal")
	var saved string
	if err := e.db.GetJSON(ctxBg, settingJDURL, &saved); err == nil {
		t.Fatalf("the address %q stayed saved", saved)
	}
	// With an earlier saved address, that one stays.
	if err := e.db.SetJSON(ctxBg, settingJDURL, "http://earlier:3128"); err != nil {
		t.Fatal(err)
	}
	e.expectErr(e.do("PUT", "/api/v1/connection", map[string]any{"url": "http://other.host:3128"}), 500, "internal")
	if err := e.db.GetJSON(ctxBg, settingJDURL, &saved); err != nil || saved != "http://earlier:3128" {
		t.Fatalf("saved = %q, %v, want the earlier address", saved, err)
	}
}
