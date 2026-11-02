package sync_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
	jdsync "github.com/Buffer379x/jdweb/internal/sync"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

const (
	pathListen   = "/events/listen"
	pathPackages = "/downloadsV2/queryPackages"
	pathLinks    = "/downloadsV2/queryLinks"
	pathLgPkgs   = "/linkgrabberv2/queryPackages"
	pathToolbar  = "/toolbar/getStatus"
	pathInterval = "/downloadevents/setStatusEventInterval"
	pathSubStat  = "/events/getsubscriptionstatus"
)

// fast is the test timing: everything a test waits for happens within moments.
func fast() jdsync.Config {
	return jdsync.Config{
		PatchInterval: 10 * time.Millisecond, Debounce: 5 * time.Millisecond, MinReloadGap: 20 * time.Millisecond,
		StructureCheck: time.Hour, PollAttached: time.Hour, PollDetached: time.Hour,
		BackoffMin: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond,
		StableAfter: time.Millisecond, ListenRetry: 10 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// fixture is a running engine with a browser-like reader on the hub.
type fixture struct {
	e     *jdsync.Engine
	store *state.Store
	hub   *sse.Hub
	inbox *inbox
	stop  func()
}

func run(t *testing.T, client *jd.Client, cfg jdsync.Config) *fixture {
	t.Helper()
	store, hub := state.New(), sse.New()
	in := watch(t, hub)
	e := jdsync.New(client, store, hub, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	var once gosync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("Run did not end within 10 s after the cancel")
			}
		})
	}
	t.Cleanup(stop)
	return &fixture{e, store, hub, in, stop}
}

func start(t *testing.T, srv *testjd.Server, cfg jdsync.Config) *fixture {
	t.Helper()
	return run(t, jd.New(srv.URL), cfg)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within 10 s: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *fixture) waitConn(t *testing.T, want state.Connection) state.Status {
	t.Helper()
	var st state.Status
	deadline := time.Now().Add(10 * time.Second)
	for {
		st = f.e.Status()
		if st.Connection == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("connection = %q (%s), want %q", st.Connection, st.ConnectionDetail, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// inbox keeps what a subscriber of the hub received.
type inbox struct {
	mu   gosync.Mutex
	msgs []sse.Message
}

func watch(t *testing.T, hub *sse.Hub) *inbox {
	t.Helper()
	ch, cancel := hub.Subscribe()
	in := &inbox{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range ch {
			in.mu.Lock()
			in.msgs = append(in.msgs, m)
			in.mu.Unlock()
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
	return in
}

// has reports whether a message of the event type holds all the given parts.
func (in *inbox) has(event string, parts ...string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
next:
	for _, m := range in.msgs {
		if m.Event != event {
			continue
		}
		for _, p := range parts {
			if !strings.Contains(string(m.Data), p) {
				continue next
			}
		}
		return true
	}
	return false
}

func (in *inbox) count(event, part string) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	n := 0
	for _, m := range in.msgs {
		if m.Event == event && strings.Contains(string(m.Data), part) {
			n++
		}
	}
	return n
}

func (in *inbox) reset() {
	in.mu.Lock()
	in.msgs = nil
	in.mu.Unlock()
}

func delta(kind string, id int64, field string, value any) testjd.Event {
	return testjd.Event{Publisher: "downloads", EventID: kind + "." + field, EventData: map[string]any{"uuid": id, field: value}}
}

// progress moves a link forward like a download does: in the fake and as an event,
// so that a reload at any moment finds the same value.
func progress(srv *testjd.Server, link, loaded int64) {
	srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.BytesLoaded = loaded })
	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", loaded))
}

func addRunning(srv *testjd.Server, name string) (pkg, link int64) {
	p, l := srv.AddPackage(testjd.Downloads, testjd.Package{Name: name},
		testjd.Link{Name: name + ".bin", BytesTotal: 1000, BytesLoaded: 100, Speed: 50, ETA: 18, Running: true})
	return p, l[0]
}

func dlLink(t *testing.T, s *state.Store, id int64) state.DlLink {
	t.Helper()
	for _, l := range s.Snapshot(state.Downloads).DlLinks {
		if l.ID == fmt.Sprint(id) {
			return l
		}
	}
	t.Fatalf("link %d is not in the mirror", id)
	return state.DlLink{}
}

func hasLink(s *state.Store, id int64) bool {
	return slices.ContainsFunc(s.Snapshot(state.Downloads).DlLinks, func(l state.DlLink) bool { return l.ID == fmt.Sprint(id) })
}

func hasLinkNamed(s *state.Store, name string) bool {
	return slices.ContainsFunc(s.Snapshot(state.Downloads).DlLinks, func(l state.DlLink) bool { return l.Name == name })
}

// T-SYN-01
func TestStartWithReachableJD(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	srv.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "lg"}, testjd.Link{Name: "lg.bin", BytesTotal: 7})

	f := start(t, srv, fast())
	st := f.waitConn(t, state.ConnConnected)

	if got := dlLink(t, f.store, link); got.Name != "dl.bin" || !got.Running || got.Speed != 50 || got.ETA != 18 || got.State != state.LinkRunning {
		t.Errorf("link = %+v", got)
	}
	lg := f.store.Snapshot(state.Linkgrabber)
	if len(lg.LgLinks) != 1 || lg.LgLinks[0].Name != "lg.bin" {
		t.Errorf("linkgrabber = %+v", lg)
	}
	if st.ConnectionDetail != "" {
		t.Errorf("detail = %q", st.ConnectionDetail)
	}
	if st.JD.Revision != 50639 || st.JD.URL != srv.URL {
		t.Errorf("jd = %+v", st.JD)
	}
	if st.Downloads.Packages != 1 || st.Downloads.Links != 1 || st.Downloads.BytesTotal != 1000 || st.Downloads.Running != 1 || st.Linkgrabber.Links != 1 {
		t.Errorf("totals = %+v %+v", st.Downloads, st.Linkgrabber)
	}
	if st.Speed != 50 || st.Controller != "IDLE" {
		t.Errorf("status = %+v", st)
	}

	// Exactly the six publishers, no browser yet so the slow event interval.
	subs := srv.CallsTo("/events/subscribe")
	if len(subs) != 1 {
		t.Fatalf("subscribes = %d", len(subs))
	}
	var pubs []string
	_ = json.Unmarshal(subs[0].Args()[0], &pubs)
	slices.Sort(pubs)
	want := jd.Publishers()
	slices.Sort(want)
	if !slices.Equal(pubs, want) {
		t.Errorf("publishers = %v", pubs)
	}
	eventually(t, "connected announced", func() bool {
		return f.inbox.has("connection", `{"connection":"connected","detail":""}`) &&
			f.inbox.has("structure", `{"list":"downloads"}`) && f.inbox.has("structure", `{"list":"linkgrabber"}`) &&
			f.inbox.has("status", `"connection":"connected"`)
	})
}

// T-SYN-02 as decided: refused without ever having connected is misconfigured,
// with the remedy. After one good connection a failure is offline.
func TestWithoutJDMisconfiguredThenOffline(t *testing.T) {
	srv := testjd.New(t)
	srv.Down()
	f := start(t, srv, fast())

	st := f.waitConn(t, state.ConnMisconfigured)
	for _, part := range []string{"DeprecatedApiEnabled", "DeprecatedApiLocalhostOnly", srv.URL} {
		if !strings.Contains(st.ConnectionDetail, part) {
			t.Errorf("detail %q does not name %q", st.ConnectionDetail, part)
		}
	}
	eventually(t, "connection event", func() bool { return f.inbox.has("connection", `"connection":"misconfigured"`, "DeprecatedApiEnabled") })

	srv.Up()
	f.waitConn(t, state.ConnConnected)

	srv.Down()
	st = f.waitConn(t, state.ConnOffline)
	if !strings.Contains(st.ConnectionDetail, srv.URL) || strings.Contains(st.ConnectionDetail, "DeprecatedApi") {
		t.Errorf("offline detail = %q", st.ConnectionDetail)
	}

	srv.Up()
	f.waitConn(t, state.ConnConnected)
}

