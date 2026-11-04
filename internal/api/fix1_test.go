package api

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

// streamEnds waits for the end of an event stream.
func (c *sseClient) ends(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case _, ok := <-c.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("the stream is still open after %v", within)
		}
	}
}

func (e *env) openStreamAs(token string) *sseClient {
	e.t.Helper()
	return e.openStream(func(r *http.Request) {
		r.Header.Del("Cookie")
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	})
}

// A stream does not outlive its session: logout, sign out others, password
// change and expiry end it, the first three at once.
func TestEventStreamEndsWithItsSession(t *testing.T) {
	t.Run("logout", func(t *testing.T) {
		e := newEnv(t, withRun, withFastSync)
		e.waitConnected()
		c := e.openStream()
		c.waitFor(t, "event: status", 3*time.Second)
		e.expect(e.post("/api/v1/logout", nil), 204)
		c.ends(t, 3*time.Second)
	})
	t.Run("sign out others", func(t *testing.T) {
		e := newEnv(t, withRun, withFastSync)
		e.waitConnected()
		other := "other-device-token-" + strings.Repeat("z", 24)
		e.addSession(other)
		victim := e.openStreamAs(other)
		keeper := e.openStream()
		victim.waitFor(t, "event: status", 3*time.Second)
		keeper.waitFor(t, "event: status", 3*time.Second)
		e.expect(e.do("DELETE", "/api/v1/sessions", nil), 204)
		victim.ends(t, 3*time.Second)
		// The session that asked stays, and its stream with it.
		e.jd.Emit(testjdEvent())
		select {
		case _, ok := <-keeper.lines:
			if !ok {
				t.Fatal("the stream of the session that stayed was closed")
			}
		case <-time.After(300 * time.Millisecond):
		}
		if e.hub.Subscribers() != 1 {
			t.Fatalf("%d subscribers, want 1", e.hub.Subscribers())
		}
	})
	t.Run("password change", func(t *testing.T) {
		e := newEnv(t, withRun, withFastSync)
		e.waitConnected()
		other := "other-device-token-" + strings.Repeat("z", 24)
		e.addSession(other)
		victim := e.openStreamAs(other)
		victim.waitFor(t, "event: status", 3*time.Second)
		e.expect(e.post("/api/v1/password", map[string]any{"current": testPass, "new": "a-brand-new-password"}), 204)
		victim.ends(t, 3*time.Second)
	})
	t.Run("expiry is seen at the keepalive", func(t *testing.T) {
		e := newEnv(t, withRun, withFastSync, func(o *envOptions) { o.keepalive = 50 * time.Millisecond })
		e.waitConnected()
		c := e.openStream()
		c.waitFor(t, "event: status", 3*time.Second)
		if _, err := e.db.W.Exec("UPDATE session SET expires_at = ?", time.Now().Add(-time.Minute).Unix()); err != nil {
			t.Fatal(err)
		}
		c.ends(t, 3*time.Second)
	})
	t.Run("deleted behind the server's back", func(t *testing.T) {
		// `jdweb reset-password` runs in another process and ends sessions in the database only.
		e := newEnv(t, withRun, withFastSync, func(o *envOptions) { o.keepalive = 50 * time.Millisecond })
		e.waitConnected()
		c := e.openStream()
		c.waitFor(t, "event: status", 3*time.Second)
		if _, err := e.db.W.Exec("DELETE FROM session"); err != nil {
			t.Fatal(err)
		}
		c.ends(t, 3*time.Second)
	})
}

