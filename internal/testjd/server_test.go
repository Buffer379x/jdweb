package testjd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

var ctx = context.Background()

func setup(t *testing.T) (*testjd.Server, *jd.Client) {
	t.Helper()
	s := testjd.New(t)
	return s, jd.New(s.URL)
}

func post(t *testing.T, url, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func TestProtocol(t *testing.T) {
	s, c := setup(t)
	if ok, err := c.Ping(ctx); err != nil || !ok {
		t.Fatalf("ping = %v, %v", ok, err)
	}
	if rev, err := c.CoreRevision(ctx); err != nil || rev != 50639 {
		t.Fatalf("revision = %d, %v", rev, err)
	}

	t.Run("a request with an Origin is refused like by JDownloader", func(t *testing.T) {
		code, body := post(t, s.URL+"/device/ping", `{"params":[]}`, map[string]string{"Origin": "http://x"})
		if code != 403 || !strings.Contains(body, "AUTH_FAILED") || !strings.Contains(body, "Bad Origin") {
			t.Errorf("%d %s", code, body)
		}
	})
	t.Run("unknown path is 501 without body", func(t *testing.T) {
		code, body := post(t, s.URL+"/nope/nothing", `{"params":[]}`, nil)
		if code != 501 || body != "" {
			t.Errorf("%d %q", code, body)
		}
	})
	t.Run("wrong parameter type is BAD_PARAMETERS", func(t *testing.T) {
		code, body := post(t, s.URL+"/downloadsV2/removeLinks", `{"params":[1,2]}`, nil)
		if code != 400 || !strings.Contains(body, "BAD_PARAMETERS") {
			t.Errorf("%d %s", code, body)
		}
		err := c.Call(ctx, "/downloadsV2/removeLinks", nil, "a", "b")
		var e *jd.Error
		if !asErr(err, &e) || e.Type != jd.TypeBadParameters || e.Status != 400 || e.Src != "DEVICE" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("missing parameter is BAD_PARAMETERS", func(t *testing.T) {
		if code, _ := post(t, s.URL+"/downloadsV2/removeLinks", `{"params":[]}`, nil); code != 400 {
			t.Errorf("code = %d", code)
		}
	})
	t.Run("unparsable body is a 500 stacktrace", func(t *testing.T) {
		code, body := post(t, s.URL+"/device/ping", `garbage`, nil)
		if code != 500 || !strings.HasPrefix(body, "java.lang.RuntimeException") {
			t.Errorf("%d %s", code, body)
		}
	})
}

func asErr(err error, target **jd.Error) bool {
	e, ok := err.(*jd.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestCallLog(t *testing.T) {
	s, c := setup(t)
	_, _ = c.Ping(ctx)
	_ = c.Downloads().SetPriority(ctx, jd.PriorityLow, []int64{5}, nil)
	post(t, s.URL+"/device/ping", `{"params":[]}`, map[string]string{"Origin": "http://evil"})

	calls := s.Calls()
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Path != "/device/ping" || calls[0].Method != "POST" || calls[0].Origin != "" || calls[0].ContentType != "application/json" || calls[0].Status != 200 || string(calls[0].Params) != "[]" {
		t.Errorf("call 0 = %+v", calls[0])
	}
	if calls[1].Path != "/downloadsV2/setPriority" || string(calls[1].Params) != `["LOW",[5],[]]` {
		t.Errorf("call 1 = %+v", calls[1])
	}
	if args := calls[1].Args(); len(args) != 3 || string(args[0]) != `"LOW"` {
		t.Errorf("args = %s", args)
	}
	if calls[2].Origin != "http://evil" || calls[2].Status != 403 {
		t.Errorf("call 2 = %+v", calls[2])
	}
	if n := len(s.CallsTo("/device/ping")); n != 2 {
		t.Errorf("CallsTo = %d", n)
	}
	s.ResetCalls()
	if len(s.Calls()) != 0 {
		t.Error("ResetCalls")
	}
}

func seed(s *testjd.Server) (pkg int64, links []int64) {
	return s.AddPackage(testjd.Downloads, testjd.Package{Name: "Pkg", SaveTo: "/output/Pkg", Priority: "HIGH"},
		testjd.Link{Name: "a.bin", URL: "http://h/a.bin", BytesTotal: 1000, BytesLoaded: 250, Speed: 100, Running: true, Host: "h1"},
		testjd.Link{Name: "b.bin", URL: "http://h/b.bin", BytesTotal: 2000, Finished: true, BytesLoaded: 2000, Host: "h2"},
		testjd.Link{Name: "c.bin", URL: "http://h/c.bin", BytesTotal: 3000, Disabled: true, Host: "h2"},
		testjd.Link{Name: "d.bin", URL: "http://h/d.bin", BytesTotal: 4000, Skipped: true, SkipReason: "DISK_FULL", Host: "h1"},
	)
}

func TestDownloadQueries(t *testing.T) {
	s, c := setup(t)
	pkg, ids := seed(s)
	if pkg < 1<<31 || ids[0] < 1<<31 {
		t.Errorf("ids %d %d should not fit into 32 bits, like in JDownloader", pkg, ids[0])
	}

	links, err := c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
	if err != nil || len(links) != 4 {
		t.Fatal(links, err)
	}
	a, b, cc, d := links[0], links[1], links[2], links[3]
	if a.Name != "a.bin" || !a.Running || !a.Enabled || a.PackageUUID != pkg || a.BytesLoaded != 250 || a.Speed != 100 || a.Status != "Download" {
		t.Errorf("a = %+v", a)
	}
	if !b.Finished || b.Running || b.Status != "Finished" || b.AdvancedStatus["FinalLinkState"].ID != "FINISHED" {
		t.Errorf("b = %+v", b)
	}
	if cc.Enabled {
		t.Errorf("c = %+v", cc)
	}
	if !d.Skipped || d.AdvancedStatus.SkipReason() != jd.SkipReasonDiskFull {
		t.Errorf("d = %+v", d)
	}

	t.Run("false zero and empty fields are left out like JDownloader does", func(t *testing.T) {
		var raw []map[string]json.RawMessage
		q := jd.AllDownloadLinkFields()
		if err := c.Call(ctx, "/downloadsV2/queryLinks", &raw, q); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"enabled"} {
			if _, ok := raw[2][k]; ok {
				t.Errorf("disabled link has %q", k)
			}
		}
		for _, k := range []string{"finished", "skipped", "running", "finishedDate", "extractionStatus", "comment"} {
			if _, ok := raw[0][k]; k != "running" && ok {
				t.Errorf("link a has %q", k)
			}
		}
		if _, ok := raw[1]["running"]; ok {
			t.Error("finished link has running")
		}
	})
	t.Run("without field flags only uuid, packageUUID and name", func(t *testing.T) {
		var raw []map[string]json.RawMessage
		if err := c.Call(ctx, "/downloadsV2/queryLinks", &raw, map[string]any{}); err != nil {
			t.Fatal(err)
		}
		if len(raw[0]) != 3 {
			t.Errorf("fields = %v", raw[0])
		}
	})
	t.Run("maxResults and startAt", func(t *testing.T) {
		page, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{MaxResults: 2, StartAt: 1})
		if err != nil || len(page) != 2 || page[0].Name != "b.bin" || page[1].Name != "c.bin" {
			t.Errorf("page = %+v, %v", page, err)
		}
		page, err = c.DownloadLinks(ctx, jd.DownloadLinkQuery{StartAt: 3})
		if err != nil || len(page) != 1 || page[0].Name != "d.bin" {
			t.Errorf("tail = %+v, %v", page, err)
		}
		page, err = c.DownloadLinks(ctx, jd.DownloadLinkQuery{StartAt: 9})
		if err != nil || len(page) != 0 {
			t.Errorf("beyond = %+v, %v", page, err)
		}
	})
	t.Run("filter by id", func(t *testing.T) {
		got, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{LinkIDs: []int64{ids[2], ids[0]}})
		if err != nil || len(got) != 2 || got[0].Name != "a.bin" || got[1].Name != "c.bin" {
			t.Errorf("by link id = %+v, %v", got, err)
		}
		got, err = c.DownloadLinks(ctx, jd.DownloadLinkQuery{PackageIDs: []int64{12345}})
		if err != nil || len(got) != 0 {
			t.Errorf("by unknown package = %+v, %v", got, err)
		}
	})

	pkgs, err := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
	if err != nil || len(pkgs) != 1 {
		t.Fatal(pkgs, err)
	}
	p := pkgs[0]
	if p.UUID != pkg || p.Name != "Pkg" || p.ChildCount != 4 || p.BytesTotal != 10000 || p.BytesLoaded != 2250 || p.Speed != 100 || !p.Running || p.Finished ||
		p.SaveTo != "/output/Pkg" || p.Priority != jd.PriorityHigh || len(p.Hosts) != 2 || p.Hosts[0] != "h1" {
		t.Errorf("package = %+v", p)
	}
	if p.ETA != 77 { // (10000-2250) bytes left at 100 bytes/s
		t.Errorf("ETA = %d", p.ETA)
	}
}

func TestLinkgrabberQueriesAndCounts(t *testing.T) {
	s, c := setup(t)
	s.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "G"},
		testjd.Link{Name: "on", BytesTotal: 1},
		testjd.Link{Name: "off", Availability: "OFFLINE"},
		testjd.Link{Name: "unk", Availability: "UNKNOWN"},
		testjd.Link{Name: "tmp", Availability: "TEMP_UNKNOWN"},
	)
	pk, err := c.GrabberPackages(ctx, jd.AllGrabberPackageFields())
	if err != nil || len(pk) != 1 {
		t.Fatal(pk, err)
	}
	if p := pk[0]; p.OnlineCount != 1 || p.OfflineCount != 1 || p.UnknownCount != 1 || p.TempUnknownCount != 1 || p.ChildCount != 4 {
		t.Errorf("package = %+v", pk[0])
	}
	ln, err := c.GrabberLinks(ctx, jd.AllGrabberLinkFields())
	if err != nil || len(ln) != 4 || ln[0].Availability != jd.AvailabilityOnline || ln[1].Availability != jd.AvailabilityOffline || ln[3].Availability != jd.AvailabilityTempUnknown {
		t.Errorf("links = %+v, %v", ln, err)
	}
	// The two lists are separate.
	if dl, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); len(dl) != 0 {
		t.Errorf("download list = %+v", dl)
	}
}

