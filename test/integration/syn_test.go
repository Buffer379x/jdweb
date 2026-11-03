//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

// T-SYN-01: start with a reachable JDownloader.
func TestSYN01_StartConnected(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	waitFor(t, "connected", 30e9, func() bool { return a.status(t).Connection == state.ConnConnected })
	st := a.status(t)
	if st.JD.Revision < 50000 || !st.JD.Docker {
		t.Errorf("jd info = %+v", st.JD)
	}
	if st.Controller != "IDLE" {
		t.Errorf("controller = %s", st.Controller)
	}
	_ = a.downloads(t)
	_ = a.grabber(t)
}

// outageCheck is what T-SYN-04 and T-SYN-05 share: with a download running and
// the browser connected, JDownloader goes away; JDWeb shows offline, nothing
// stays "running", and within 30 s of JDownloader being back the state is right
// and events work again.
func (a *app) outageCheck(t *testing.T, what string, cause func()) {
	t.Helper()
	cleanJD(t)
	s := a.openStream(t)
	a.limit(t, 50*1024)
	a.stage(t, prefix+"syn", directory("syn"), fx("/slow/it-syn.bin"))
	a.ctl(t, "start")
	eventually(t, "the link runs", 20*time.Second, func() bool { return a.downloads(t).running() == 1 })
	m := s.mark()
	t0 := time.Now()
	cause()
	offline := s.wait(t, m, 60*time.Second, "connection", `"connection":"offline"`)
	t.Logf("%s: offline event after %v", what, offline.At.Sub(t0).Round(100*time.Millisecond))
	if n := a.downloads(t).running(); n != 0 || a.status(t).Controller != "IDLE" {
		t.Errorf("%s: while offline %d links still run, controller %s", what, n, a.status(t).Controller)
	}
	// The state while offline is the last known one and says so.
	if c := a.status(t).Connection; c == state.ConnConnected {
		t.Errorf("%s: connection %s", what, c)
	}
	back := s.wait(t, m, 180*time.Second, "connection", `"connection":"connected"`)
	t.Logf("%s: connected again %v after the cause (E: 30 s after JDownloader answers)", what, back.At.Sub(t0).Round(100*time.Millisecond))
	// Whatever JDownloader kept is shown the way JDownloader has it.
	if n := a.downloads(t).running(); n != 0 {
		t.Errorf("%s: %d links run after the restart", what, n)
	}
	// Events are live again: a change made inside JDownloader arrives without a reload.
	m = s.mark()
	if err := direct().ControllerStart(bg()); err != nil {
		t.Fatal(err)
	}
	t1 := time.Now()
	waitFor(t, "controller RUNNING as event", 10*time.Second, func() bool {
		_, ok := s.find(m, "status", `"controller":"RUNNING"`)
		if !ok && time.Since(t1) > 5*time.Second {
			t.Logf("status after 5 s: %+v", a.status(t))
			t1 = t1.Add(time.Hour)
		}
		return ok
	})
	t.Logf("%s: start made in JDownloader reached the stream after %v", what, time.Since(t1).Round(10*time.Millisecond))
	_ = direct().ControllerStop(bg())
	// And one made in JDWeb works.
	a.add(t, prefix+"syn2", directory("syn2"), fx("/it-syn-after.bin"))
}

// T-SYN-04: restart of JDownloader through its API (and through JDWeb's system action: T-SYS-01).
func TestSYN04_RestartByAPI(t *testing.T) {
	a := shared(t)
	if !dockerOK() {
		t.Log("no docker: the end of the restart is awaited through the API only")
	}
	a.outageCheck(t, "T-SYN-04 restart through POST /system/restart", func() {
		a.must(t, "POST", "/api/v1/system/restart", nil, nil, 202)
	})
}