// A slow upload leaves the response its own time: the write deadline of the
// server starts with the request, so it has to move on big routes. Otherwise the
// browser sees a broken connection after a successful add and sends it again.
func TestSlowBigUploadStillGetsItsAnswer(t *testing.T) {
	e := newEnv(t)
	hs := httptest.NewUnstartedServer(e.srv.Handler())
	hs.Config.WriteTimeout = 300 * time.Millisecond
	hs.Start()
	defer hs.Close()

	post := func(path, body string, pause time.Duration) (int, error) {
		conn, err := net.Dial("tcp", strings.TrimPrefix(hs.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nX-JDWeb-Request: 1\r\nCookie: %s=%s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n",
			path, strings.TrimPrefix(hs.URL, "http://"), hs.URL, auth.CookieName, e.token, len(body))
		half := len(body) / 2
		fmt.Fprint(conn, body[:half])
		time.Sleep(pause)
		fmt.Fprint(conn, body[half:])
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}
	body := `{"links":["http://example.com/a"]}`
	if code, err := post("/api/v1/linkgrabber/add", body, 600*time.Millisecond); err != nil || code != 200 {
		t.Fatalf("slow add: %d, %v", code, err)
	}
	if n := len(e.jdCalls("/linkgrabberv2/addLinks")); n != 1 {
		t.Fatalf("%d addLinks calls", n)
	}
	// An ordinary route keeps the server's deadline: the answer is lost, which is the point of the test.
	if _, err := post("/api/v1/downloads/actions/disable", `{"linkIds":["5"]}`, 600*time.Millisecond); err == nil {
		t.Log("a small route survived a slow body too; the server deadline did not fire")
	}
}

func TestStaticHeadersTypesAndHiddenFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":                 {Data: []byte("<!doctype html>JDWeb")},
		"assets/app-1.js":            {Data: []byte("x")},
		"assets/font-1.woff2":        {Data: []byte("w")},
		"assets/app-1.css":           {Data: []byte("c")},
		"manifest.webmanifest":       {Data: []byte("{}")},
		"icon.svg":                   {Data: []byte("<svg/>")},
		".vite/manifest.json":        {Data: []byte(`{"secret":"build"}`)},
		".gitkeep":                   {},
		".env":                       {Data: []byte("TOKEN=1")},
		"assets/.hidden":             {Data: []byte("h")},
		"assets/.vite/manifest.json": {Data: []byte("{}")},
	}
	e := newEnv(t, func(o *envOptions) { o.static = fsys })
	for path, ct := range map[string]string{
		"/assets/app-1.js": "text/javascript; charset=utf-8", "/assets/font-1.woff2": "font/woff2", "/assets/app-1.css": "text/css; charset=utf-8",
		"/manifest.webmanifest": "application/manifest+json", "/icon.svg": "image/svg+xml", "/": "text/html; charset=utf-8",
	} {
		r := e.get(path, noSession)
		if r.status != 200 || r.header.Get("Content-Type") != ct {
			t.Errorf("%s: %d %q, want %q", path, r.status, r.header.Get("Content-Type"), ct)
		}
	}
	for _, p := range []string{"/.vite/manifest.json", "/.gitkeep", "/.env", "/assets/.hidden", "/assets/.vite/manifest.json", "/%2evite/manifest.json"} {
		r := e.get(p, noSession)
		if r.status != 404 || strings.Contains(string(r.body), "build") || strings.Contains(string(r.body), "TOKEN") || strings.Contains(string(r.body), "JDWeb") {
			t.Errorf("%s: %d %s", p, r.status, r.body)
		}
	}
	// ETag of the content: a second visit costs a 304, a changed file a new tag.
	first := e.get("/", noSession)
	tag := first.header.Get("ETag")
	if tag == "" || !strings.HasPrefix(tag, `"`) {
		t.Fatalf("ETag = %q", tag)
	}
	r := e.get("/", noSession, func(o *reqOpts) { o.header = map[string]string{"If-None-Match": tag} })
	if r.status != 304 || len(r.body) != 0 {
		t.Fatalf("revalidation = %d %q", r.status, r.body)
	}
	if e.get("/assets/app-1.js", noSession).header.Get("ETag") != "" {
		t.Error("an immutable asset needs no ETag")
	}
	fsys["index.html"] = &fstest.MapFile{Data: []byte("<!doctype html>JDWeb v2")}
	if e.get("/", noSession).header.Get("ETag") == tag {
		t.Error("the ETag did not change with the content")
	}
}

// Reading the type from the key alone would fail open: an unknown or empty
// type is masked, only known switches and numbers are shown.
func TestIsSecretFailsClosed(t *testing.T) {
	for _, c := range []struct {
		key, typ string
		want     bool
	}{
		{"RouterPassword", "STRING", true}, {"RouterPassword", "", true}, {"ApiKey", "SOMETHING_NEW", true},
		{"PasswordList", "STRING_LIST", true}, {"Secret", "OBJECT", true}, {"Token", "OBJECT_LIST", true}, {"token", "HEX_COLOR", true},
		{"PasswordProtectionEnabled", "BOOLEAN", false}, {"TokenCount", "INT", false}, {"SecretSize", "LONG", false},
		{"ApiKeyRatio", "DOUBLE", false}, {"TokenType", "ENUM", false},
		{"Name", "STRING", false}, {"Name", "", false}, {"Anything", "OBJECT", false},
	} {
		if got := isSecret(c.key, c.typ); got != c.want {
			t.Errorf("isSecret(%q, %q) = %v, want %v", c.key, c.typ, got, c.want)
		}
	}
	// And the masked shape follows the value for a type nobody knows.
	if got := string(maskJSON([]byte(`["a","b"]`), "")); got != `["`+maskValue+`"]` {
		t.Errorf("list of unknown type: %s", got)
	}
	if got := string(maskJSON([]byte(`{"user":"u","pw":"p"}`), "OBJECT")); got != `"`+maskValue+`"` {
		t.Errorf("object: %s", got)
	}
	if got := string(maskJSON([]byte(`null`), "")); got != "null" {
		t.Errorf("null: %s", got)
	}
}

