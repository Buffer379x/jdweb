package jd_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testguard"
)

// These tests talk to a real JDownloader and run only when JDWEB_TEST_JD_URL
// is set, for example to the test stack (scripts/teststack.sh url). They need
// the fixture server of the stack, which JDownloader reaches as
// http://jdweb-test-files:8000. They touch only what they add and remove it
// again. Never point them at a JDownloader that holds real downloads.

func liveClient(t *testing.T) *jd.Client {
	t.Helper()
	raw := os.Getenv("JDWEB_TEST_JD_URL")
	if raw == "" {
		t.Skip("JDWEB_TEST_JD_URL is not set")
	}
	u, err := jd.NormalizeURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := jd.New(u)
	requireOnlyTestData(t, c, "jdweb-live-") // every live test, read-only ones too
	return c
}

// requireOnlyTestData refuses to go on unless the instance is the test stack
// (testguard.Rule), so the tests can never touch a real JDownloader.
func requireOnlyTestData(t *testing.T, c *jd.Client, prefix string) {
	t.Helper()
	if err := testguard.Require(context.Background(), c, os.Getenv("JDWEB_TEST_JD_URL"), prefix); err != nil {
		t.Fatal(err)
	}
}

func TestLiveReadOnly(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	if ok, err := c.Ping(ctx); err != nil || !ok {
		t.Fatalf("ping = %v, %v", ok, err)
	}
	if rev, err := c.CoreRevision(ctx); err != nil || rev < 50000 {
		t.Errorf("revision = %d, %v", rev, err)
	}
	if _, err := c.Uptime(ctx); err != nil {
		t.Error(err)
	}
	if si, err := c.SystemInfos(ctx); err != nil || si.OSFamily == "" || si.HeapMax == 0 {
		t.Errorf("system = %+v, %v", si, err)
	}
	if st, err := c.StorageInfos(ctx, "/output"); err != nil || len(st) != 1 || st[0].Size == 0 {
		t.Errorf("storage = %+v, %v", st, err)
	}
	if tb, err := c.Toolbar(ctx); err != nil || tb.State == "" {
		t.Errorf("toolbar = %+v, %v", tb, err)
	}
	if _, err := c.ControllerState(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.ControllerSpeed(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.Captchas(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.DialogIDs(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.ExtractionQueue(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.Accounts(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.BasicAuths(ctx); err != nil {
		t.Error(err)
	}
	if h, err := c.PremiumHosters(ctx); err != nil || len(h) < 100 {
		t.Errorf("hosters = %d, %v", len(h), err)
	}
	if ex, err := c.Extensions(ctx); err != nil || len(ex) == 0 {
		t.Errorf("extensions = %v, %v", ex, err)
	}
	if pl, err := c.Plugins(ctx); err != nil || len(pl) < 100 || pl[0].ClassName == "" {
		t.Errorf("plugins = %d, %v", len(pl), err)
	}
	if e, err := c.ConfigQuery(ctx, jd.ConfigQuery{Pattern: ".*DownloadSpeedLimit.*", Values: true, DefaultValues: true}); err != nil || len(e) != 3 {
		t.Errorf("config = %+v, %v", e, err)
	}
	if _, err := c.PluginSettings(ctx, jd.ConfigQuery{Pattern: ".*", Values: true}); err != nil {
		t.Error(err)
	}
	if _, _, err := c.SpeedLimit(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.UpdateAvailable(ctx); err != nil {
		t.Error(err)
	}
	if st, err := c.ControllerState(ctx); err != nil {
		t.Error(err)
	} else if !func() bool {
		for _, k := range jd.ControllerStates() {
			if k == st {
				return true
			}
		}
		return false
	}() {
		t.Errorf("controller state %q is not in jd.ControllerStates()", st)
	}
	for name, f := range map[string]func(string) ([]byte, string, error){
		"1fichier.com": func(n string) ([]byte, string, error) { return c.HosterIcon(ctx, n) },
		"archive.zip":  func(n string) ([]byte, string, error) { return c.FileIcon(ctx, n) },
	} {
		data, ct, err := f(name)
		if err != nil || ct != "image/png" || !strings.HasPrefix(string(data), "\x89PNG") {
			t.Errorf("icon %s: %d bytes, %q, %v", name, len(data), ct, err)
		}
	}
	if _, err := c.DownloadFolderHistory(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.DownloadPackages(ctx, jd.AllDownloadPackageFields()); err != nil {
		t.Error(err)
	}
	if _, err := c.DownloadLinks(ctx, jd.AllDownloadLinkFields()); err != nil {
		t.Error(err)
	}
	if _, err := c.GrabberPackages(ctx, jd.AllGrabberPackageFields()); err != nil {
		t.Error(err)
	}
	if _, err := c.GrabberLinks(ctx, jd.AllGrabberLinkFields()); err != nil {
		t.Error(err)
	}
	if _, err := c.StopMarkedLink(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.IsCollecting(ctx); err != nil {
		t.Error(err)
	}
	if _, err := c.IsCrawling(ctx); err != nil {
		t.Error(err)
	}
	// The error model on a real JDownloader.
	err := c.Call(ctx, "/nonexistent/path", nil)
	if !jd.IsNotImplemented(err) {
		t.Errorf("unknown path: %v", err)
	}
	err = c.Call(ctx, "/downloadsV2/removeLinks", nil, "a", "b")
	if e, ok := err.(*jd.Error); !ok || e.Type != jd.TypeBadParameters || e.Status != 400 {
		t.Errorf("bad parameters: %v", err)
	}
}

func TestLivePublishers(t *testing.T) {
	c := liveClient(t)
	pubs, err := c.ListPublishers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, p := range pubs {
		have[p.Publisher] = true
	}
	for _, want := range jd.Publishers() {
		if !have[want] {
			t.Errorf("JDownloader has no publisher %q", want)
		}
	}
	if have["linkgrabberv2"] {
		t.Error("linkgrabberv2 became a publisher")
	}
}

// TestLiveLifecycle adds links, edits them in the linkgrabber, moves them to
// the download list, lets two of them download with a speed limit and checks
// the event stream along the way.
func TestLiveLifecycle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	const pkgName = "jdweb-live-test"
	requireOnlyTestData(t, c, "jdweb-live-")

	wasOn, wasLimit, err := c.SpeedLimit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = c.ControllerStop(bg)
		ops := c.Downloads()
		if pk, err := c.DownloadPackages(bg, jd.DownloadPackageQuery{}); err == nil {
			for _, p := range pk {
				if strings.HasPrefix(p.Name, pkgName) {
					_ = ops.Remove(bg, nil, []int64{p.UUID})
				}
			}
		}
		if pk, err := c.GrabberPackages(bg, jd.GrabberPackageQuery{}); err == nil {
			for _, p := range pk {
				if strings.HasPrefix(p.Name, pkgName) {
					_ = c.Linkgrabber().Remove(bg, nil, []int64{p.UUID})
				}
			}
		}
		_ = c.SetSpeedLimit(bg, wasOn, wasLimit)
	})

	// Start from stopped downloads, otherwise there is no RUNNING event to see.
	_ = c.ControllerStop(ctx)
	waitFor(t, 30*time.Second, "stopped downloads", func() bool {
		st, _ := c.ControllerState(ctx)
		return st != jd.ControllerRunning && st != jd.ControllerStopping
	})

	sub, err := c.Subscribe(ctx, jd.Publishers())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Unsubscribe(context.Background(), sub.ID) })
	if sub.ID == 0 || sub.MaxPollTimeout != 25000 {
		t.Fatalf("subscription = %+v", sub)
	}
	if err := c.SetStatusEventInterval(ctx, sub.ID, 1000); err != nil {
		t.Fatal(err)
	}
	st, err := c.SubscriptionStatus(ctx, sub.ID)
	if err != nil || !st.Exists(sub.ID) {
		t.Fatalf("status = %+v, %v", st, err)
	}

	if err := c.SetSpeedLimit(ctx, true, 20480); err != nil {
		t.Fatal(err)
	}
	if on, n, _ := c.SpeedLimit(ctx); !on || n != 20480 {
		t.Errorf("speed limit = %v %d", on, n)
	}

	no := false
	_, err = c.AddLinks(ctx, jd.AddLinksRequest{
		Links:       []string{"http://jdweb-test-files:8000/slow/live-a.bin", "http://jdweb-test-files:8000/slow/live-b.bin", "http://jdweb-test-files:8000/live-c.bin"},
		PackageName: pkgName, DestinationFolder: "/output/jdweb-live", Autostart: &no, AutoExtract: &no, DeepDecrypt: &no,
		Priority: jd.PriorityHigh, Comment: "live", DownloadPassword: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	var pkg jd.GrabberPackage
	var links []jd.GrabberLink
	waitFor(t, 30*time.Second, "three online links", func() bool {
		pk, err := c.GrabberPackages(ctx, jd.AllGrabberPackageFields())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pk {
			if p.Name == pkgName && p.OnlineCount == 3 {
				pkg = p
				links, _ = c.GrabberLinks(ctx, jd.GrabberLinkQuery{PackageIDs: []int64{p.UUID}, Availability: true, BytesTotal: true, URL: true, Priority: true, Comment: true, Password: true, Enabled: true})
				return len(links) == 3
			}
		}
		return false
	})
	if pkg.SaveTo != "/output/jdweb-live/"+pkgName || pkg.Priority != "" && pkg.Priority != jd.PriorityHigh {
		t.Errorf("package = %+v", pkg)
	}
	for _, l := range links {
		if l.Availability != jd.AvailabilityOnline || l.BytesTotal == 0 || l.Priority != jd.PriorityHigh || l.Comment != "live" || l.Password != "pw" || !l.Enabled {
			t.Errorf("link = %+v", l)
		}
	}

	lg := c.Linkgrabber()
	byName := map[string]int64{}
	for _, l := range links {
		byName[l.Name] = l.UUID
	}
	if err := lg.RenameLink(ctx, byName["live-c.bin"], "live-renamed.bin"); err != nil {
		t.Fatal(err)
	}
	if err := lg.SetEnabled(ctx, false, []int64{byName["live-b.bin"]}, nil); err != nil {
		t.Fatal(err)
	}
	if err := lg.SetPriority(ctx, jd.PriorityLow, []int64{byName["live-a.bin"]}, nil); err != nil {
		t.Fatal(err)
	}
	if err := lg.RenamePackage(ctx, pkg.UUID, pkgName+"-x"); err != nil {
		t.Fatal(err)
	}
	if err := lg.SetDirectory(ctx, "/output/jdweb-live2", []int64{pkg.UUID}); err != nil {
		t.Fatal(err)
	}
	urls, err := lg.URLs(ctx, []int64{byName["live-a.bin"]}, nil, jd.UrlContent)
	if err != nil || len(urls) != 1 {
		t.Errorf("urls = %v, %v", urls, err)
	}
	got, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{PackageIDs: []int64{pkg.UUID}, Enabled: true, Priority: true})
	for _, l := range got {
		switch l.UUID {
		case byName["live-b.bin"]:
			if l.Enabled {
				t.Error("live-b.bin should be disabled")
			}
		case byName["live-a.bin"]:
			// Revision 50639 reports LOW (and LOWEST) back as LOWER.
			if l.Priority != jd.PriorityLower {
				t.Errorf("live-a priority = %q", l.Priority)
			}
		}
		if l.UUID == byName["live-c.bin"] && l.Name != "live-renamed.bin" {
			t.Errorf("rename: %q", l.Name)
		}
	}

	counter, _, err := c.DownloadsStructureChanged(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.MoveToDownloads(ctx, nil, []int64{pkg.UUID}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "download list changed", func() bool {
		_, changed, _ := c.DownloadsStructureChanged(ctx, counter)
		return changed
	})
	var dp jd.DownloadPackage
	pks, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
	for _, p := range pks {
		if p.Name == pkgName+"-x" {
			dp = p
		}
	}
	if dp.UUID == 0 || dp.UUID == pkg.UUID || dp.ChildCount != 3 || dp.SaveTo != "/output/jdweb-live2" {
		t.Fatalf("package in the download list = %+v (linkgrabber id %d)", dp, pkg.UUID)
	}
	if left, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{PackageIDs: []int64{pkg.UUID}}); len(left) != 0 {
		t.Errorf("linkgrabber still has %d links", len(left))
	}
	dls, _ := c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
	var mine []jd.DownloadLink
	for _, l := range dls {
		if l.PackageUUID == dp.UUID {
			mine = append(mine, l)
			if _, kept := byNameValue(byName, l.UUID); !kept {
				t.Errorf("link %d got a new id when it moved", l.UUID)
			}
		}
	}
	if len(mine) != 3 {
		t.Fatalf("links = %+v", mine)
	}

	// Download, with the progress events.
	if err := c.ControllerStart(ctx); err != nil {
		t.Fatal(err)
	}
	var sawProgress, sawRunning bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !(sawProgress && sawRunning) {
		lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ev, err := c.Listen(lctx, sub.ID)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ev {
			sawProgress = sawProgress || e.Publisher == jd.PublisherDownloads && strings.HasPrefix(e.EventID, "LINK_UPDATE.bytesLoaded")
			sawRunning = sawRunning || e.Publisher == jd.PublisherWatchdog && e.EventID == "RUNNING"
		}
	}
	if !sawProgress || !sawRunning {
		t.Errorf("events: progress=%v running=%v", sawProgress, sawRunning)
	}
	if st, _ := c.ControllerState(ctx); st != jd.ControllerRunning {
		t.Errorf("state = %s", st)
	}
	tb, _ := c.Toolbar(ctx)
	if !tb.Limit || tb.LimitSpeed != 20480 {
		t.Errorf("toolbar = %+v", tb)
	}
	if err := c.ControllerPause(ctx, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.ControllerState(ctx); st != jd.ControllerPause {
		t.Errorf("state = %s", st)
	}
	if err := c.ControllerPause(ctx, false); err != nil {
		t.Fatal(err)
	}
	// A stop mark set right after the pause switch was dropped by JDownloader
	// in about one of four runs (cause not found), so set it until it sticks.
	waitFor(t, 10*time.Second, "the stop mark", func() bool {
		if err := c.SetStopMark(ctx, mine[0].UUID, -1); err != nil {
			t.Fatal(err)
		}
		l, err := c.StopMarkedLink(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return l != nil && l.UUID == mine[0].UUID
	})
	if err := c.RemoveStopMark(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ControllerStop(ctx); err != nil {
		t.Fatal(err)
	}

	_ = c.ControllerStop(ctx)

	// Removing something that does not exist is not an error.
	if err := c.Downloads().Remove(ctx, []int64{1, 2, 3}, []int64{4}); err != nil {
		t.Errorf("remove of vanished ids: %v", err)
	}
	if err := c.Downloads().Remove(ctx, nil, []int64{dp.UUID}); err != nil {
		t.Fatal(err)
	}
	if left, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{PackageIDs: []int64{dp.UUID}}); len(left) != 0 {
		t.Errorf("links left: %+v", left)
	}

	// An unknown subscription is a 404, which is how a restarted JDownloader shows.
	if err := c.Unsubscribe(ctx, sub.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.SubscriptionStatus(ctx, sub.ID); st.Exists(sub.ID) {
		t.Errorf("status after unsubscribe = %+v", st)
	}
	if _, err := c.Listen(ctx, sub.ID); !jd.IsNotFound(err) {
		t.Errorf("listen after unsubscribe: %v", err)
	}
}

// TestLiveSkipped makes JDownloader skip a link (destination without space)
// and reads the reason from the advanced status.
func TestLiveSkipped(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	no := false
	const name = "jdweb-live-skip"
	requireOnlyTestData(t, c, "jdweb-live-")
	t.Cleanup(func() {
		bg := context.Background()
		_ = c.ControllerStop(bg)
		if pk, err := c.DownloadPackages(bg, jd.DownloadPackageQuery{}); err == nil {
			for _, p := range pk {
				if p.Name == name {
					_ = c.Downloads().Remove(bg, nil, []int64{p.UUID})
				}
			}
		}
	})
	if _, err := c.AddLinks(ctx, jd.AddLinksRequest{Links: []string{"http://jdweb-test-files:8000/live-skip.bin"}, PackageName: name, DestinationFolder: "/proc/jdweb-nope", Autostart: &no}); err != nil {
		t.Fatal(err)
	}
	var pkgID int64
	waitFor(t, 30*time.Second, "the link in the linkgrabber", func() bool {
		pk, _ := c.GrabberPackages(ctx, jd.GrabberPackageQuery{})
		for _, p := range pk {
			if p.Name == name {
				pkgID = p.UUID
			}
		}
		l, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{PackageIDs: []int64{pkgID}, Availability: true})
		return pkgID != 0 && len(l) == 1 && l[0].Availability == jd.AvailabilityOnline
	})
	if err := c.MoveToDownloads(ctx, nil, []int64{pkgID}); err != nil {
		t.Fatal(err)
	}
	var link jd.DownloadLink
	waitFor(t, 10*time.Second, "the link in the download list", func() bool {
		ls, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
		for _, l := range ls {
			if l.Name == "live-skip.bin" {
				link = l
			}
		}
		return link.UUID != 0
	})
	if err := c.ControllerStop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.DownloadsForce(ctx, []int64{link.UUID}, nil); err != nil {
		t.Fatal(err)
	}
	var got jd.DownloadLink
	waitFor(t, 30*time.Second, "a skipped link", func() bool {
		ls, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{LinkIDs: []int64{link.UUID}, Skipped: true, AdvancedStatus: true, Status: true})
		if len(ls) == 1 && ls[0].Skipped {
			got = ls[0]
		}
		return got.UUID != 0
	})
	if got.AdvancedStatus.SkipReason() == "" || got.Status == "" {
		t.Errorf("skipped link = %+v", got)
	}
	t.Logf("skip reason %q, status %q", got.AdvancedStatus.SkipReason(), got.Status)
	if err := c.DownloadsUnskip(ctx, nil, []int64{link.UUID}, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "unskipped", func() bool {
		ls, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{LinkIDs: []int64{link.UUID}, Skipped: true})
		return len(ls) == 1 && !ls[0].Skipped
	})
	_ = c.ControllerStop(ctx)
}

func byNameValue(m map[string]int64, id int64) (string, bool) {
	for k, v := range m {
		if v == id {
			return k, true
		}
	}
	return "", false
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}
