package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

func TestStatusIsTheEngineStatusWithNormalizedController(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.jd.SetController("STOPPED_STATE")
	e.waitConnected()
	var st state.Status
	eventually(t, "controller IDLE", func() bool {
		r := e.get("/api/v1/status")
		r.json(t, &st)
		return r.status == 200 && st.Controller == "IDLE" && st.JD.URL == e.jd.URL
	})
	if st.Connection != state.ConnConnected || !st.JD.Docker || st.JD.Revision != 50639 {
		t.Fatalf("status = %+v", st)
	}
	// The raw state never leaks.
	if r := e.get("/api/v1/status"); strings.Contains(string(r.body), "STOPPED_STATE") {
		t.Fatalf("status shows the raw controller state: %s", r.body)
	}
}

func TestListsHaveStringIDsAndKeepHostileNames(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	names := []string{"<script>alert(1)</script>", strings.Repeat("x", 500), "‮RTL‬ evil.exe", "emoji \U0001F4E6\U0001F680 done", `"quoted" & <b>`}
	var links []testjd.Link
	for _, n := range names {
		links = append(links, testjd.Link{Name: n, BytesTotal: 5})
	}
	pkg, linkIDs := e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "<img src=x onerror=alert(1)>"}, links...)
	e.waitConnected()
	eventually(t, "links loaded", func() bool { return len(e.mirror.Snapshot(state.Downloads).DlLinks) == len(names) })

	r := e.get("/api/v1/downloads")
	e.expect(r, 200)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q", r.header.Get("Content-Type"))
	}
	var raw struct {
		Packages []map[string]any
		Links    []map[string]any
	}
	r.json(t, &raw)
	if len(raw.Packages) != 1 || raw.Packages[0]["id"] != fmt.Sprint(pkg) || raw.Packages[0]["name"] != "<img src=x onerror=alert(1)>" {
		t.Fatalf("packages = %v", raw.Packages)
	}
	for i, l := range raw.Links {
		for _, k := range []string{"id", "packageId"} {
			if _, ok := l[k].(string); !ok {
				t.Errorf("link %d: %s = %#v is not a string", i, k, l[k])
			}
		}
		if l["id"] != fmt.Sprint(linkIDs[i]) || l["packageId"] != fmt.Sprint(pkg) {
			t.Errorf("link %d: ids %v %v", i, l["id"], l["packageId"])
		}
		if l["name"] != names[i] {
			t.Errorf("link %d: name %q changed to %q", i, names[i], l["name"])
		}
	}
	// Linkgrabber has the same shape.
	e.expect(e.get("/api/v1/linkgrabber"), 200)
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListsAreGzippedWhenAccepted(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	var links []testjd.Link
	for i := 0; i < 300; i++ {
		links = append(links, testjd.Link{Name: fmt.Sprintf("file-%04d.bin", i), BytesTotal: 100})
	}
	e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "big"}, links...)
	e.waitConnected()
	eventually(t, "loaded", func() bool { return len(e.mirror.Snapshot(state.Downloads).DlLinks) == 300 })

	for _, p := range []string{"/api/v1/downloads", "/api/v1/linkgrabber"} {
		plain := e.get(p)
		zipped := e.get(p, func(o *reqOpts) { o.header = map[string]string{"Accept-Encoding": "gzip, deflate"} })
		if plain.header.Get("Content-Encoding") != "" {
			t.Errorf("%s: compressed for a client that did not ask", p)
		}
		if zipped.header.Get("Content-Encoding") != "gzip" || !strings.Contains(zipped.header.Get("Vary"), "Accept-Encoding") {
			t.Fatalf("%s: Content-Encoding = %q, Vary = %q", p, zipped.header.Get("Content-Encoding"), zipped.header.Get("Vary"))
		}
		if !bytes.Equal(gunzip(t, zipped.body), plain.body) {
			t.Errorf("%s: the compressed body differs", p)
		}
		if p == "/api/v1/downloads" && len(zipped.body)*5 > len(plain.body) {
			t.Errorf("%s: %d bytes compressed from %d, hardly smaller", p, len(zipped.body), len(plain.body))
		}
	}
	for _, ae := range []string{"gzip;q=0", "identity", "br", ""} {
		r := e.get("/api/v1/downloads", func(o *reqOpts) { o.header = map[string]string{"Accept-Encoding": ae} })
		if r.header.Get("Content-Encoding") != "" {
			t.Errorf("Accept-Encoding %q got %q", ae, r.header.Get("Content-Encoding"))
		}
	}
}

// sseClient reads an event stream line by line.
type sseClient struct {
	resp  *http.Response
	lines chan string
}