// T-SYN-05: docker restart, then the container removed and created again
// (scripts/teststack.sh up), as an update of the container does it.
func TestSYN05_DockerRestartAndRecreate(t *testing.T) {
	if !dockerOK() {
		t.Skip("docker is not available: T-SYN-05 needs the test container")
	}
	if os.Getenv("JDWEB_IT_NO_DOCKER") != "" {
		t.Skip("JDWEB_IT_NO_DOCKER is set")
	}
	a := shared(t)
	a.outageCheck(t, "T-SYN-05 docker restart", func() {
		if out, err := docker("restart", jdContainer()); err != nil {
			t.Fatalf("docker restart: %v %s", err, out)
		}
	})
	a.outageCheck(t, "T-SYN-05 container removed and created again", func() {
		if out, err := docker("rm", "-f", jdContainer()); err != nil {
			t.Fatalf("docker rm: %v %s", err, out)
		}
		go func() {
			cmd := exec.Command("bash", filepath.Join(root, "scripts", "teststack.sh"), "up")
			cmd.Dir = root
			_ = cmd.Run()
		}()
	})
}

// T-SYN-07: events of the linkcollector, linkcrawler, dialogs (and captchas,
// which cannot be triggered here) are recorded from a subscription of their
// own and the mirror of JDWeb must agree with JDownloader afterwards. What
// JDWeb does not know is reported in its log, once per event id.
func TestSYN07_RecordedEvents(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	sub, err := c.Subscribe(bg(), jd.Publishers())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Unsubscribe(bg(), sub.ID) })
	var mu sync.Mutex
	seen := map[string]string{}
	count := map[string]int{}
	ctx, cancel := context.WithCancel(bg())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			evs, err := c.Listen(ctx, sub.ID)
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			mu.Lock()
			for _, e := range evs {
				k := e.Publisher + "/" + e.EventID
				count[k]++
				if _, ok := seen[k]; !ok {
					d := string(e.EventData)
					if len(d) > 160 {
						d = d[:160]
					}
					seen[k] = d
				}
			}
			mu.Unlock()
		}
	}()
	t.Cleanup(func() { cancel(); <-done })

	urls, _ := numberedURLs("syn07", 12)
	g := a.add(t, prefix+"syn07", directory("syn07"), urls[:3]...)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": urls[3:], "packageName": prefix + "syn07b", "autostart": false}, nil, 200)
	a.waitGrabberNames(t, "it-lg-00012.bin")
	a.action(t, "linkgrabber", "rename-package", map[string]any{"packageId": g.Packages[0].ID, "name": prefix + "syn07-renamed"})
	a.action(t, "linkgrabber", "disable", sel(lgLinkIDs(g)[:1], nil))
	a.action(t, "linkgrabber", "priority", with(sel(lgLinkIDs(g)[1:2], nil), "priority", "HIGH"))
	a.action(t, "linkgrabber", "to-downloads", map[string]any{"start": false, "packageIds": lgPackageIDs(a.grabber(t))})
	eventually(t, "moved", 30*time.Second, func() bool { return len(a.downloads(t).Links) == 12 })
	// A dialog.
	go a.call("POST", "/api/v1/toggles/premium", map[string]any{"enabled": false})
	eventually(t, "dialog", 10*time.Second, func() bool { return a.status(t).Dialogs == 1 })
	var dl []dialogDTO
	a.must(t, "GET", "/api/v1/dialogs", nil, &dl, 200)
	a.must(t, "POST", "/api/v1/dialogs/"+dl[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "OK"}}, nil, 204)
	t.Cleanup(func() { _, _ = direct().TogglePremium(bg()) })
	time.Sleep(3 * time.Second)

	mu.Lock()
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		t.Logf("T-SYN-07 %-50s x%-4d %s", k, count[k], seen[k])
	}
	mu.Unlock()
	// The mirror agrees with JDownloader.
	truth, _ := c.GrabberLinks(bg(), jd.AllGrabberLinkFields())
	if n := len(a.grabber(t).Links); n != len(truth) {
		t.Errorf("linkgrabber: mirror %d, JDownloader %d", n, len(truth))
	}
	dtruth, _ := c.DownloadLinks(bg(), jd.AllDownloadLinkFields())
	if n := len(a.downloads(t).Links); n != len(dtruth) {
		t.Errorf("downloads: mirror %d, JDownloader %d", n, len(dtruth))
	}
	var unknown []string
	for _, l := range strings.Split(a.logs.String(), "\n") {
		if strings.Contains(l, "ignoring an unknown JDownloader event") {
			var m map[string]any
			_ = json.Unmarshal([]byte(l), &m)
			unknown = append(unknown, fmt.Sprint(m["publisher"], "/", m["event"]))
		}
	}
	t.Logf("T-SYN-07 events JDWeb logged as unknown: %v", unknown)
	for _, u := range unknown {
		if strings.HasPrefix(u, "linkcollector/") || strings.HasPrefix(u, "linkcrawler/") || strings.HasPrefix(u, "dialogs/") || strings.HasPrefix(u, "captchas/") || strings.Contains(u, "reset") {
			t.Errorf("%s is not understood", u)
		}
	}
}