func TestMoveToDownloadlist(t *testing.T) {
	s, c := setup(t)
	pkg, ids := s.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "G", SaveTo: "/o/G"}, testjd.Link{Name: "a"}, testjd.Link{Name: "b"}, testjd.Link{Name: "c"})
	_, other := s.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "H"}, testjd.Link{Name: "h"})

	sub, err := c.Subscribe(ctx, jd.Publishers())
	if err != nil {
		t.Fatal(err)
	}
	before, _, _ := c.DownloadsStructureChanged(ctx, 0)

	if err := c.MoveToDownloads(ctx, []int64{ids[0], ids[2]}, nil); err != nil {
		t.Fatal(err)
	}
	dl, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
	if len(dl) != 2 || dl[0].UUID != ids[0] || dl[1].UUID != ids[2] {
		t.Fatalf("download links = %+v", dl)
	}
	dp, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
	if len(dp) != 1 || dp[0].Name != "G" || dp[0].UUID == pkg || dp[0].SaveTo != "/o/G" {
		t.Errorf("the package gets a new id: %+v (old %d)", dp, pkg)
	}
	if dl[0].PackageUUID != dp[0].UUID {
		t.Errorf("link package = %d, want %d", dl[0].PackageUUID, dp[0].UUID)
	}
	lg, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{})
	if len(lg) != 2 || lg[0].UUID != ids[1] || lg[1].UUID != other[0] {
		t.Errorf("linkgrabber = %+v", lg)
	}

	after, changed, _ := c.DownloadsStructureChanged(ctx, before)
	if !changed || after == before {
		t.Error("the structure counter of the download list should move")
	}
	if _, changed, _ := c.DownloadsStructureChanged(ctx, after); changed {
		t.Error("and stay where it is without a change")
	}

	got := drain(t, c, sub.ID)
	for _, want := range []string{"linkcollector/CONTENT_REMOVED", "linkcollector/STRUCTURE_REFRESH", "downloads/ADD_PACKAGE", "downloads/REFRESH_STRUCTURE"} {
		if !got[want] {
			t.Errorf("missing event %s in %v", want, got)
		}
	}

	// Moving a whole package takes all its links.
	if err := c.MoveToDownloads(ctx, nil, []int64{lgPackage(t, c, "H")}); err != nil {
		t.Fatal(err)
	}
	if dl, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); len(dl) != 3 || dl[2].Name != "h" {
		t.Errorf("after package move: %+v", dl)
	}
}