func (e *env) openStream(mods ...func(*http.Request)) *sseClient {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/events", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: e.token})
	req.Header.Set("Accept-Encoding", "gzip")
	for _, m := range mods {
		m(req)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	c := &sseClient{resp: resp, lines: make(chan string, 4096)}
	go func() {
		defer close(c.lines)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
	}()
	e.t.Cleanup(func() { resp.Body.Close() })
	return c
}

// next returns the next line or fails after the timeout.
func (c *sseClient) next(t *testing.T, within time.Duration) (string, bool) {
	t.Helper()
	select {
	case l, ok := <-c.lines:
		return l, ok
	case <-time.After(within):
		t.Fatalf("no line within %v", within)
		return "", false
	}
}

// waitFor reads lines until one has the prefix, and returns it.
func (c *sseClient) waitFor(t *testing.T, prefix string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		l, ok := c.next(t, time.Until(deadline))
		if !ok {
			t.Fatalf("stream ended while waiting for %q", prefix)
		}
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
}

func TestEventStreamSendsStatusThenEvents(t *testing.T) {
	e := newEnv(t, withRun, withFastSync, func(o *envOptions) { o.keepalive = 40 * time.Millisecond })
	e.waitConnected()
	c := e.openStream()
	if c.resp.StatusCode != 200 || c.resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %q", c.resp.StatusCode, c.resp.Header.Get("Content-Type"))
	}
	if c.resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("the event stream is compressed, which would delay events")
	}
	if cc := c.resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if c.resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("no CSP on the stream")
	}
	// First comes the state the browser needs, in the format of docs/api.md.
	c.waitFor(t, "event: status", 3*time.Second)
	data := c.waitFor(t, "data: ", 3*time.Second)
	var st state.Status
	if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &st); err != nil || st.Connection != state.ConnConnected {
		t.Fatalf("first status event: %v %s", err, data)
	}
	c.waitFor(t, "event: connection", 3*time.Second)

	// A keepalive comment comes on its own.
	c.waitFor(t, ": keepalive", 3*time.Second)

	// A change in JDownloader arrives as a patch.
	_, ids := e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "live"}, testjd.Link{Name: "l.bin", BytesTotal: 1000, BytesLoaded: 10, Running: true, Speed: 5})
	e.engine.Resync(state.Downloads) // the direct mutator of the fake sends no event
	c.waitFor(t, "event: structure", 5*time.Second)
	e.jd.UpdateLink(testjd.Downloads, ids[0], func(l *testjd.Link) { l.BytesLoaded = 500 })
	e.jd.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.bytesLoaded", EventData: map[string]any{"uuid": ids[0], "bytesLoaded": 500}})
	c.waitFor(t, "event: patch", 5*time.Second)
	got := c.waitFor(t, "data: ", 3*time.Second)
	if !strings.Contains(got, fmt.Sprint(ids[0])) || !strings.Contains(got, `"bytesLoaded":500`) {
		t.Fatalf("patch = %s", got)
	}
}

func TestEventStreamCountsAsAnOpenBrowser(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	if n := e.hub.Subscribers(); n != 0 {
		t.Fatalf("%d subscribers before any browser", n)
	}
	c := e.openStream()
	c.waitFor(t, "event: status", 3*time.Second)
	eventually(t, "one subscriber", func() bool { return e.hub.Subscribers() == 1 })
	// While a browser is attached the engine sets the faster status interval.
	eventually(t, "the engine switched to the attached rate", func() bool {
		return len(e.jdCalls("/downloadevents/setStatusEventInterval")) >= 2
	})
	c.resp.Body.Close()
	eventually(t, "no subscriber after the browser left", func() bool { return e.hub.Subscribers() == 0 })
	// ...and back to the slow rate once the last browser left (Detach).
	eventually(t, "the engine switched back to the detached rate", func() bool {
		return len(e.jdCalls("/downloadevents/setStatusEventInterval")) >= 3
	})
}

func TestEventStreamEndsWhenTheHubDropsASlowBrowser(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	// A browser that never reads: a raw connection.
	conn, err := net.Dial("tcp", strings.TrimPrefix(e.ts.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /api/v1/events HTTP/1.1\r\nHost: x\r\nCookie: %s=%s\r\n\r\n", auth.CookieName, e.token)
	eventually(t, "the stream is subscribed", func() bool { return e.hub.Subscribers() == 1 })

	big := bytes.Repeat([]byte("x"), 256<<10)
	go func() {
		for i := 0; i < 600 && e.hub.Subscribers() > 0; i++ {
			e.hub.Publish(sse.Message{Event: "status", Data: big})
			time.Sleep(time.Millisecond)
		}
	}()
	eventually(t, "the hub dropped the browser", func() bool { return e.hub.Subscribers() == 0 })

	// Reading on reaches the end of the stream: the server closed it, the
	// browser would reconnect and load the lists again.
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil { // returns at the end of the chunked body
		t.Fatalf("the stream did not end cleanly after the drop: %v", err)
	}
}

func TestEventStreamEndsOnShutdown(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	c := e.openStream()
	c.waitFor(t, "event: status", 3*time.Second)
	e.srv.CloseStreams()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-c.lines:
			if !ok {
				// And a new stream is refused while closing.
				r := e.get("/api/v1/events")
				if r.status != 503 {
					t.Fatalf("stream after CloseStreams: %d", r.status)
				}
				return
			}
		case <-deadline:
			t.Fatal("the stream is still open after CloseStreams")
		}
	}
}

// writeEvent splits data with line breaks into data fields.
func TestWriteEventFraming(t *testing.T) {
	var buf bytes.Buffer
	rc := http.NewResponseController(nopFlusher{&buf})
	if err := writeEvent(&buf, rc, "patch", []byte("{\"a\":1}\nsecond\r\nthird")); err != nil {
		t.Fatal(err)
	}
	want := "event: patch\ndata: {\"a\":1}\ndata: second\ndata: third\n\n"
	if buf.String() != want {
		t.Fatalf("frame = %q, want %q", buf.String(), want)
	}
}

type nopFlusher struct{ io.Writer }

func (nopFlusher) Header() http.Header { return http.Header{} }
func (nopFlusher) WriteHeader(int)     {}
func (n nopFlusher) Flush()            {}