// Captchas cannot be triggered offline (no hoster that asks is reachable
// from the test network); what can be checked is the empty case and the
// answers for ids JDownloader does not know.
func TestCAP_NoCaptchaToSolve(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	var list []map[string]any
	a.must(t, "GET", "/api/v1/captchas", nil, &list, 200)
	if len(list) != 0 || a.status(t).Captchas != 0 {
		t.Errorf("captchas = %v", list)
	}
	for _, c := range []struct {
		m, p string
		body any
	}{
		{"GET", "/api/v1/captchas/123456", nil},
		{"POST", "/api/v1/captchas/123456/solve", map[string]any{"result": "abc"}},
		{"POST", "/api/v1/captchas/123456/skip", map[string]any{"type": "SINGLE"}},
		{"POST", "/api/v1/captchas/123456/skip", map[string]any{"type": "REFRESH"}},
		{"POST", "/api/v1/captchas/123456/skip", map[string]any{"type": "BLOCK_HOSTER"}},
	} {
		st, b := a.call(c.m, c.p, c.body)
		t.Logf("T-CAP unknown id %s %s: %d %s", c.m, c.p, st, clip(b))
		if st >= 500 {
			t.Errorf("%s %s = %d", c.m, c.p, st)
		}
	}
}

// T-SYN-02: JDownloader not reachable at start: offline, no crash, the process stays up.
func TestSYN02_StartWithoutJDownloader(t *testing.T) {
	addr, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	a := startApp(t, appOpts{jdURL: "http://" + addr}) // nobody listens there
	waitFor(t, "offline", 20*time.Second, func() bool { return a.status(t).Connection == state.ConnOffline })
	time.Sleep(4 * time.Second)
	st := a.status(t)
	if st.Connection != state.ConnOffline {
		t.Errorf("status = %+v", st)
	}
	select {
	case <-a.done:
		t.Fatal("jdweb ended")
	default:
	}
	t.Logf("T-SYN-02 offline, detail %q", st.ConnectionDetail)
}

// T-SYN-03 (the part testable here): another service at the address is told apart from an offline JDownloader.
func TestSYN03_OtherServiceAtTheAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	a := startApp(t, appOpts{jdURL: srv.URL})
	waitFor(t, "a state other than syncing", 20*time.Second, func() bool {
		c := a.status(t).Connection
		return c == state.ConnMisconfigured || c == state.ConnOffline
	})
	st := a.status(t)
	t.Logf("T-SYN-03 other service: connection=%s detail=%q", st.Connection, st.ConnectionDetail)
	if st.Connection != state.ConnMisconfigured {
		t.Errorf("connection = %s, want misconfigured", st.Connection)
	}
}

// T-SYN-12: two streams (two tabs) get the same changes.
func TestSYN12_TwoTabs(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	s1, s2 := a.openStream(t), a.openStream(t)
	m1, m2 := s1.mark(), s2.mark()
	a.limit(t, 77*1024)
	s1.wait(t, m1, 5*time.Second, "status", `"limitBytes":78848`)
	s2.wait(t, m2, 5*time.Second, "status", `"limitBytes":78848`)
	a.add(t, prefix+"syn12", directory("syn12"), fx("/it-syn12.bin"))
	s1.wait(t, m1, 10*time.Second, "structure", "linkgrabber")
	s2.wait(t, m2, 10*time.Second, "structure", "linkgrabber")
}