func TestArchiveSecretsAreMaskedAndRevealed(t *testing.T) {
	e := newEnv(t, withDebugLog)
	const final, listed = "final-pw-7", "list-pw-8"
	e.jd.AddArchive(testjdArchive("arch-1", "/output/x", final, []string{listed, "second"}))
	e.jd.AddArchive(testjdArchive("arch-2", "", "", nil))

	r := e.post("/api/v1/extraction/archives", map[string]any{"linkIds": []string{"1"}})
	e.expect(r, 200)
	for _, leak := range []string{final, listed, "second"} {
		if strings.Contains(string(r.body), leak) {
			t.Fatalf("the archive list holds %q: %s", leak, r.body)
		}
	}
	var out []struct {
		ArchiveID string
		Settings  struct {
			ExtractPath, FinalPassword string
			Passwords                  []string
			Secret                     bool
		}
	}
	r.json(t, &out)
	if len(out) != 2 || !out[0].Settings.Secret || out[0].Settings.FinalPassword != maskValue ||
		len(out[0].Settings.Passwords) != 1 || out[0].Settings.Passwords[0] != maskValue || out[0].Settings.ExtractPath != "/output/x" {
		t.Fatalf("archives = %s", r.body)
	}
	// Not set stays visibly empty.
	if s := out[1].Settings; s.FinalPassword != "" || len(s.Passwords) != 0 || !s.Secret {
		t.Fatalf("empty archive = %+v", s)
	}

	rev := e.post("/api/v1/extraction/archives/arch-1/reveal", nil)
	e.expect(rev, 200)
	if !strings.Contains(string(rev.body), final) || !strings.Contains(string(rev.body), listed) || !strings.Contains(string(rev.body), "second") {
		t.Fatalf("reveal = %s", rev.body)
	}
	if !strings.Contains(e.logs.String(), "archive passwords revealed") {
		t.Error("a reveal leaves no trace")
	}

	// The mask is no value.
	e.jd.ResetCalls()
	for name, body := range map[string]map[string]any{
		"final mask": {"finalPassword": maskValue}, "list mask": {"passwords": []string{maskValue}}, "list with mask": {"passwords": []string{"new", maskValue}},
	} {
		if r := e.do("PUT", "/api/v1/extraction/archives/arch-1", body); r.status != 400 || !strings.Contains(r.errMessage(), "mask") {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	if len(e.jdCalls("/extraction/setArchiveSettings")) != 0 {
		t.Fatal("a mask reached JDownloader")
	}
	// Changing the path alone keeps the passwords (JDownloader would clear a missing list).
	e.expect(e.do("PUT", "/api/v1/extraction/archives/arch-1", map[string]any{"extractPath": "/output/y"}), 204)
	a := e.jd.Archives()[0]
	if a.ExtractPath != "/output/y" || a.FinalPassword != final || len(a.Passwords) != 2 || a.Passwords[0] != listed {
		t.Fatalf("archive after a path change = %+v", a)
	}
	// An empty finalPassword is sent explicitly. JDownloader ignores it (as the real one does),
	// and JDWeb must not call that done.
	e.jd.ResetCalls()
	if r := e.do("PUT", "/api/v1/extraction/archives/arch-1", map[string]any{"finalPassword": ""}); r.status != 409 || r.errCode() != "jd_error" {
		t.Fatalf("clearing the final password: %d %s, want 409 jd_error", r.status, r.body)
	}
	if got := argsJSON(e.lastCall("/extraction/setArchiveSettings")); !strings.Contains(got, `"finalPassword":""`) {
		t.Fatalf("the empty string was not sent: %s", got)
	}
	if a := e.jd.Archives()[0]; a.FinalPassword != final || a.ExtractPath != "/output/y" || len(a.Passwords) != 2 {
		t.Fatalf("archive after the refused clear = %+v", a)
	}
	// A real new list replaces it, an empty one clears it.
	e.expect(e.do("PUT", "/api/v1/extraction/archives/arch-1", map[string]any{"passwords": []string{"fresh"}}), 204)
	if a := e.jd.Archives()[0]; len(a.Passwords) != 1 || a.Passwords[0] != "fresh" {
		t.Fatalf("passwords = %v", a.Passwords)
	}
	e.expect(e.do("PUT", "/api/v1/extraction/archives/arch-1", map[string]any{"passwords": []string{}}), 204)
	if a := e.jd.Archives()[0]; len(a.Passwords) != 0 {
		t.Fatalf("passwords = %v", a.Passwords)
	}
	for _, s := range []string{final, listed, "fresh"} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("the log holds %q", s)
		}
	}
	e.expectErr(e.post("/api/v1/extraction/archives/"+strings.Repeat("a", 600)+"/reveal", nil), 400, "bad_request")
}

func testjdEvent() testjd.Event {
	return testjd.Event{Publisher: "downloads", EventID: "UNKNOWN_EVENT_FOR_TEST"}
}

func testjdArchive(id, path, final string, pws []string) testjd.Archive {
	return testjd.Archive{ID: id, ExtractPath: path, FinalPassword: final, Passwords: pws}
}