// Review Focus 3: another service on the port gets its own hint.
func TestOtherServiceOnThePort(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"html page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>nginx</html>")
		},
		"not found page": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"redirect":       func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusFound) },
		"unknown path":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotImplemented) },
		"forbidden":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"server error":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"json, ping false": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":false}`)
		},
		"json, no data": func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"status":"ok"}`) },
	} {
		t.Run(name, func(t *testing.T) {
			other := httptest.NewServer(h)
			defer other.Close()
			f := run(t, jd.New(other.URL), fast())
			st := f.waitConn(t, state.ConnMisconfigured)
			if st.ConnectionDetail != state.DetailOtherService {
				t.Errorf("detail = %q", st.ConnectionDetail)
			}
			f.stop()
		})
	}
}

// Review Focus 3: every kind of bad address has a different, clear hint.
func TestHintsDiffer(t *testing.T) {
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	a := run(t, jd.New(other.URL), fast()).waitConn(t, state.ConnMisconfigured).ConnectionDetail

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := "http://" + ln.Addr().String()
	ln.Close()
	b := run(t, jd.New(free), fast()).waitConn(t, state.ConnMisconfigured).ConnectionDetail

	c := run(t, nil, fast()).waitConn(t, state.ConnMisconfigured).ConnectionDetail
	if a == b || b == c || a == c || a == "" || b == "" || c == "" {
		t.Errorf("hints: %q, %q, %q", a, b, c)
	}
}

func TestNoAddressYetThenSetURL(t *testing.T) {
	srv := testjd.New(t)
	f := run(t, nil, fast())
	st := f.waitConn(t, state.ConnMisconfigured)
	if st.ConnectionDetail != state.DetailNoURL {
		t.Errorf("detail = %q", st.ConnectionDetail)
	}
	if f.e.Client() != nil {
		t.Error("client without an address")
	}
	if err := f.e.SetURL(srv.URL); err != nil {
		t.Fatal(err)
	}
	f.waitConn(t, state.ConnConnected)
	if f.e.Client() == nil || f.e.Client().BaseURL() != srv.URL {
		t.Errorf("client = %v", f.e.Client())
	}
}

// Review Focus 1, the heart of this package: JDownloader goes away while links
// run and a browser is open.
func TestRestartWhileDownloadsRun(t *testing.T) {
	srv := testjd.New(t)
	pkg, link := addRunning(srv, "dl")
	srv.SetController("RUNNING")
	f := start(t, srv, fast())
	f.e.Attach()
	st := f.waitConn(t, state.ConnConnected)
	eventually(t, "the controller known", func() bool { st = f.e.Status(); return st.Controller == "RUNNING" && st.Speed == 50 })
	firstSub := srv.Subscriptions()
	f.inbox.reset()

	// It restarts: connections cut, subscriptions forgotten, and when it is back
	// the link did not get far.
	srv.Down()
	srv.DropSubscription()
	st = f.waitConn(t, state.ConnOffline)

	// The browser is told, and nothing keeps looking as if it was running.
	got := dlLink(t, f.store, link)
	if got.Running || got.Speed != 0 || got.ETA != state.ETAUnknown || got.State == state.LinkRunning {
		t.Errorf("link while offline = %+v", got)
	}
	if got.Name != "dl.bin" || got.BytesLoaded != 100 {
		t.Errorf("last known values lost: %+v", got)
	}
	snap := f.store.Snapshot(state.Downloads)
	if !snap.Stale {
		t.Error("mirror not marked stale")
	}
	if p := snap.DlPackages[0]; p.Running || p.Speed != 0 || p.ETA != state.ETAUnknown {
		t.Errorf("package while offline = %+v", p)
	}
	if st.Speed != 0 || st.Downloads.Running != 0 || st.Downloads.ETA != state.ETAUnknown || st.Controller != "IDLE" {
		t.Errorf("status while offline = %+v", st)
	}
	eventually(t, "patch and events", func() bool {
		return f.inbox.has("patch", fmt.Sprint(link), `"running":false`, `"speed":0`, `"eta":null`, `"state":"waiting"`) &&
			f.inbox.has("connection", `"connection":"offline"`) &&
			f.inbox.has("status", `"connection":"offline"`, `"speed":0`)
	})

	// Back with another picture: the old link finished, a new one runs.
	if !srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.Running, l.Speed, l.ETA, l.Finished, l.BytesLoaded = false, 0, 0, true, 1000 }) {
		t.Fatal("link gone")
	}
	newLink := srv.AddLink(testjd.Downloads, pkg, testjd.Link{Name: "new.bin", BytesTotal: 10, Running: true, Speed: 5, ETA: 2})
	f.inbox.reset()
	srv.Up()
	st = f.waitConn(t, state.ConnConnected)

	if got := dlLink(t, f.store, link); !got.Finished || got.Running || got.State != state.LinkFinished || got.BytesLoaded != 1000 {
		t.Errorf("old link after the return = %+v", got)
	}
	if got := dlLink(t, f.store, newLink); !got.Running || got.Speed != 5 {
		t.Errorf("new link after the return = %+v", got)
	}
	if f.store.Stale() {
		t.Error("still stale")
	}
	subs := srv.Subscriptions()
	if len(subs) != 1 || slices.Equal(subs, firstSub) {
		t.Errorf("subscriptions = %v, before %v", subs, firstSub)
	}
	eventually(t, "resync announced", func() bool {
		return f.inbox.has("structure", `{"list":"downloads"}`) && f.inbox.has("structure", `{"list":"linkgrabber"}`) &&
			f.inbox.has("connection", `"connection":"connected"`)
	})

	// And the events flow again.
	srv.Emit(delta("LINK_UPDATE", newLink, "bytesLoaded", 7))
	eventually(t, "progress after the return", func() bool { return dlLink(t, f.store, newLink).BytesLoaded == 7 })
}

// T-SYN-04: the subscription is lost, without any connection problem.
func TestLostSubscriptionByListen404(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	before := srv.Subscriptions()
	links, pkgs, lg := len(srv.CallsTo(pathLinks)), len(srv.CallsTo(pathPackages)), len(srv.CallsTo(pathLgPkgs))
	f.inbox.reset()

	srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.BytesLoaded = 500 })
	srv.DropSubscription()

	eventually(t, "a new subscription", func() bool { s := srv.Subscriptions(); return len(s) == 1 && !slices.Equal(s, before) })
	f.waitConn(t, state.ConnConnected)
	eventually(t, "the full load", func() bool {
		return len(srv.CallsTo(pathLinks)) > links && len(srv.CallsTo(pathPackages)) > pkgs && len(srv.CallsTo(pathLgPkgs)) > lg && dlLink(t, f.store, link).BytesLoaded == 500
	})
	eventually(t, "announced", func() bool {
		return f.inbox.has("connection", `"connection":"syncing"`) && f.inbox.has("structure", `{"list":"downloads"}`) && f.inbox.has("structure", `{"list":"linkgrabber"}`)
	})
	// The new subscription works.
	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", 600))
	eventually(t, "events on the new subscription", func() bool { return dlLink(t, f.store, link).BytesLoaded == 600 })
}

// The other signal of a lost subscription: getsubscriptionstatus answers
// subscriptionid -1, while subscribed is false either way.
func TestLostSubscriptionByStatusMinusOne(t *testing.T) {
	srv := testjd.New(t)
	cfg := fast()
	cfg.StructureCheck = 20 * time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)

	// A live subscription with subscribed:false must not count as lost.
	eventually(t, "status checks", func() bool { return len(srv.CallsTo(pathSubStat)) >= 3 })
	if n := len(srv.CallsTo("/events/subscribe")); n != 1 {
		t.Fatalf("subscribed %d times although the subscription lives", n)
	}

	srv.FailNext(pathSubStat, 200, `{"data":{"queueSize":0,"subscriptionid":-1,"subscribed":false}}`)
	eventually(t, "a new subscription", func() bool { return len(srv.CallsTo("/events/subscribe")) >= 2 })
	f.waitConn(t, state.ConnConnected)
}

func TestDeltaBecomesPatchAndTotals(t *testing.T) {
	srv := testjd.New(t)
	pkg, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	calls := len(srv.CallsTo(pathLinks))

	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", 250))
	srv.Emit(delta("PACKAGE_UPDATE", pkg, "bytesLoaded", 250))
	eventually(t, "patch", func() bool {
		return f.inbox.has("patch", `"list":"downloads"`, fmt.Sprint(link), `"bytesLoaded":250`)
	})
	eventually(t, "totals", func() bool { return f.e.Status().Downloads.BytesLoaded == 250 })
	eventually(t, "status event", func() bool { return f.inbox.has("status", `"bytesLoaded":250`) })
	if n := len(srv.CallsTo(pathLinks)); n != calls {
		t.Errorf("a plain delta caused %d list loads", n-calls)
	}
}

func TestLinkgrabberDeltaBecomesPatch(t *testing.T) {
	srv := testjd.New(t)
	_, links := srv.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "lg"}, testjd.Link{Name: "lg.bin"})
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	srv.Emit(testjd.Event{Publisher: "linkcollector", EventID: "LINK_UPDATE.name", EventData: map[string]any{"uuid": links[0], "name": "renamed.bin"}})
	eventually(t, "patch", func() bool { return f.inbox.has("patch", `"list":"linkgrabber"`, "renamed.bin") })
}

// T-SYN-06
func TestEventForUnknownLinkReloadsThatList(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	lg := len(srv.CallsTo(pathLgPkgs))
	f.inbox.reset()

	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "late"}, testjd.Link{Name: "late.bin"}) // no event, like an unnoticed change
	srv.Emit(delta("LINK_UPDATE", ids[0], "bytesLoaded", 1))

	eventually(t, "the link in the mirror", func() bool { return hasLink(f.store, ids[0]) })
	eventually(t, "structure event", func() bool { return f.inbox.has("structure", `{"list":"downloads"}`) })
	if n := len(srv.CallsTo(pathLgPkgs)); n != lg {
		t.Errorf("the other list was loaded too, %d times", n-lg)
	}
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
}

// T-SYN-08
func TestUnknownEventsAreIgnored(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	calls := len(srv.CallsTo(pathLinks))

	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "BRAND_NEW", EventData: map[string]any{"x": 1}})
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.shiny", EventData: map[string]any{"uuid": link, "shiny": true}})
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.speed", EventData: "not an object"})
	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", 111))

	eventually(t, "the event behind them", func() bool { return dlLink(t, f.store, link).BytesLoaded == 111 })
	if st := f.e.Status(); st.Connection != state.ConnConnected || st.ConnectionDetail != "" {
		t.Errorf("status = %+v", st)
	}
	if n := len(srv.CallsTo(pathLinks)); n != calls {
		t.Errorf("list loads = %d", n-calls)
	}
}

// With the real timing: a burst is one reload, and reloads are 2 s apart.
func TestStructureEventsAreDebouncedWithRealTiming(t *testing.T) {
	srv := testjd.New(t)
	cfg := jdsync.DefaultConfig()
	cfg.Logger = fast().Logger
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	base := len(srv.CallsTo(pathPackages))
	reloads := func() int { return len(srv.CallsTo(pathPackages)) - base }

	for range 5 {
		srv.Emit(testjd.Event{Publisher: "downloads", EventID: "ADD_LINK"})
	}
	eventually(t, "the first reload", func() bool { return reloads() >= 1 })
	first := time.Now()

	for range 5 {
		srv.Emit(testjd.Event{Publisher: "downloads", EventID: "REFRESH_STRUCTURE"})
	}
	eventually(t, "the second reload", func() bool { return reloads() >= 2 })
	if gap := time.Since(first); gap < 1800*time.Millisecond {
		t.Errorf("second reload after %v", gap)
	}
	if n := reloads(); n != 2 {
		t.Errorf("reloads = %d for two bursts", n)
	}
}

// An event that arrives while a reload is under way is newer than the answer of
// that reload. If it were applied first the answer would overwrite it, and a
// link that stopped would stay "running" for good.
func TestEventDuringReloadIsAppliedAfterIt(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")

	var hook gosync.Mutex
	var afterLinks func()
	client := jd.New(srv.URL, jd.WithHTTPClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(r)
		if r.URL.Path == pathLinks {
			hook.Lock()
			fn := afterLinks
			afterLinks = nil
			hook.Unlock()
			if fn != nil {
				fn()
			}
		}
		return resp, err
	})}))
	f := run(t, client, fast())
	f.waitConn(t, state.ConnConnected)

	eventDone := make(chan struct{})
	hook.Lock()
	afterLinks = func() {
		defer close(eventDone)
		// JDownloader answered the query with running:true, and only now the link stops.
		srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.Running, l.Speed, l.ETA = false, 0, 0 })
		srv.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.running", EventData: map[string]any{"uuid": link}})
		// The listener takes it from the queue while this reload still runs. Give
		// an engine that applies events right away the chance to do so: the time
		// is spent proving that nothing happens.
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			if srv.Queued(srv.Subscriptions()[0]) == 0 && !mirrorRunning(f.store, link) {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	hook.Unlock()
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "REFRESH_STRUCTURE"})

	select {
	case <-eventDone:
	case <-time.After(10 * time.Second):
		t.Fatal("no reload within 10 s")
	}
	eventually(t, "the link stopped", func() bool {
		l := dlLink(t, f.store, link)
		return !l.Running && l.Speed == 0
	})
	// It stays that way.
	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", 101))
	eventually(t, "the next event", func() bool { return dlLink(t, f.store, link).BytesLoaded == 101 })
	if l := dlLink(t, f.store, link); l.Running || l.State == state.LinkRunning {
		t.Errorf("link = %+v", l)
	}
}

func mirrorRunning(s *state.Store, id int64) bool {
	for _, l := range s.Snapshot(state.Downloads).DlLinks {
		if l.ID == fmt.Sprint(id) {
			return l.Running
		}
	}
	return false
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The safety net of Concept 5.3: a change nobody announced is found through the
// structure counters.
func TestStructureCounterFindsSilentChanges(t *testing.T) {
	srv := testjd.New(t)
	cfg := fast()
	cfg.StructureCheck = 20 * time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	base := len(srv.CallsTo(pathPackages))

	// While the counters stand still nothing is loaded.
	dlCounter := "/downloadsV2/getStructureChangeCounter"
	mark := len(srv.CallsTo(dlCounter))
	eventually(t, "counter checks", func() bool { return len(srv.CallsTo(dlCounter)) >= mark+3 })
	if n := len(srv.CallsTo(pathPackages)); n != base {
		t.Errorf("%d reloads without a change", n-base)
	}

	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "silent"}, testjd.Link{Name: "silent.bin"})
	eventually(t, "the silent link", func() bool { return hasLink(f.store, ids[0]) })
	eventually(t, "structure event", func() bool { return f.inbox.has("structure", `{"list":"downloads"}`) })

	_, lg := srv.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "silent-lg"}, testjd.Link{Name: "silent-lg.bin"})
	eventually(t, "the silent linkgrabber link", func() bool {
		return slices.ContainsFunc(f.store.Snapshot(state.Linkgrabber).LgLinks, func(l state.LgLink) bool { return l.ID == fmt.Sprint(lg[0]) })
	})
}

// A check that found a change but could not load it must look again next time.
func TestStructureCounterKeepsAFailedLoadInMind(t *testing.T) {
	srv := testjd.New(t)
	cfg := fast()
	cfg.StructureCheck = 20 * time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	srv.FailNext(pathLinks, 500, "java.lang.RuntimeException")
	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "silent"}, testjd.Link{Name: "silent.bin"})
	eventually(t, "the silent link", func() bool { return hasLink(f.store, ids[0]) })
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
}

func intervalMs(c testjd.Call) int64 {
	var ms int64
	_ = json.Unmarshal(c.Args()[1], &ms)
	return ms
}

// pollGap waits for the next status poll and returns how long n more take.
func pollGap(t *testing.T, srv *testjd.Server, n int) time.Duration {
	t.Helper()
	count := func() int { return len(srv.CallsTo(pathToolbar)) }
	start := count()
	eventually(t, "the next poll", func() bool { return count() > start })
	begin, first := time.Now(), count()
	eventually(t, fmt.Sprintf("%d more polls", n), func() bool { return count() >= first+n })
	return time.Since(begin)
}

func TestAttachSetsEventIntervalAndPollRate(t *testing.T) {
	const slow = 150 * time.Millisecond
	srv := testjd.New(t)
	cfg := fast()
	cfg.PollAttached = 10 * time.Millisecond
	cfg.PollDetached = slow
	cfg.EventsAttached, cfg.EventsDetached = 1000*time.Millisecond, 5000*time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)

	lastInterval := func() int64 {
		c := srv.CallsTo(pathInterval)
		if len(c) == 0 {
			return 0
		}
		return intervalMs(c[len(c)-1])
	}
	if got := lastInterval(); got != 5000 {
		t.Errorf("interval without a browser = %d", got)
	}

	// Without a browser: the slow rate. A timer can only be late, so this bound is firm.
	const atLeast = 3 * slow * 8 / 10
	detached := pollGap(t, srv, 3)
	if detached < atLeast {
		t.Errorf("3 polls without a browser took %v, want at least %v", detached, atLeast)
	}

	// With a browser: much faster, measured against the slow rate just seen.
	f.e.Attach()
	eventually(t, "the fast interval", func() bool { return lastInterval() == 1000 })
	attached := pollGap(t, srv, 12)
	if perPoll, slowPoll := attached/12, detached/3; perPoll > slowPoll/2 {
		t.Errorf("with a browser %v per poll, without %v", perPoll, slowPoll)
	}

	// A second tab changes nothing, the polls in between give it time to show.
	n := len(srv.CallsTo(pathInterval))
	f.e.Attach()
	f.e.Detach()
	if got := pollGap(t, srv, 3); got > 3*slow {
		t.Errorf("one browser left, but 3 polls took %v", got)
	}
	if got := len(srv.CallsTo(pathInterval)); got != n {
		t.Errorf("interval set %d more times with one browser left", got-n)
	}

	// The last browser gone: slow again.
	f.e.Detach()
	eventually(t, "the slow interval", func() bool { return lastInterval() == 5000 })
	if got := pollGap(t, srv, 3); got < atLeast {
		t.Errorf("3 polls after the last browser left took %v, want at least %v", got, atLeast)
	}

	// More detaches than attaches do no harm.
	f.e.Detach()
	f.e.Attach()
	eventually(t, "attached again", func() bool { return lastInterval() == 1000 })
}

// The default poll rate with a browser, in real time.
func TestPollRateWithRealTiming(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for real polls")
	}
	srv := testjd.New(t)
	cfg := jdsync.DefaultConfig()
	cfg.Logger = fast().Logger
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)
	if got := pollGap(t, srv, 2); got < 3600*time.Millisecond || got > 6*time.Second {
		t.Errorf("2 polls took %v, want about 4 s", got)
	}
}

func TestBrowserOpenBeforeTheConnection(t *testing.T) {
	srv := testjd.New(t)
	store, hub := state.New(), sse.New()
	e := jdsync.New(jd.New(srv.URL), store, hub, fast())
	e.Attach()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = e.Run(ctx) }()
	defer func() { cancel(); <-done }()
	eventually(t, "the fast interval first", func() bool {
		c := srv.CallsTo(pathInterval)
		return len(c) > 0 && intervalMs(c[0]) == 1000
	})
}

func TestStatusFromToolbarAndConfig(t *testing.T) {
	srv := testjd.New(t)
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	c := jd.New(srv.URL)
	if err := c.SetSpeedLimit(context.Background(), true, 20480); err != nil {
		t.Fatal(err)
	}
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)

	eventually(t, "toggles", func() bool {
		tg := f.e.Status().Toggles
		return tg == state.Toggles{Limit: true, LimitBytes: 20480, Reconnect: true, Premium: true, Clipboard: true}
	})

	// During a pause the toolbar shows the pause speed as limitspeed. The limit
	// that counts is the configured one (recorded behaviour of revision 50639).
	srv.FailNext(pathToolbar, 200, `{"data":{"state":"PAUSE","pause":true,"running":true,"limit":true,"limitspeed":10240,"speed":3}}`)
	eventually(t, "pause", func() bool {
		return f.e.Status().Controller == "PAUSE" || f.inbox.has("status", `"controller":"PAUSE"`)
	})
	eventually(t, "the limit from the pause status", func() bool { return f.inbox.has("status", `"controller":"PAUSE"`, `"limitBytes":20480`) })
	if f.inbox.has("status", `"limitBytes":10240`) {
		t.Error("the pause speed was taken for the limit")
	}

	for raw, want := range map[string]string{"RUNNING": "RUNNING", "STOPPING": "STOPPING", "STOPPED_STATE": "IDLE", "IDLE": "IDLE", "PAUSE": "PAUSE"} {
		srv.SetController(raw)
		eventually(t, raw, func() bool { return f.e.Status().Controller == want })
	}

	if err := c.SetSpeedLimit(context.Background(), false, 0); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the limit off, its value kept", func() bool {
		tg := f.e.Status().Toggles
		return !tg.Limit && tg.LimitBytes == 20480
	})
}

func TestWatchdogEventRefreshesStatusAtOnce(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast()) // polls once per hour, so only the event can do it
	f.waitConn(t, state.ConnConnected)
	srv.SetController("RUNNING")
	eventually(t, "the controller", func() bool { return f.e.Status().Controller == "RUNNING" })
	eventually(t, "status event", func() bool { return f.inbox.has("status", `"controller":"RUNNING"`) })
}

func TestCaptchaAndDialogCounts(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)

	id := srv.AddCaptcha(testjd.CaptchaJob{Hoster: "h"})
	eventually(t, "captcha", func() bool { return f.e.Status().Captchas == 1 && f.inbox.has("captcha", `{"count":1}`) })
	srv.AddDialog(testjd.Dialog{Type: "x"})
	eventually(t, "dialog", func() bool { return f.e.Status().Dialogs == 1 && f.inbox.has("dialog", `{"count":1}`) })

	if err := jd.New(srv.URL).CaptchaSkip(context.Background(), id, jd.SkipSingle); err != nil {
		t.Fatal(err)
	}
	eventually(t, "captcha gone", func() bool { return f.e.Status().Captchas == 0 && f.inbox.has("captcha", `{"count":0}`) })

	// A count only changes with a real change.
	before := f.inbox.count("dialog", "count")
	srv.SetController("RUNNING")
	eventually(t, "another poll", func() bool { return f.e.Status().Controller == "RUNNING" })
	if got := f.inbox.count("dialog", "count"); got != before {
		t.Errorf("dialog events = %d, before %d", got, before)
	}
}

func TestCrawlingShowsInStatus(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	srv.FailNext("/linkgrabberv2/isCollecting", 200, `{"data":true}`)
	srv.Emit(testjd.Event{Publisher: "linkcrawler", EventID: "STARTED", EventData: map[string]any{"jobId": 1}})
	eventually(t, "crawling", func() bool { return f.inbox.has("status", `"crawling":true`) })
	srv.Emit(testjd.Event{Publisher: "linkcrawler", EventID: "FINISHED", EventData: map[string]any{"jobId": 1}})
	eventually(t, "not crawling", func() bool { return !f.e.Status().Linkgrabber.Crawling })
}

// T-SYN-09: a stacktrace instead of JSON is reported to the log, sync goes on.
func TestInternalErrorsDoNotStopSync(t *testing.T) {
	const trace = "java.lang.NullPointerException\n\tat org.appwork.remoteapi.RemoteAPI.call(RemoteAPI.java:1)"
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)
	subs := srv.Subscriptions()
	f.inbox.reset()

	srv.FailNext(pathToolbar, 500, trace)
	srv.FailNext("/captcha/list", 500, trace)
	srv.FailNext("/dialogs/list", 500, trace)
	srv.FailNext(pathListen, 500, trace)
	srv.FailNext(pathListen, 500, `{"src":"DEVICE","data":null,"type":"INTERNAL_SERVER_ERROR"}`)
	srv.FailNext(pathListen, 400, `{"src":"DEVICE","data":"x","type":"BAD_PARAMETERS"}`)
	progress(srv, link, 321)
	eventually(t, "events after failed listens", func() bool { return dlLink(t, f.store, link).BytesLoaded == 321 })

	// A failed reload is tried again.
	srv.FailNext(pathLinks, 500, trace)
	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "later"}, testjd.Link{Name: "later.bin"})
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "ADD_PACKAGE", EventData: map[string]any{"uuid": 1}})
	eventually(t, "the reload worked in the end", func() bool { return hasLink(f.store, ids[0]) })

	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
	if got := srv.Subscriptions(); !slices.Equal(got, subs) {
		t.Errorf("subscriptions = %v, before %v", got, subs)
	}
	if f.inbox.has("connection", "offline") || f.inbox.has("connection", "misconfigured") {
		t.Error("connection state changed")
	}
	if f.inbox.has("status", "NullPointer") || f.inbox.has("connection", "NullPointer") {
		t.Error("stacktrace reached the browser")
	}
}

// The first load fails with an error of JDownloader: retried, no crash, no stacktrace shown.
func TestFirstLoadFailsAndIsRetried(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	srv.FailNext(pathLinks, 500, "java.lang.RuntimeException: secret stack")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	if !hasLink(f.store, link) {
		t.Error("link not loaded")
	}
	if f.inbox.has("connection", "secret stack") {
		t.Error("stacktrace reached the browser")
	}
	// The subscription of the failed attempt is not left behind.
	eventually(t, "one subscription", func() bool { return len(srv.Subscriptions()) == 1 })
}

// With the real backoff: tries at 0 s, 1 s and 3 s.
func TestBackoffWithRealTiming(t *testing.T) {
	var mu gosync.Mutex
	var tries []time.Time
	refused := errors.New("connection refused")
	client := jd.New("http://jd.invalid", jd.WithHTTPClient(&http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		tries = append(tries, time.Now())
		mu.Unlock()
		return nil, refused
	})}))
	cfg := jdsync.DefaultConfig()
	cfg.Logger = fast().Logger
	f := run(t, client, cfg)
	eventually(t, "three tries", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(tries) >= 3
	})
	f.stop()
	mu.Lock()
	defer mu.Unlock()
	if g := tries[1].Sub(tries[0]); g < 900*time.Millisecond || g > 1500*time.Millisecond {
		t.Errorf("first wait = %v", g)
	}
	if g := tries[2].Sub(tries[1]); g < 1800*time.Millisecond || g > 2500*time.Millisecond {
		t.Errorf("second wait = %v", g)
	}
}

// The backoff ends as soon as JDownloader answers, and the next loss starts it over.
func TestSelfHealingRepeats(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	for i := range 5 {
		f.waitConn(t, state.ConnConnected)
		srv.Down()
		srv.DropSubscription()
		f.waitConn(t, state.ConnOffline)
		srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.BytesLoaded = int64(i) })
		srv.Up()
		f.waitConn(t, state.ConnConnected)
		if got := dlLink(t, f.store, link); got.BytesLoaded != int64(i) || !got.Running {
			t.Errorf("round %d: link = %+v", i, got)
		}
	}
}

func TestSetURLSwapsTheConnection(t *testing.T) {
	a, b := testjd.New(t), testjd.New(t)
	_, idsA := a.AddPackage(testjd.Downloads, testjd.Package{Name: "a"}, testjd.Link{Name: "a.bin"})
	_, idsB := b.AddPackage(testjd.Downloads, testjd.Package{Name: "b"}, testjd.Link{Name: "b.bin"})
	linkA, linkB := idsA[0], idsB[0]

	f := start(t, a, fast())
	f.waitConn(t, state.ConnConnected)
	f.inbox.reset()

	// A slash at the end and a missing scheme are fine (Review Focus 3).
	if err := f.e.SetURL(strings.TrimPrefix(b.URL, "http://") + "/"); err != nil {
		t.Fatal(err)
	}
	// When SetURL returns the old list is gone. Both fakes count ids the same way,
	// so the links are told apart by name.
	if hasLinkNamed(f.store, "a.bin") {
		t.Error("the mirror still holds the old list")
	}
	st := f.e.Status()
	if st.JD.URL != b.URL || st.Connection == state.ConnMisconfigured {
		t.Errorf("status = %+v", st)
	}
	f.waitConn(t, state.ConnConnected)
	eventually(t, "the new list", func() bool { return hasLinkNamed(f.store, "b.bin") })
	if hasLinkNamed(f.store, "a.bin") {
		t.Error("old link in the new mirror")
	}
	if f.e.Client().BaseURL() != b.URL {
		t.Errorf("client = %s", f.e.Client().BaseURL())
	}
	eventually(t, "old subscription removed", func() bool { return len(a.Subscriptions()) == 0 })
	eventually(t, "browsers told", func() bool {
		return f.inbox.has("structure", `{"list":"downloads"}`) && f.inbox.has("connection", `"connection":"syncing"`)
	})

	// Events of the old JDownloader do not reach the new mirror any more, those of the new one do.
	a.Emit(delta("LINK_UPDATE", linkA, "name", "from-the-old-one.bin"))
	b.Emit(delta("LINK_UPDATE", linkB, "bytesLoaded", 8))
	eventually(t, "new events", func() bool { return dlLink(t, f.store, linkB).BytesLoaded == 8 })
	if hasLinkNamed(f.store, "from-the-old-one.bin") {
		t.Error("an event of the old JDownloader reached the mirror")
	}
}

func TestSetURLForgetsTheGoodConnection(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := "http://" + ln.Addr().String()
	ln.Close()
	if err := f.e.SetURL(free); err != nil {
		t.Fatal(err)
	}
	st := f.waitConn(t, state.ConnMisconfigured)
	if !strings.Contains(st.ConnectionDetail, free) || !strings.Contains(st.ConnectionDetail, "DeprecatedApiEnabled") {
		t.Errorf("detail = %q", st.ConnectionDetail)
	}
}

func TestSetURLRejectsNonsense(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	for _, bad := range []string{"", "ftp://x", "http://", "http://a:99999", "http://u:p@host:1"} {
		if err := f.e.SetURL(bad); !errors.Is(err, jd.ErrInvalidURL) {
			t.Errorf("SetURL(%q) = %v", bad, err)
		}
	}
	if st := f.e.Status(); st.Connection != state.ConnConnected || st.JD.URL != srv.URL {
		t.Errorf("status = %+v", st)
	}
}

func TestSetURLBeforeRun(t *testing.T) {
	srv := testjd.New(t)
	store, hub := state.New(), sse.New()
	store.ReplaceDownloads(nil, []jd.DownloadLink{{UUID: 1}})
	e := jdsync.New(nil, store, hub, fast())
	if err := e.SetURL(srv.URL); err != nil { // no loop to wait for: must not block
		t.Fatal(err)
	}
	if len(store.Snapshot(state.Downloads).DlLinks) != 0 {
		t.Error("mirror not cleared")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = e.Run(ctx) }()
	defer func() { cancel(); <-done }()
	eventually(t, "connected", func() bool { return e.Status().Connection == state.ConnConnected })
}

func TestRunOnlyOnce(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	if err := f.e.Run(context.Background()); !errors.Is(err, jdsync.ErrRunning) {
		t.Errorf("second Run = %v", err)
	}
	f.stop()
}

func TestRunEndsWithItsContext(t *testing.T) {
	srv := testjd.New(t)
	store, hub := state.New(), sse.New()
	e := jdsync.New(jd.New(srv.URL), store, hub, fast())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	eventually(t, "connected", func() bool { return e.Status().Connection == state.ConnConnected })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run still running")
	}
	eventually(t, "the subscription removed", func() bool { return len(srv.Subscriptions()) == 0 })
}

const htmlAnswer = "<html>a hiccup of a proxy</html>"

// Fix round 1: one bad answer from a JDownloader that is there changes nothing
// the browsers can see: no misconfigured, no offline, no running:false, no new
// subscription.
func TestBadAnswersFromAHealthyJDownloaderAreSoft(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	srv.SetController("RUNNING")
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	cfg.StructureCheck = 10 * time.Millisecond
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)
	subs := srv.Subscriptions()
	f.inbox.reset()

	failing := []struct {
		path   string
		status int
		body   string
	}{
		{pathToolbar, 200, htmlAnswer},
		{pathToolbar, 501, ""},
		{"/config/get", 200, htmlAnswer},
		{"/linkgrabberv2/isCollecting", 501, ""},
		{"/linkcrawler/isCrawling", 200, htmlAnswer},
		{"/captcha/list", 200, `{"data":"not a list"}`}, // a decode mismatch
		{"/dialogs/list", 200, htmlAnswer},
		{pathSubStat, 200, htmlAnswer},
		{"/downloadsV2/getStructureChangeCounter", 200, htmlAnswer},
		{"/linkgrabberv2/getChildrenChanged", 501, ""},
		{pathListen, 200, htmlAnswer},
		{pathListen, 501, ""},
		{pathLinks, 200, htmlAnswer},
	}
	for _, c := range failing {
		srv.FailNext(c.path, c.status, c.body)
	}
	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "after"}, testjd.Link{Name: "after.bin"})
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "ADD_PACKAGE", EventData: map[string]any{"uuid": 1}})

	for _, c := range failing {
		eventually(t, "the bad answer on "+c.path, func() bool {
			return slices.ContainsFunc(srv.CallsTo(c.path), func(call testjd.Call) bool { return call.Status == c.status })
		})
	}
	eventually(t, "the reload worked in the end", func() bool { return hasLink(f.store, ids[0]) })
	progress(srv, link, 222)
	eventually(t, "events go on", func() bool { return dlLink(t, f.store, link).BytesLoaded == 222 })

	if st := f.e.Status(); st.Connection != state.ConnConnected || st.ConnectionDetail != "" {
		t.Errorf("status = %+v", st)
	}
	for _, conn := range []string{"misconfigured", "offline", "syncing"} {
		if f.inbox.has("connection", conn) {
			t.Errorf("the browsers saw %s", conn)
		}
	}
	if f.inbox.has("patch", `"running":false`) {
		t.Error("running:false flicker")
	}
	if !dlLink(t, f.store, link).Running {
		t.Error("the link stopped")
	}
	if got := srv.Subscriptions(); !slices.Equal(got, subs) {
		t.Errorf("subscriptions = %v, before %v", got, subs)
	}
	if n := len(srv.CallsTo("/events/subscribe")); n != 1 {
		t.Errorf("subscribed %d times", n)
	}
}

// A bad answer between the ping and connected is offline with its own hint as
// long as the ping still answers, never "another service".
func TestBadAnswerBeforeConnectedIsNotAnotherService(t *testing.T) {
	for name, c := range map[string]struct {
		path, body string
		status     int
	}{
		"subscribe, html":     {"/events/subscribe", htmlAnswer, 200},
		"subscribe, no id":    {"/events/subscribe", `{"data":{"subscriptionid":-1}}`, 200},
		"subscribe, void":     {"/events/subscribe", `{"data":""}`, 200},
		"packages, html":      {pathPackages, htmlAnswer, 200},
		"links, unknown path": {pathLinks, "", 501},
		"linkgrabber, html":   {"/linkgrabberv2/queryLinks", htmlAnswer, 200},
		"counter, stacktrace": {"/downloadsV2/getStructureChangeCounter", "java.lang.NullPointerException", 500},
	} {
		t.Run(name, func(t *testing.T) {
			srv := testjd.New(t)
			srv.FailNext(c.path, c.status, c.body)
			cfg := fast()
			cfg.BackoffMin, cfg.BackoffMax = 200*time.Millisecond, 200*time.Millisecond
			f := start(t, srv, cfg)
			st := f.waitConn(t, state.ConnOffline)
			if want := state.DetailFailing(srv.URL); st.ConnectionDetail != want {
				t.Errorf("detail = %q, want %q", st.ConnectionDetail, want)
			}
			f.waitConn(t, state.ConnConnected)
			if f.inbox.has("connection", "misconfigured") {
				t.Error("the browsers saw misconfigured")
			}
			f.stop()
		})
	}
}

// If the ping fails too when asked again, it really is something else now.
func TestPingAgainAfterABadAnswer(t *testing.T) {
	srv := testjd.New(t)
	srv.FailNext(pathPackages, 200, htmlAnswer)
	srv.FailNext("/device/ping", 200, `{"data":true}`) // the first ping is good
	srv.FailNext("/device/ping", 200, htmlAnswer)      // the one asked after the bad answer is not
	cfg := fast()
	cfg.BackoffMin, cfg.BackoffMax = 200*time.Millisecond, 200*time.Millisecond
	f := start(t, srv, cfg)
	st := f.waitConn(t, state.ConnMisconfigured)
	if st.ConnectionDetail != state.DetailOtherService {
		t.Errorf("detail = %q", st.ConnectionDetail)
	}
	// It is asked at once, not only by the next attempt after a wait in "offline".
	if f.inbox.has("connection", `"connection":"offline"`) {
		t.Error("offline came first")
	}
	f.waitConn(t, state.ConnConnected)
}

// A load that fails midway has already put the running links of the first list
// back into the mirror. They must stop again.
func TestLoadFailingMidwayLeavesNothingRunning(t *testing.T) {
	srv := testjd.New(t)
	pkg, link := addRunning(srv, "dl")
	srv.SetController("RUNNING")
	cfg := fast()
	cfg.BackoffMin, cfg.BackoffMax = 300*time.Millisecond, 300*time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)

	srv.FailNext("/linkgrabberv2/queryLinks", 500, "java.lang.NullPointerException")
	srv.DropSubscription()
	st := f.waitConn(t, state.ConnOffline)
	snap := f.store.Snapshot(state.Downloads)

	if st.ConnectionDetail != state.DetailFailing(srv.URL) || st.Speed != 0 || st.Downloads.Running != 0 {
		t.Errorf("status = %+v", st)
	}
	if !snap.Stale {
		t.Error("not marked stale")
	}
	for _, l := range snap.DlLinks {
		if l.Running || l.Speed != 0 || l.ETA != state.ETAUnknown || l.State == state.LinkRunning {
			t.Errorf("link after the failed load = %+v", l)
		}
	}
	for _, p := range snap.DlPackages {
		if p.Running || p.Speed != 0 || p.ETA != state.ETAUnknown {
			t.Errorf("package %d after the failed load = %+v", pkg, p)
		}
	}
	f.waitConn(t, state.ConnConnected)
	if got := dlLink(t, f.store, link); !got.Running || got.Speed != 50 {
		t.Errorf("link after the good load = %+v", got)
	}
}

// Fix round 1: a failed long-poll may have cost events, so it reloads.
func TestFailedListenCatchesUp(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	_, lg := srv.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "lg"}, testjd.Link{Name: "lg.bin"})
	srv.SetController("RUNNING")
	f := start(t, srv, fast()) // polls and checks once per hour: only the failure can do it
	f.waitConn(t, state.ConnConnected)
	subs := srv.Subscriptions()

	// The event that said so is lost with the failed answer.
	srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.Running, l.Speed, l.ETA = false, 0, 0 })
	srv.UpdateLink(testjd.Linkgrabber, lg[0], func(l *testjd.Link) { l.Name = "renamed.bin" })
	srv.FailNext(pathListen, 500, "java.lang.NullPointerException")
	wakeListen(srv) // one is waiting already, the next one fails

	eventually(t, "the link stopped under connected", func() bool {
		l := dlLink(t, f.store, link)
		return !l.Running && l.Speed == 0 && l.State != state.LinkRunning
	})
	eventually(t, "the other list too", func() bool {
		l := f.store.Snapshot(state.Linkgrabber).LgLinks
		return len(l) == 1 && l[0].Name == "renamed.bin"
	})
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
	if got := srv.Subscriptions(); !slices.Equal(got, subs) {
		t.Errorf("subscriptions = %v, before %v", got, subs)
	}
}

func wakeListen(srv *testjd.Server) {
	srv.Emit(testjd.Event{Publisher: "downloads", EventID: "BRAND_NEW"})
}

func TestFiveFailedListensGiveUpTheSubscription(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	before := srv.Subscriptions()
	f.inbox.reset()

	for range 5 {
		srv.FailNext(pathListen, 500, "java.lang.NullPointerException")
	}
	wakeListen(srv)
	eventually(t, "a new subscription", func() bool {
		s := srv.Subscriptions()
		return len(srv.CallsTo("/events/subscribe")) == 2 && len(s) >= 1 && !slices.Equal(s, before)
	})
	f.waitConn(t, state.ConnConnected)
	eventually(t, "resync announced", func() bool { return f.inbox.has("connection", `"connection":"syncing"`) })
	progress(srv, link, 444)
	eventually(t, "events on the new subscription", func() bool { return dlLink(t, f.store, link).BytesLoaded == 444 })
}

// Failures only count while they are consecutive.
func TestGoodListenResetsTheFailureCount(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)

	for round := range 3 {
		for range 4 {
			srv.FailNext(pathListen, 500, "java.lang.NullPointerException")
		}
		wakeListen(srv) // the waiting listen answers well, then four fail
		want := 1000 + round
		progress(srv, link, int64(want))
		eventually(t, "the event behind the failures", func() bool { return dlLink(t, f.store, link).BytesLoaded == int64(want) })
	}
	if n := len(srv.CallsTo("/events/subscribe")); n != 1 {
		t.Errorf("subscribed %d times", n)
	}
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
}

// If what answers the long-poll stops being JDownloader, the ping finds out.
func TestAnotherServiceTakesOverDuringASession(t *testing.T) {
	srv := testjd.New(t)
	cfg := fast()
	cfg.BackoffMin, cfg.BackoffMax = 300*time.Millisecond, 300*time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	for range 5 {
		srv.FailNext(pathListen, 200, htmlAnswer)
	}
	srv.FailNext("/device/ping", 200, htmlAnswer)
	wakeListen(srv)
	st := f.waitConn(t, state.ConnMisconfigured)
	if st.ConnectionDetail != state.DetailOtherService {
		t.Errorf("detail = %q", st.ConnectionDetail)
	}
	f.waitConn(t, state.ConnConnected)
}

// The toolbar says nothing runs, the mirror says something does: an event got lost.
func TestStoppedControllerWithRunningLinkReloads(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	srv.SetController("STOPPED_STATE")
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	if !dlLink(t, f.store, link).Running {
		t.Fatal("the link should run in the mirror")
	}
	srv.UpdateLink(testjd.Downloads, link, func(l *testjd.Link) { l.Running, l.Speed, l.ETA = false, 0, 0 })
	f.e.Attach()
	eventually(t, "the link stopped", func() bool { return !dlLink(t, f.store, link).Running })
	eventually(t, "the totals", func() bool { return f.e.Status().Downloads.Running == 0 })
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
}

// Should JDownloader really report that combination, it costs one reload per
// structure check, not one per poll.
func TestStoppedControllerWithRunningLinkDoesNotReloadPerPoll(t *testing.T) {
	srv := testjd.New(t)
	addRunning(srv, "dl")
	srv.SetController("STOPPED_STATE")
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)
	before := len(srv.CallsTo(pathLinks))
	pollGap(t, srv, 20)
	if got := len(srv.CallsTo(pathLinks)) - before; got > 1 {
		t.Errorf("%d reloads in 20 polls", got)
	}
}

func TestRunningControllerWithRunningLinkDoesNotReload(t *testing.T) {
	srv := testjd.New(t)
	addRunning(srv, "dl")
	srv.SetController("RUNNING")
	cfg := fast()
	cfg.PollAttached = 5 * time.Millisecond
	f := start(t, srv, cfg)
	f.e.Attach()
	f.waitConn(t, state.ConnConnected)
	before := len(srv.CallsTo(pathLinks))
	pollGap(t, srv, 20)
	if got := len(srv.CallsTo(pathLinks)) - before; got != 0 {
		t.Errorf("%d reloads", got)
	}
}

func TestKnownRevisionAndDockerSurviveASoftFailure(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast())
	st := f.waitConn(t, state.ConnConnected)
	if st.JD.Revision != 50639 || !st.JD.Docker {
		t.Fatalf("jd = %+v", st.JD)
	}
	f.inbox.reset()
	srv.FailNext("/jd/getCoreRevision", 500, "java.lang.NullPointerException")
	srv.FailNext("/system/getSystemInfos", 200, htmlAnswer)
	srv.DropSubscription()
	eventually(t, "both failed", func() bool {
		return slices.ContainsFunc(srv.CallsTo("/jd/getCoreRevision"), func(c testjd.Call) bool { return c.Status == 500 }) &&
			len(srv.CallsTo("/system/getSystemInfos")) >= 2
	})
	eventually(t, "connected on a new subscription", func() bool {
		return len(srv.CallsTo("/events/subscribe")) == 2 && f.e.Status().Connection == state.ConnConnected
	})
	if st := f.e.Status(); st.JD.Revision != 50639 || !st.JD.Docker {
		t.Errorf("jd = %+v", st.JD)
	}
	if f.inbox.has("status", `"revision":0`) || f.inbox.has("status", `"docker":false`) {
		t.Error("the browsers saw the known values vanish")
	}
}

// An empty answer that comes at once must not become a hot loop.
func TestEmptyListenIsPaced(t *testing.T) {
	srv := testjd.New(t)
	srv.SetPollTimeout(0)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)

	count := func() int { return len(srv.CallsTo(pathListen)) }
	first := count()
	eventually(t, "polls go on", func() bool { return count() > first })
	begin, base := time.Now(), count()
	eventually(t, "4 more polls", func() bool { return count() >= base+4 })
	if got := time.Since(begin); got < 320*time.Millisecond {
		t.Errorf("4 empty polls took only %v", got)
	}
	srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", 7))
	eventually(t, "events still arrive", func() bool { return dlLink(t, f.store, link).BytesLoaded == 7 })
}

// A browser that stopped reading must not hold the engine up.
func TestSlowBrowserDoesNotBlockTheEngine(t *testing.T) {
	srv := testjd.New(t)
	_, link := addRunning(srv, "dl")
	f := start(t, srv, fast())
	f.waitConn(t, state.ConnConnected)
	slow, cancel := f.hub.Subscribe()
	defer cancel()

	for i := range 10 * sse.BufferSize {
		srv.Emit(delta("LINK_UPDATE", link, "bytesLoaded", i+1000))
		want := int64(i + 1000)
		eventually(t, "progress", func() bool { return dlLink(t, f.store, link).BytesLoaded == want })
		if i%5 == 0 {
			eventually(t, "a patch", func() bool { return f.inbox.has("patch", fmt.Sprintf(`"bytesLoaded":%d`, want)) })
		}
	}
	// The slow one was dropped: its channel ends after the buffered messages.
	for range sse.BufferSize + 1 {
		select {
		case _, ok := <-slow:
			if !ok {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("slow subscriber still connected and silent")
		}
	}
	t.Error("slow subscriber was not disconnected")
}

// SetURL and restarts leave nothing running behind.
func TestNoGoroutineLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	func() {
		a, b := testjd.Start(), testjd.Start()
		defer a.Close()
		defer b.Close()
		store, hub := state.New(), sse.New()
		e := jdsync.New(jd.New(a.URL), store, hub, fast())
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = e.Run(ctx) }()

		for i := range 20 {
			srv := a
			if i%2 == 0 {
				srv = b
			}
			if err := e.SetURL(srv.URL); err != nil {
				t.Fatal(err)
			}
			eventually(t, "connected", func() bool {
				st := e.Status()
				return st.Connection == state.ConnConnected && st.JD.URL == srv.URL
			})
		}
		a.Down()
		b.Down()
		eventually(t, "offline", func() bool { return e.Status().Connection == state.ConnOffline })
		a.Up()
		b.Up()
		eventually(t, "connected again", func() bool { return e.Status().Connection == state.ConnConnected })
		cancel()
		<-done
	}()

	var after int
	deadline := time.Now().Add(10 * time.Second)
	for {
		after = runtime.NumGoroutine()
		if after <= before || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after > before {
		buf := make([]byte, 1<<16)
		t.Errorf("goroutines %d -> %d\n%s", before, after, buf[:runtime.Stack(buf, true)])
	}
}

// Concept 10.8: below 150 MB with 10,000 links, here with a full load, every
// link in progress and the fake in the same process.
func TestEngineWith10000Links(t *testing.T) {
	if testing.Short() {
		t.Skip("big list")
	}
	srv := testjd.New(t)
	for p := range 500 {
		links := make([]testjd.Link, 20)
		for i := range links {
			links[i] = testjd.Link{
				Name: fmt.Sprintf("a-rather-usual-file-name-%03d-%03d.part01.rar", p, i), URL: fmt.Sprintf("https://files.example/d/%03d/%03d", p, i),
				Host: "files.example", BytesTotal: 1 << 30, BytesLoaded: 7, Running: true, Speed: 1000, ETA: 99, Status: "Download",
			}
		}
		srv.AddPackage(testjd.Downloads, testjd.Package{Name: fmt.Sprintf("package %d", p), SaveTo: "/output/x"}, links...)
	}
	cfg := fast()
	f := start(t, srv, cfg)
	f.waitConn(t, state.ConnConnected)
	if n := len(f.store.Snapshot(state.Downloads).DlLinks); n != 10000 {
		t.Fatalf("links = %d", n)
	}
	for _, l := range f.store.Snapshot(state.Downloads).DlLinks[:2000] {
		var id int64
		_, _ = fmt.Sscan(l.ID, &id)
		srv.Emit(delta("LINK_UPDATE", id, "bytesLoaded", 8))
	}
	eventually(t, "progress applied", func() bool { return f.e.Status().Downloads.BytesLoaded >= 10000*7+2000 })
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("with engine, mirror and fake: heap in use %.1f MB, from the OS in total %.1f MB", float64(m.HeapAlloc)/(1<<20), float64(m.Sys)/(1<<20))
	if m.Sys > 150<<20 {
		t.Errorf("the process holds %d MB", m.Sys>>20)
	}
}

// An action on an entry JDownloader removed on its own answers with success
// and sends no event. Resync is how the API layer makes the mirror converge.
func TestResyncReloadsAListWithoutAnEvent(t *testing.T) {
	srv := testjd.New(t)
	f := start(t, srv, fast()) // StructureCheck is an hour: only Resync can find the change
	f.waitConn(t, state.ConnConnected)

	_, ids := srv.AddPackage(testjd.Downloads, testjd.Package{Name: "silent"}, testjd.Link{Name: "silent.bin"})
	time.Sleep(100 * time.Millisecond)
	if hasLink(f.store, ids[0]) {
		t.Fatal("the silent link arrived without Resync, the test proves nothing")
	}
	f.e.Resync(state.Downloads)
	eventually(t, "the silent link after Resync", func() bool { return hasLink(f.store, ids[0]) })
	eventually(t, "structure event", func() bool { return f.inbox.has("structure", `{"list":"downloads"}`) })

	_, lg := srv.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "silent-lg"}, testjd.Link{Name: "silent-lg.bin"})
	f.e.Resync(state.Linkgrabber)
	eventually(t, "the silent linkgrabber link after Resync", func() bool {
		return slices.ContainsFunc(f.store.Snapshot(state.Linkgrabber).LgLinks, func(l state.LgLink) bool { return l.ID == fmt.Sprint(lg[0]) })
	})
}

func TestResyncWithoutSessionNeitherBlocksNorPanics(t *testing.T) {
	e := jdsync.New(nil, state.New(), sse.New(), fast())
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			e.Resync(state.Downloads)
			e.Resync(state.Linkgrabber)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Resync blocks while no session runs")
	}
}
