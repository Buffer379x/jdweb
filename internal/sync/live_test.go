package sync_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
	jdsync "github.com/Buffer379x/jdweb/internal/sync"
	"github.com/Buffer379x/jdweb/internal/testguard"
)

// This test talks to a real JDownloader and runs only when JDWEB_TEST_JD_URL is
// set to the test stack (scripts/teststack.sh url). It restarts that
// JDownloader. It refuses to run on an instance with packages of anyone else.

const livePrefix = "jdweb-live-"

func requireOnlyTestData(t *testing.T, c *jd.Client) {
	t.Helper()
	if err := testguard.Require(context.Background(), c, os.Getenv("JDWEB_TEST_JD_URL"), livePrefix); err != nil {
		t.Fatal(err)
	}
}

func waitLive(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %v: %s", d, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitUp waits until JDownloader answers again after a restart, with a client
// that has nothing to do with the engine.
func waitUp(t *testing.T, c *jd.Client, d time.Duration) {
	t.Helper()
	waitLive(t, d, "JDownloader answers", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ok, err := c.Ping(ctx)
		return err == nil && ok
	})
}

// answerDialogs cancels what JDownloader asks, unanswered dialogs block its controller.
func answerDialogs(ctx context.Context, c *jd.Client) {
	if ids, err := c.DialogIDs(ctx); err == nil {
		slices.Reverse(ids)
		for _, id := range ids {
			_ = c.DialogAnswer(ctx, id, map[string]any{"closereason": "CANCEL"})
		}
	}
}