func lgPackage(t *testing.T, c *jd.Client, name string) int64 {
	t.Helper()
	pk, _ := c.GrabberPackages(ctx, jd.GrabberPackageQuery{})
	for _, p := range pk {
		if p.Name == name {
			return p.UUID
		}
	}
	t.Fatalf("no package %s", name)
	return 0
}

// drain reads the queued events and returns "publisher/eventid" of each.
func drain(t *testing.T, c *jd.Client, sub int64) map[string]bool {
	t.Helper()
	ev, err := c.Listen(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range ev {
		out[e.Publisher+"/"+e.EventID] = true
	}
	return out
}

func TestRemoveLinks(t *testing.T) {
	s, c := setup(t)
	pkg, ids := seed(s)
	if err := c.Downloads().Remove(ctx, []int64{ids[0], 999999}, nil); err != nil { // a vanished id is no error
		t.Fatal(err)
	}
	if l, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); len(l) != 3 {
		t.Errorf("links = %d", len(l))
	}
	if err := c.Downloads().Remove(ctx, nil, []int64{pkg}); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.DownloadPackages(ctx, jd.DownloadPackageQuery{}); len(p) != 0 {
		t.Errorf("packages = %+v", p)
	}
	// Removing the last link of a package removes the package.
	_, ids2 := s.AddPackage(testjd.Downloads, testjd.Package{Name: "x"}, testjd.Link{Name: "only"})
	if err := c.Downloads().Remove(ctx, ids2, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.DownloadPackages(ctx, jd.DownloadPackageQuery{}); len(p) != 0 {
		t.Errorf("empty package stays: %+v", p)
	}
}

func TestListActions(t *testing.T) {
	s, c := setup(t)
	pkg, ids := seed(s)
	ops := c.Downloads()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(ops.SetEnabled(ctx, false, []int64{ids[0]}, nil))
	must(ops.SetPriority(ctx, jd.PriorityHighest, []int64{ids[1]}, nil))
	must(ops.SetPriority(ctx, jd.PriorityDefault, []int64{ids[0]}, nil))
	must(ops.RenameLink(ctx, ids[1], "renamed.bin"))
	must(ops.RenamePackage(ctx, pkg, "Renamed"))
	must(ops.SetDirectory(ctx, "/new/dir", []int64{pkg}))
	must(ops.SetPassword(ctx, "secret", []int64{ids[2]}, nil))
	links, _ := c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
	if links[0].Enabled || links[1].Priority != jd.PriorityHighest || links[0].Priority != "" || links[1].Name != "renamed.bin" || links[2].Password != "secret" {
		t.Errorf("links = %+v", links)
	}
	pk, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
	if pk[0].Name != "Renamed" || pk[0].SaveTo != "/new/dir" {
		t.Errorf("package = %+v", pk[0])
	}
	if err := ops.SetPriority(ctx, "BOGUS", nil, nil); err == nil || !strings.Contains(err.Error(), "BAD_PARAMETERS") {
		t.Errorf("priority BOGUS: %v", err)
	}

	t.Run("move to new package", func(t *testing.T) {
		must(ops.MoveToNewPackage(ctx, []int64{ids[3]}, nil, "New", "/n"))
		pk, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
		if len(pk) != 2 || pk[1].Name != "New" || pk[1].SaveTo != "/n" || pk[1].ChildCount != 1 || pk[0].ChildCount != 3 {
			t.Errorf("packages = %+v", pk)
		}
	})
	t.Run("move links keeps order rules", func(t *testing.T) {
		pk, _ := c.DownloadPackages(ctx, jd.DownloadPackageQuery{})
		must(ops.MoveLinks(ctx, []int64{ids[2]}, -1, pk[0].UUID)) // to the top of the same package
		l, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{PackageIDs: []int64{pk[0].UUID}})
		if len(l) != 3 || l[0].UUID != ids[2] || l[1].UUID != ids[0] {
			t.Errorf("links = %+v", l)
		}
		must(ops.MoveLinks(ctx, []int64{ids[0]}, ids[3], pk[1].UUID)) // behind d in the other package
		l, _ = c.DownloadLinks(ctx, jd.DownloadLinkQuery{PackageIDs: []int64{pk[1].UUID}})
		if len(l) != 2 || l[0].UUID != ids[3] || l[1].UUID != ids[0] || l[1].PackageUUID != pk[1].UUID {
			t.Errorf("links = %+v", l)
		}
	})
	t.Run("move packages", func(t *testing.T) {
		pk, _ := c.DownloadPackages(ctx, jd.DownloadPackageQuery{})
		must(ops.MovePackages(ctx, []int64{pk[1].UUID}, -1))
		got, _ := c.DownloadPackages(ctx, jd.DownloadPackageQuery{})
		if got[0].UUID != pk[1].UUID || got[1].UUID != pk[0].UUID {
			t.Errorf("order = %+v", got)
		}
	})
	t.Run("split by hoster", func(t *testing.T) {
		mixed, _ := s.AddPackage(testjd.Downloads, testjd.Package{Name: "Mixed"},
			testjd.Link{Name: "x1", Host: "h1"}, testjd.Link{Name: "y", Host: "h2"}, testjd.Link{Name: "x2", Host: "h1"})
		must(ops.SplitByHoster(ctx, nil, []int64{mixed}))
		got, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
		var names []string
		for _, p := range got {
			if strings.HasPrefix(p.Name, "Mixed") {
				names = append(names, p.Name)
				if len(p.Hosts) != 1 {
					t.Errorf("package %s has hosts %v", p.Name, p.Hosts)
				}
			}
		}
		if len(names) != 2 || names[0] != "Mixed-h1" || names[1] != "Mixed-h2" {
			t.Errorf("packages = %v", names)
		}
	})
	t.Run("urls", func(t *testing.T) {
		u, err := ops.URLs(ctx, []int64{ids[1]}, nil)
		if err != nil || len(u["http://h/b.bin"]) != 1 {
			t.Errorf("urls = %v, %v", u, err)
		}
	})
	t.Run("cleanup", func(t *testing.T) {
		count := func() int { l, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); return len(l) }
		n := count()
		must(ops.Cleanup(ctx, nil, nil, jd.CleanupDeleteFinished, jd.CleanupRemoveLinksOnly, jd.SelectionAll))
		l, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
		for _, x := range l {
			if x.UUID == ids[1] {
				t.Error("finished link stays")
			}
		}
		if len(l) != n-1 {
			t.Errorf("links = %d, want %d", len(l), n-1)
		}
		must(ops.Cleanup(ctx, []int64{ids[0]}, nil, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionSelected))
		if got := count(); got != n-2 {
			t.Errorf("links = %d, want %d", got, n-2)
		}
		must(ops.Cleanup(ctx, nil, nil, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionAll))
		if got := count(); got != 0 {
			t.Errorf("links = %d", got)
		}
	})
}

func TestDownloadOnlyActions(t *testing.T) {
	s, c := setup(t)
	_, ids := seed(s)
	if err := c.DownloadsUnskip(ctx, nil, []int64{ids[3]}, jd.SkipReasonManual); err != nil { // another reason: stays skipped
		t.Fatal(err)
	}
	if l := s.Links(testjd.Downloads); !l[3].Skipped {
		t.Error("unskip with another reason cleared the skip")
	}
	if err := c.DownloadsUnskip(ctx, nil, []int64{ids[3]}, ""); err != nil {
		t.Fatal(err)
	}
	if l := s.Links(testjd.Downloads); l[3].Skipped || l[3].SkipReason != "" {
		t.Errorf("link = %+v", l[3])
	}
	if err := c.DownloadsReset(ctx, []int64{ids[1]}, nil); err != nil {
		t.Fatal(err)
	}
	if l := s.Links(testjd.Downloads); l[1].Finished || l[1].BytesLoaded != 0 {
		t.Errorf("link = %+v", l[1])
	}
	if err := c.SetStopMark(ctx, ids[0], -1); err != nil {
		t.Fatal(err)
	}
	if l, err := c.StopMarkedLink(ctx); err != nil || l == nil || l.UUID != ids[0] {
		t.Errorf("stop mark = %+v, %v", l, err)
	}
	if err := c.RemoveStopMark(ctx); err != nil {
		t.Fatal(err)
	}
	if l, err := c.StopMarkedLink(ctx); err != nil || l != nil {
		t.Errorf("stop mark = %+v, %v", l, err)
	}
}

func TestAddLinks(t *testing.T) {
	s, c := setup(t)
	sub, _ := c.Subscribe(ctx, jd.Publishers())
	yes := true
	job, err := c.AddLinks(ctx, jd.AddLinksRequest{Links: []string{"http://h:8000/x/one.bin?size=5", "http://h:8000/two.zip"}, PackageName: "Mine", DestinationFolder: "/o", Priority: jd.PriorityHigh, Comment: "c", DownloadPassword: "pw"})
	if err != nil || job == 0 {
		t.Fatal(job, err)
	}
	lg, _ := c.GrabberLinks(ctx, jd.AllGrabberLinkFields())
	if len(lg) != 2 || lg[0].Name != "one.bin" || lg[0].URL != "http://h:8000/x/one.bin" || lg[0].Priority != jd.PriorityHigh || lg[0].Comment != "c" || lg[0].Password != "pw" || lg[1].Name != "two.zip" {
		t.Errorf("links = %+v", lg)
	}
	pk, _ := c.GrabberPackages(ctx, jd.AllGrabberPackageFields())
	if len(pk) != 1 || pk[0].Name != "Mine" || pk[0].SaveTo != "/o/Mine" {
		t.Errorf("packages = %+v", pk)
	}
	got := drain(t, c, sub.ID)
	for _, want := range []string{"linkcrawler/STARTED", "linkcrawler/FINISHED", "linkcollector/LINK_ADDED", "linkcollector/STRUCTURE_REFRESH"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}

	// autostart moves the links to the download list and starts the controller.
	if _, err := c.AddLinks(ctx, jd.AddLinksRequest{Links: []string{"http://h/auto.bin"}, Autostart: &yes}); err != nil {
		t.Fatal(err)
	}
	if dl, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); len(dl) != 1 || dl[0].Name != "auto.bin" {
		t.Errorf("download list = %+v", dl)
	}
	if st, _ := c.ControllerState(ctx); st != jd.ControllerRunning {
		t.Errorf("state = %s", st)
	}

	// A container is read as a data URL; the fake takes one url per line.
	if _, err := c.AddContainer(ctx, "DLC", []byte("http://h/c1.bin\nhttp://h/c2.bin\n")); err != nil {
		t.Fatal(err)
	}
	if lg, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{}); len(lg) != 4 {
		t.Errorf("links after container = %d", len(lg))
	}
	var e *jd.Error
	err = c.Call(ctx, "/linkgrabberv2/addContainer", nil, "DLC", "not a data url")
	if !asErr(err, &e) || e.Type != jd.TypeInternal || e.Status != 500 {
		t.Errorf("bad container: %v", err)
	}
	if err := c.ClearLinkgrabber(ctx); err != nil {
		t.Fatal(err)
	}
	if lg, _ := c.GrabberLinks(ctx, jd.GrabberLinkQuery{}); len(lg) != 0 {
		t.Errorf("after clear = %+v", lg)
	}
	if s.Links(testjd.Linkgrabber) != nil {
		t.Error("Links(Linkgrabber) after clear")
	}
}