func TestLiveSelfHealing(t *testing.T) {
	raw := os.Getenv("JDWEB_TEST_JD_URL")
	if raw == "" {
		t.Skip("JDWEB_TEST_JD_URL is not set")
	}
	url, err := jd.NormalizeURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := jd.New(url)
	ctx := context.Background()
	requireOnlyTestData(t, c)

	// A new name each run: an old .part file would make JDownloader ask a dialog.
	stamp := time.Now().Unix()
	pkgName := fmt.Sprintf("%ssync-%d", livePrefix, stamp)
	file := fmt.Sprintf("live-sync-%d.bin", stamp)
	const dir = "/output/jdweb-live-sync"

	wasOn, wasLimit, err := c.SpeedLimit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		waitUp(t, c, 2*time.Minute)
		_ = c.ControllerStop(bg)
		answerDialogs(bg, c)
		if pk, err := c.DownloadPackages(bg, jd.DownloadPackageQuery{}); err == nil {
			for _, p := range pk {
				if p.Name == pkgName {
					_ = c.Downloads().Cleanup(bg, nil, []int64{p.UUID}, jd.CleanupDeleteAll, jd.CleanupRemoveLinksAndDeleteFiles, jd.SelectionSelected)
					_ = c.Downloads().Remove(bg, nil, []int64{p.UUID})
				}
			}
		}
		if pk, err := c.GrabberPackages(bg, jd.GrabberPackageQuery{}); err == nil {
			for _, p := range pk {
				if p.Name == pkgName {
					_ = c.Linkgrabber().Remove(bg, nil, []int64{p.UUID})
				}
			}
		}
		_ = c.SetSpeedLimit(bg, wasOn, wasLimit)
		answerDialogs(bg, c)
	})
	if err := c.SetSpeedLimit(ctx, true, 20480); err != nil {
		t.Fatal(err)
	}

	f := run(t, jd.New(url), jdsync.Config{Logger: fast().Logger})
	f.e.Attach()
	st := f.waitConn(t, state.ConnConnected)
	if st.JD.Revision < 50000 || st.JD.URL != url {
		t.Errorf("jd = %+v", st.JD)
	}
	waitLive(t, 10*time.Second, "the configured limit", func() bool {
		tg := f.e.Status().Toggles
		return tg.Limit && tg.LimitBytes == 20480
	})

	// A link arrives in the linkgrabber, seen only through the mirror.
	no := false
	if _, err := c.AddLinks(ctx, jd.AddLinksRequest{
		Links: []string{"http://jdweb-test-files:8000/slow/" + file}, PackageName: pkgName,
		DestinationFolder: dir, Autostart: &no, AutoExtract: &no, DeepDecrypt: &no,
	}); err != nil {
		t.Fatal(err)
	}
	var grabbed state.LgPackage
	var link state.LgLink
	waitLive(t, 30*time.Second, "the link online in the mirror", func() bool {
		snap := f.store.Snapshot(state.Linkgrabber)
		for _, p := range snap.LgPackages {
			if p.Name == pkgName {
				grabbed = p
			}
		}
		for _, l := range snap.LgLinks {
			if l.Name == file && l.Availability == string(jd.AvailabilityOnline) {
				link = l
				return grabbed.ID != ""
			}
		}
		return false
	})
	if !f.inbox.has("structure", `{"list":"linkgrabber"}`) {
		t.Error("no structure event for the linkgrabber")
	}

	// Into the download list. The link keeps its id, the package gets a new one.
	var pkgID int64
	if _, err := fmt.Sscan(grabbed.ID, &pkgID); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveToDownloads(ctx, nil, []int64{pkgID}); err != nil {
		t.Fatal(err)
	}
	waitLive(t, 15*time.Second, "the link in the download list of the mirror", func() bool {
		return slices.ContainsFunc(f.store.Snapshot(state.Downloads).DlLinks, func(l state.DlLink) bool { return l.ID == link.ID })
	})
	waitLive(t, 15*time.Second, "the link gone from the linkgrabber of the mirror", func() bool {
		return !slices.ContainsFunc(f.store.Snapshot(state.Linkgrabber).LgLinks, func(l state.LgLink) bool { return l.ID == link.ID })
	})

	// Progress arrives as patches.
	f.inbox.reset()
	if err := c.ControllerStart(ctx); err != nil {
		t.Fatal(err)
	}
	find := func() state.DlLink {
		for _, l := range f.store.Snapshot(state.Downloads).DlLinks {
			if l.ID == link.ID {
				return l
			}
		}
		return state.DlLink{}
	}
	waitLive(t, 30*time.Second, "the link running", func() bool {
		l := find()
		return l.Running && l.State == state.LinkRunning && l.BytesLoaded > 0
	})
	first := find().BytesLoaded
	waitLive(t, 30*time.Second, "progress in the mirror", func() bool { return find().BytesLoaded > first })
	waitLive(t, 10*time.Second, "progress patches", func() bool {
		return f.inbox.has("patch", `"list":"downloads"`, link.ID, `"bytesLoaded"`)
	})
	waitLive(t, 10*time.Second, "the status", func() bool {
		st := f.e.Status()
		return st.Controller == "RUNNING" && st.Speed > 0 && st.Downloads.Running >= 1 && st.Toggles.LimitBytes == 20480
	})

	// JDownloader restarts while the link runs and a browser is open (Review Focus 1).
	f.inbox.reset()
	if err := c.RestartJD(ctx); err != nil && !jd.IsConnection(err) {
		t.Fatal(err)
	}
	// Revision 50639 keeps answering for some seconds before it goes down.
	waitLive(t, 60*time.Second, "offline or resync", func() bool {
		c := f.e.Status().Connection
		return c == state.ConnOffline || c == state.ConnSyncing
	})
	sawOffline := false
	var goneAt time.Time
	var upAt time.Time
	waitLive(t, 2*time.Minute, "connected again", func() bool {
		st := f.e.Status()
		switch st.Connection {
		case state.ConnOffline:
			if !sawOffline {
				sawOffline, goneAt = true, time.Now()
			}
			// Nothing may look alive while JDownloader is away.
			if st.Speed != 0 || st.Downloads.Running != 0 || st.Controller != "IDLE" {
				t.Errorf("status while offline = %+v", st)
			}
			for _, l := range f.store.Snapshot(state.Downloads).DlLinks {
				if l.Running || l.Speed != 0 || l.ETA != state.ETAUnknown || l.State == state.LinkRunning {
					t.Errorf("link while offline = %+v", l)
				}
			}
			probe, cancel := context.WithTimeout(context.Background(), time.Second)
			if ok, err := c.Ping(probe); err == nil && ok && upAt.IsZero() {
				upAt = time.Now()
			}
			cancel()
		case state.ConnMisconfigured:
			t.Errorf("a JDownloader that worked is not misconfigured: %+v", st)
		}
		return st.Connection == state.ConnConnected
	})
	if !sawOffline {
		t.Error("never saw offline during the restart")
	}
	if !upAt.IsZero() {
		healed := time.Since(upAt)
		t.Logf("offline for %v, connected %v after JDownloader answered again", time.Since(goneAt), healed)
		if healed > 30*time.Second {
			t.Errorf("healing took %v", healed)
		}
	}
	if !f.inbox.has("connection", `"connection":"offline"`) {
		t.Error("offline was not announced")
	}
	if !f.inbox.has("patch", link.ID, `"running":false`, `"speed":0`, `"eta":null`) {
		t.Error("the stop of the link was not sent as a patch")
	}
	if !f.inbox.has("structure", `{"list":"downloads"}`) {
		t.Error("the resync was not announced")
	}
	if f.store.Stale() {
		t.Error("mirror still stale")
	}

	// The mirror agrees with JDownloader: same ids, nothing running that is not.
	waitLive(t, 30*time.Second, "the mirror agrees with JDownloader", func() bool {
		real, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{Running: true})
		if err != nil {
			return false
		}
		mine := f.store.Snapshot(state.Downloads).DlLinks
		if len(real) != len(mine) {
			return false
		}
		for i := range real {
			if fmt.Sprint(real[i].UUID) != mine[i].ID || real[i].Running != mine[i].Running {
				return false
			}
		}
		return true
	})

	// Live events flow on the new subscription: the download goes on when started.
	if err := c.ControllerStart(ctx); err != nil {
		t.Fatal(err)
	}
	f.inbox.reset()
	waitLive(t, 60*time.Second, "progress patches after the restart", func() bool {
		return f.inbox.has("patch", `"list":"downloads"`, link.ID, `"bytesLoaded"`)
	})
	if st := f.e.Status(); st.Connection != state.ConnConnected {
		t.Errorf("status = %+v", st)
	}
}