func TestControllerAndToolbar(t *testing.T) {
	s, c := setup(t)
	sub, _ := c.Subscribe(ctx, jd.Publishers())
	seed(s)

	if st, _ := c.ControllerState(ctx); st != jd.ControllerIdle {
		t.Fatalf("initial state = %s", st)
	}
	tb, err := c.Toolbar(ctx)
	if err != nil || tb.State != jd.ControllerIdle || tb.Running || tb.Limit || !tb.Premium || !tb.Reconnect || !tb.Clipboard || tb.StopAfter {
		t.Fatalf("toolbar = %+v, %v", tb, err)
	}
	if err := c.ControllerStart(ctx); err != nil {
		t.Fatal(err)
	}
	tb, _ = c.Toolbar(ctx)
	if tb.State != jd.ControllerRunning || !tb.Running || tb.Speed != 100 || tb.DownloadCurrent != 250 || tb.DownloadComplete != 2000 {
		t.Errorf("running toolbar = %+v", tb)
	}
	if n, _ := c.ControllerSpeed(ctx); n != 100 {
		t.Errorf("speed = %d", n)
	}
	if err := c.ControllerPause(ctx, true); err != nil {
		t.Fatal(err)
	}
	if tb, _ = c.Toolbar(ctx); tb.State != jd.ControllerPause || !tb.Pause || tb.Running {
		t.Errorf("paused toolbar = %+v", tb)
	}
	if err := c.ControllerPause(ctx, false); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.ControllerState(ctx); st != jd.ControllerRunning {
		t.Errorf("resumed state = %s", st)
	}
	if err := c.ControllerStop(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.ControllerState(ctx); st != jd.ControllerStopped || s.Controller() != "STOPPED_STATE" {
		t.Errorf("stopped state = %s", st)
	}
	got := drain(t, c, sub.ID)
	for _, want := range []string{"downloadwatchdog/RUNNING", "downloadwatchdog/PAUSED", "downloadwatchdog/STOPPED"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	s.SetController("PAUSE")
	if st, _ := c.ControllerState(ctx); st != jd.ControllerPause {
		t.Errorf("SetController: %s", st)
	}

	// Toggles return the new state.
	for name, f := range map[string]func(context.Context) (bool, error){
		"premium": c.TogglePremium, "clipboard": c.ToggleClipboard, "reconnect": c.ToggleReconnect, "stopafter": c.ToggleStopAfter, "limit": c.ToggleSpeedLimit,
	} {
		first, err := f(ctx)
		second, err2 := f(ctx)
		if err != nil || err2 != nil || first == second {
			t.Errorf("%s toggled %v then %v (%v, %v)", name, first, second, err, err2)
		}
	}
}

func TestSpeedLimitThroughConfig(t *testing.T) {
	s, c := setup(t)
	on, n, err := c.SpeedLimit(ctx)
	if err != nil || on || n != 51200 {
		t.Fatalf("default = %v %d %v", on, n, err)
	}
	if err := c.SetSpeedLimit(ctx, true, 204800); err != nil {
		t.Fatal(err)
	}
	tb, _ := c.Toolbar(ctx)
	if !tb.Limit || tb.LimitSpeed != 204800 {
		t.Errorf("toolbar = %+v", tb)
	}
	if v := s.Config("org.jdownloader.settings.GeneralSettings", "DownloadSpeedLimit"); v != int64(204800) {
		t.Errorf("config = %v", v)
	}
	// A value of the wrong type is refused with false, an unknown key too.
	if err := c.ConfigSet(ctx, "org.jdownloader.settings.GeneralSettings", "", "DownloadSpeedLimit", "abc"); err != jd.ErrRejected {
		t.Errorf("wrong type: %v", err)
	}
	if err := c.ConfigSet(ctx, "org.jdownloader.settings.GeneralSettings", "", "Nonexistent", 1); err != jd.ErrRejected {
		t.Errorf("unknown key: %v", err)
	}
	if v, err := c.ConfigGet(ctx, "org.jdownloader.settings.GeneralSettings", "", "Nonexistent"); err != nil || v != nil {
		t.Errorf("get unknown = %s, %v", v, err)
	}
	if err := c.ConfigReset(ctx, "org.jdownloader.settings.GeneralSettings", "", "DownloadSpeedLimit"); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := c.SpeedLimit(ctx); n != 51200 {
		t.Errorf("after reset = %d", n)
	}
	e, err := c.ConfigQuery(ctx, jd.ConfigQuery{Pattern: ".*DownloadSpeedLimit.*", Values: true, DefaultValues: true})
	if err != nil || len(e) != 2 || e[0].Key != "DownloadSpeedLimit" || e[0].AbstractType != "INT" || string(e[0].DefaultValue) != "51200" {
		t.Errorf("query = %+v, %v", e, err)
	}
}

func TestCaptchasAndDialogs(t *testing.T) {
	s, c := setup(t)
	sub, _ := c.Subscribe(ctx, jd.Publishers())

	if l, err := c.Captchas(ctx); err != nil || len(l) != 0 {
		t.Fatalf("captchas = %v, %v", l, err)
	}
	id := s.AddCaptcha(testjd.CaptchaJob{Hoster: "example.org", Type: "image", Timeout: 1000, Image: "data:image/png;base64,AAAA"})
	other := s.AddCaptcha(testjd.CaptchaJob{Hoster: "x.org"})
	l, _ := c.Captchas(ctx)
	if len(l) != 2 || l[0].ID != id || l[0].Hoster != "example.org" {
		t.Errorf("captchas = %+v", l)
	}
	if j, _ := c.Captcha(ctx, id); j == nil || j.Timeout != 1000 {
		t.Errorf("captcha = %+v", j)
	}
	if j, err := c.Captcha(ctx, 4); err != nil || j != nil {
		t.Errorf("unknown captcha = %+v, %v", j, err)
	}
	if img, err := c.CaptchaImage(ctx, id); err != nil || img != "data:image/png;base64,AAAA" {
		t.Errorf("image = %q, %v", img, err)
	}
	if err := c.CaptchaSolve(ctx, id, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := c.CaptchaSolve(ctx, id, "abc"); !jd.IsNotFound(err) {
		t.Errorf("solving twice: %v", err)
	}
	if err := c.CaptchaSkip(ctx, other, jd.SkipSingle); err != nil {
		t.Fatal(err)
	}
	if l, _ := c.Captchas(ctx); len(l) != 0 {
		t.Errorf("captchas = %+v", l)
	}

	d := s.AddDialog(testjd.Dialog{Type: "x.ConfirmDialogInterface", Properties: map[string]string{"title": "Sure?"}})
	ids, _ := c.DialogIDs(ctx)
	if len(ids) != 1 || ids[0] != d {
		t.Errorf("ids = %v", ids)
	}
	if dd, err := c.Dialog(ctx, d); err != nil || dd.Type != "x.ConfirmDialogInterface" || dd.Properties["title"] != "Sure?" {
		t.Errorf("dialog = %+v, %v", dd, err)
	}
	if err := c.DialogAnswer(ctx, d, map[string]any{"closereason": "OK"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DialogAnswer(ctx, d, nil); err == nil {
		t.Error("answering twice should fail")
	}
	got := drain(t, c, sub.ID)
	for _, want := range []string{"captchas/NEW", "captchas/DONE", "dialogs/NEW", "dialogs/EXPIRED"} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

func TestEventsLongPoll(t *testing.T) {
	s, c := setup(t)
	s.SetPollTimeout(5 * time.Second)
	sub, err := c.Subscribe(ctx, []string{jd.PublisherDownloads, jd.PublisherWatchdog})
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID < 1<<31 || sub.MaxPollTimeout != 5000 || len(sub.Subscriptions) != 2 {
		t.Errorf("subscription = %+v", sub)
	}

	t.Run("blocks until an event arrives", func(t *testing.T) {
		type res struct {
			ev  []jd.Event
			err error
			d   time.Duration
		}
		ch := make(chan res)
		start := time.Now()
		go func() {
			ev, err := c.Listen(ctx, sub.ID)
			ch <- res{ev, err, time.Since(start)}
		}()
		select {
		case r := <-ch:
			t.Fatalf("listen returned early: %+v", r)
		case <-time.After(150 * time.Millisecond):
		}
		s.Emit(testjd.Event{Publisher: "downloads", EventID: "LINK_UPDATE.bytesLoaded", EventData: map[string]any{"uuid": 5, "bytesLoaded": 10}})
		r := <-ch
		if r.err != nil || len(r.ev) != 1 || r.ev[0].EventID != "LINK_UPDATE.bytesLoaded" || r.d > 2*time.Second {
			t.Fatalf("listen = %+v", r)
		}
		var d struct{ UUID, BytesLoaded int64 }
		if err := json.Unmarshal(r.ev[0].EventData, &d); err != nil || d.UUID != 5 || d.BytesLoaded != 10 {
			t.Errorf("data = %s, %v", r.ev[0].EventData, err)
		}
	})
	t.Run("returns queued events at once, in order, and empties the queue", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			s.Emit(testjd.Event{Publisher: "downloadwatchdog", EventID: []string{"RUNNING", "PAUSED", "STOPPED"}[i]})
		}
		if n := s.Queued(sub.ID); n != 3 {
			t.Errorf("queued = %d", n)
		}
		st, _ := c.SubscriptionStatus(ctx, sub.ID)
		if st.QueueSize != 3 || !st.Exists(sub.ID) || st.Subscribed {
			t.Errorf("status = %+v", st)
		}
		ev, err := c.Listen(ctx, sub.ID)
		if err != nil || len(ev) != 3 || ev[0].EventID != "RUNNING" || ev[2].EventID != "STOPPED" {
			t.Fatalf("events = %+v, %v", ev, err)
		}
		if ev[0].EventData != nil && string(ev[0].EventData) != "null" {
			t.Errorf("data = %s", ev[0].EventData)
		}
		if s.Queued(sub.ID) != 0 {
			t.Error("queue not emptied")
		}
	})
	t.Run("publishers that were not subscribed are not delivered", func(t *testing.T) {
		s.Emit(testjd.Event{Publisher: "captchas", EventID: "NEW"})
		if s.Queued(sub.ID) != 0 {
			t.Error("captchas event was queued")
		}
	})
	t.Run("an empty poll answers with an empty list after the poll timeout", func(t *testing.T) {
		s.SetPollTimeout(80 * time.Millisecond)
		start := time.Now()
		ev, err := c.Listen(ctx, sub.ID)
		if err != nil || len(ev) != 0 || time.Since(start) < 70*time.Millisecond {
			t.Errorf("events = %v, %v after %v", ev, err, time.Since(start))
		}
	})
	t.Run("setsubscription changes the publishers, unsubscribe removes", func(t *testing.T) {
		if _, err := c.SetSubscription(ctx, sub.ID, []string{jd.PublisherCaptchas}); err != nil {
			t.Fatal(err)
		}
		s.Emit(testjd.Event{Publisher: "captchas", EventID: "NEW"})
		s.Emit(testjd.Event{Publisher: "downloads", EventID: "ADD_PACKAGE"})
		if s.Queued(sub.ID) != 1 {
			t.Errorf("queued = %d", s.Queued(sub.ID))
		}
		if err := c.Unsubscribe(ctx, sub.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Listen(ctx, sub.ID); !jd.IsNotFound(err) {
			t.Errorf("listen after unsubscribe: %v", err)
		}
	})
	t.Run("any publisher name is accepted by subscribe, like in JDownloader", func(t *testing.T) {
		sb, err := c.Subscribe(ctx, []string{"linkgrabberv2"})
		if err != nil || sb.ID == 0 {
			t.Errorf("%+v, %v", sb, err)
		}
	})
}

func TestDropSubscription(t *testing.T) {
	s, c := setup(t)
	s.SetPollTimeout(5 * time.Second)
	sub, _ := c.Subscribe(ctx, jd.Publishers())
	other, _ := c.Subscribe(ctx, jd.Publishers())

	errc := make(chan error, 1)
	go func() { _, err := c.Listen(ctx, sub.ID); errc <- err }()
	time.Sleep(100 * time.Millisecond)
	s.DropSubscription()

	select {
	case err := <-errc:
		if !jd.IsNotFound(err) {
			t.Errorf("blocked listen ended with %v, want 404", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a blocked listen did not end after DropSubscription")
	}
	if _, err := c.Listen(ctx, other.ID); !jd.IsNotFound(err) {
		t.Errorf("listen on the other subscription: %v", err)
	}
	st, err := c.SubscriptionStatus(ctx, sub.ID)
	if err != nil || st.Exists(sub.ID) || st.ID != -1 || st.QueueSize != 0 {
		t.Errorf("status = %+v, %v", st, err)
	}
	if len(s.Subscriptions()) != 0 {
		t.Error("subscriptions remain")
	}
	// Subscribing again works, with a new id.
	again, err := c.Subscribe(ctx, jd.Publishers())
	if err != nil || again.ID == sub.ID {
		t.Errorf("resubscribe = %+v, %v", again, err)
	}
}

func TestDownAndUp(t *testing.T) {
	s, c := setup(t)
	seed(s)
	if _, err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	s.SetPollTimeout(5 * time.Second)
	sub, _ := c.Subscribe(ctx, jd.Publishers())
	errc := make(chan error, 1)
	go func() { _, err := c.Listen(ctx, sub.ID); errc <- err }()
	time.Sleep(100 * time.Millisecond)

	s.Down()
	select {
	case err := <-errc:
		if !jd.IsConnection(err) {
			t.Errorf("open listen ended with %v, want a connection error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("open listen survived Down")
	}
	for i := 0; i < 3; i++ {
		if _, err := c.Ping(ctx); !jd.IsConnection(err) {
			t.Fatalf("ping while down: %v", err)
		}
	}
	s.Down() // idempotent

	s.Up()
	s.Up() // idempotent
	if ok, err := c.Ping(ctx); err != nil || !ok {
		t.Fatalf("ping after up: %v, %v", ok, err)
	}
	if l, _ := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); len(l) != 4 {
		t.Errorf("state lost: %d links", len(l))
	}
	// The same base URL works again, subscriptions survive Down/Up unless dropped.
	s.SetPollTimeout(50 * time.Millisecond)
	if _, err := c.Listen(ctx, sub.ID); err != nil {
		t.Errorf("listen after up: %v", err)
	}
}

func TestFailNext(t *testing.T) {
	s, c := setup(t)
	seed(s)

	s.FailNext("/downloadsV2/queryLinks", 500, "java.lang.RuntimeException: boom\n\tat org.appwork.X")
	s.FailNext("/downloadsV2/queryLinks", 400, `{"src":"DEVICE","type":"BAD_PARAMETERS","data":"x"}`)
	s.FailNext("/downloadsV2/queryLinks", 200, `<html>proxy login</html>`)

	_, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
	var e *jd.Error
	if !asErr(err, &e) || e.Type != jd.TypeInternal || !strings.Contains(e.Raw, "boom") {
		t.Errorf("first: %v", err)
	}
	_, err = c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
	if !asErr(err, &e) || e.Type != jd.TypeBadParameters || e.Status != 400 {
		t.Errorf("second: %v", err)
	}
	_, err = c.DownloadLinks(ctx, jd.DownloadLinkQuery{})
	if !jd.IsInvalidResponse(err) {
		t.Errorf("third: %v", err)
	}
	if l, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{}); err != nil || len(l) != 4 {
		t.Errorf("fourth, normal again: %d, %v", len(l), err)
	}
	// Only the named path fails.
	s.FailNext("/device/ping", 501, "")
	if rev, err := c.CoreRevision(ctx); err != nil || rev != 50639 {
		t.Errorf("other path: %v", err)
	}
	if _, err := c.Ping(ctx); !jd.IsNotImplemented(err) {
		t.Errorf("ping: %v", err)
	}
	for _, call := range s.CallsTo("/downloadsV2/queryLinks") {
		if call.Status == 0 {
			t.Errorf("call without status: %+v", call)
		}
	}
}

func TestResetAndMutators(t *testing.T) {
	s, c := setup(t)
	pkg, ids := seed(s)
	if !s.UpdateLink(testjd.Downloads, ids[0], func(l *testjd.Link) { l.BytesLoaded = 900; l.UUID = 1; l.PackageUUID = 2 }) {
		t.Fatal("UpdateLink")
	}
	if s.UpdateLink(testjd.Downloads, 12345, func(*testjd.Link) {}) {
		t.Error("UpdateLink on a missing link")
	}
	if !s.UpdatePackage(testjd.Downloads, pkg, func(p *testjd.Package) { p.Name = "Changed"; p.UUID = 1 }) {
		t.Fatal("UpdatePackage")
	}
	l, _ := c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
	p, _ := c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
	if l[0].BytesLoaded != 900 || l[0].UUID != ids[0] || l[0].PackageUUID != pkg || p[0].Name != "Changed" || p[0].UUID != pkg {
		t.Errorf("ids must not be changeable: %+v %+v", l[0], p[0])
	}
	if id := s.AddLink(testjd.Downloads, pkg, testjd.Link{Name: "e"}); id == 0 {
		t.Error("AddLink")
	}
	if id := s.AddLink(testjd.Downloads, 999, testjd.Link{Name: "e"}); id != 0 {
		t.Error("AddLink to a missing package")
	}
	if !s.RemoveLink(testjd.Downloads, ids[1]) || s.RemoveLink(testjd.Downloads, ids[1]) {
		t.Error("RemoveLink")
	}
	if got := s.Links(testjd.Downloads); len(got) != 4 {
		t.Errorf("links = %d", len(got))
	}
	if got := s.Packages(testjd.Downloads); len(got) != 1 || got[0].Name != "Changed" {
		t.Errorf("packages = %+v", got)
	}
	c.Downloads().Remove(ctx, nil, nil)
	s.Reset()
	if len(s.Links(testjd.Downloads)) != 0 || len(s.Calls()) != 0 {
		t.Error("Reset")
	}
}

func TestStructureCounterOnMutators(t *testing.T) {
	s, c := setup(t)
	n0, changed, _ := c.DownloadsStructureChanged(ctx, 0)
	if !changed {
		t.Fatal("the first call with 0 gets the counter")
	}
	_, ids := seed(s)
	n1, changed, _ := c.DownloadsStructureChanged(ctx, n0)
	if !changed || n1 == n0 {
		t.Error("AddPackage should move the counter")
	}
	s.UpdateLink(testjd.Downloads, ids[0], func(l *testjd.Link) { l.Speed = 5 })
	if _, changed, _ := c.DownloadsStructureChanged(ctx, n1); changed {
		t.Error("progress is not a structure change")
	}
	s.RemoveLink(testjd.Downloads, ids[0])
	if _, changed, _ := c.DownloadsStructureChanged(ctx, n1); !changed {
		t.Error("RemoveLink should move the counter")
	}
	if _, changed, _ := c.GrabberStructureChanged(ctx, 0); !changed {
		t.Error("grabber counter")
	}
}

func TestStaticAnswers(t *testing.T) {
	s, c := setup(t)
	si, err := c.SystemInfos(ctx)
	if err != nil || !si.Docker {
		t.Errorf("system = %+v, %v", si, err)
	}
	st, err := c.StorageInfos(ctx, "/output")
	if err != nil || len(st) != 1 || st[0].Path != "/output" || st[0].Free == 0 {
		t.Errorf("storage = %+v, %v", st, err)
	}
	if b, err := c.UpdateAvailable(ctx); err != nil || b {
		t.Errorf("update = %v, %v", b, err)
	}
	if up, err := c.Uptime(ctx); err != nil || up < 0 {
		t.Errorf("uptime = %d, %v", up, err)
	}
	for name, f := range map[string]func() error{
		"restart": func() error { return c.RestartJD(ctx) }, "reconnect": func() error { return c.Reconnect(ctx) },
		"update check": func() error { return c.CheckForUpdate(ctx) },
	} {
		if err := f(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if a, err := c.Accounts(ctx); err != nil || len(a) != 0 {
		t.Errorf("accounts = %+v, %v", a, err)
	}
	if h, err := c.DownloadFolderHistory(ctx); err != nil || len(h) != 0 {
		t.Errorf("history = %v, %v", h, err)
	}
	if b, err := c.IsCollecting(ctx); err != nil || b {
		t.Errorf("collecting = %v, %v", b, err)
	}
	if b, err := c.IsCrawling(ctx); err != nil || b {
		t.Errorf("crawling = %v, %v", b, err)
	}
	if v, err := c.Variants(ctx, 1); err != nil || len(v) != 0 {
		t.Errorf("variants = %v, %v", v, err)
	}
	_ = s
}

func TestConcurrentUse(t *testing.T) {
	s, c := setup(t)
	seed(s)
	s.SetPollTimeout(50 * time.Millisecond)
	sub, _ := c.Subscribe(ctx, jd.Publishers())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				switch i % 4 {
				case 0:
					_, _ = c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
				case 1:
					s.Emit(testjd.Event{Publisher: "downloads", EventID: "x"})
					s.UpdateLink(testjd.Downloads, s.Links(testjd.Downloads)[0].UUID, func(l *testjd.Link) { l.Speed++ })
				case 2:
					_, _ = c.Listen(ctx, sub.ID)
				case 3:
					_, _ = c.Toolbar(ctx)
					_ = s.Calls()
				}
			}
		}(i)
	}
	wg.Wait()
}
