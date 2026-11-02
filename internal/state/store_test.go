package state_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

const (
	pkgID  = int64(1793564007950)
	linkA  = int64(1793563980901)
	linkB  = int64(1793563981163)
	lgPkg  = int64(1793560000001)
	lgLink = int64(1793560000002)
)

func dlEvent(id, data string) jd.Event {
	return jd.Event{Publisher: jd.PublisherDownloads, EventID: id, EventData: json.RawMessage(data)}
}

func lgEvent(id, data string) jd.Event {
	return jd.Event{Publisher: jd.PublisherLinkCollector, EventID: id, EventData: json.RawMessage(data)}
}

// loaded returns a store with one running package of two links.
func loaded(t testing.TB) *state.Store {
	t.Helper()
	s := state.New()
	s.ReplaceDownloads(
		[]jd.DownloadPackage{{UUID: pkgID, Name: "pkg", BytesTotal: 300, BytesLoaded: 150, Speed: 50, ETA: 3, Enabled: true, Running: true, Hosts: []string{"a.example"}}},
		[]jd.DownloadLink{
			{UUID: linkA, PackageUUID: pkgID, Name: "a.bin", BytesTotal: 100, BytesLoaded: 50, Speed: 50, ETA: 1, Enabled: true, Running: true, Priority: jd.PriorityHigh},
			{UUID: linkB, PackageUUID: pkgID, Name: "b.bin", BytesTotal: 200, BytesLoaded: 100, Enabled: true},
		},
	)
	s.ReplaceLinkgrabber(
		[]jd.GrabberPackage{{UUID: lgPkg, Name: "lg", BytesTotal: 40, ChildCount: 1, Enabled: true}},
		[]jd.GrabberLink{{UUID: lgLink, PackageUUID: lgPkg, Name: "c.bin", BytesTotal: 40, Availability: jd.AvailabilityOnline, Enabled: true}},
	)
	s.Drain()
	return s
}

func link(t testing.TB, s *state.Store, id int64) state.DlLink {
	t.Helper()
	for _, l := range s.Snapshot(state.Downloads).DlLinks {
		if l.ID == fmt.Sprint(id) {
			return l
		}
	}
	t.Fatalf("link %d not in the mirror", id)
	return state.DlLink{}
}

func onePatch(t *testing.T, s *state.Store) state.Patch {
	t.Helper()
	p := s.Drain()
	if len(p) != 1 {
		t.Fatalf("patches = %+v, want one", p)
	}
	return p[0]
}

func TestDeltaSetsOnlyItsField(t *testing.T) {
	s := loaded(t)
	before := link(t, s, linkA)

	eff := s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":77,"uuid":%d}`, linkA)))
	if eff != (state.Effect{}) {
		t.Errorf("effect = %+v", eff)
	}

	after := link(t, s, linkA)
	want := before
	want.BytesLoaded = 77
	if after != want {
		t.Errorf("link = %+v\nwant   %+v", after, want)
	}
	p := onePatch(t, s)
	if p.List != state.Downloads || len(p.Links) != 1 || len(p.Packages) != 0 {
		t.Fatalf("patch = %+v", p)
	}
	f := p.Links[fmt.Sprint(linkA)]
	if len(f) != 1 || f["bytesLoaded"] != int64(77) {
		t.Errorf("fields = %+v", f)
	}
}

func TestDeltaWithTheSameValueMakesNoPatch(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":50,"uuid":%d}`, linkA)))
	if p := s.Drain(); len(p) != 0 {
		t.Errorf("patches = %+v", p)
	}
}

// JDownloader leaves out false values, so the field the event names falls back
// to its zero value when the payload does not carry it.
func TestNamedFieldWithoutValueIsZero(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.running", fmt.Sprintf(`{"uuid":%d}`, linkA)))
	l := link(t, s, linkA)
	if l.Running {
		t.Error("running stayed true")
	}

	s.ApplyEvent(dlEvent("LINK_UPDATE.extractionStatus", fmt.Sprintf(`{"extractionStatus":null,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("LINK_UPDATE.status", fmt.Sprintf(`{"status":null,"statusIconKey":null,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("LINK_UPDATE.eta", fmt.Sprintf(`{"uuid":%d}`, linkA)))
	l = link(t, s, linkA)
	if l.ExtractionStatus != "" || l.Status != "" || l.ETA != state.ETAUnknown {
		t.Errorf("link = %+v", l)
	}
}

func TestRunningFalseClearsSpeedAndETA(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.running", fmt.Sprintf(`{"running":false,"uuid":%d}`, linkA)))
	l := link(t, s, linkA)
	if l.Running || l.Speed != 0 || l.ETA != state.ETAUnknown || l.State != state.LinkWaiting {
		t.Errorf("link = %+v", l)
	}
	f := onePatch(t, s).Links[fmt.Sprint(linkA)]
	if f["running"] != false || f["speed"] != int64(0) || f["eta"] != state.ETAUnknown || f["state"] != state.LinkWaiting {
		t.Errorf("fields = %+v", f)
	}

	// The same for a package.
	s.ApplyEvent(dlEvent("PACKAGE_UPDATE.running", fmt.Sprintf(`{"uuid":%d}`, pkgID)))
	p := s.Snapshot(state.Downloads).DlPackages[0]
	if p.Running || p.Speed != 0 || p.ETA != state.ETAUnknown {
		t.Errorf("package = %+v", p)
	}

	// A late progress event must not bring back a speed on a link that stopped.
	s.ApplyEvent(dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"speed":99,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("LINK_UPDATE.eta", fmt.Sprintf(`{"eta":99,"uuid":%d}`, linkA)))
	l = link(t, s, linkA)
	if l.Speed != 0 || l.ETA != state.ETAUnknown {
		t.Errorf("late progress: %+v", l)
	}
}

func TestNegativeValuesAreNormalized(t *testing.T) {
	s := loaded(t)
	// Both recorded from revision 50639.
	s.ApplyEvent(dlEvent("PACKAGE_UPDATE.eta", fmt.Sprintf(`{"eta":-2147483648,"uuid":%d}`, pkgID)))
	s.ApplyEvent(dlEvent("PACKAGE_UPDATE.speed", fmt.Sprintf(`{"speed":-1,"uuid":%d}`, pkgID)))
	p := s.Snapshot(state.Downloads).DlPackages[0]
	if p.ETA != state.ETAUnknown || p.Speed != 0 {
		t.Errorf("package = %+v", p)
	}
}

func TestReplaceNormalizesLinks(t *testing.T) {
	s := state.New()
	s.ReplaceDownloads(nil, []jd.DownloadLink{
		{UUID: 1, PackageUUID: 9, Name: "stale-speed", Speed: 500, ETA: 7, Enabled: true},
		{UUID: 2, PackageUUID: 9, Name: "unknown-eta", Running: true, ETA: jd.ETAUnknown, Enabled: true},
		{UUID: 3, PackageUUID: 9, Name: "skipped", Skipped: true, Enabled: true, AdvancedStatus: jd.AdvancedStatus{"SkipReason": {ID: "DISK_FULL"}}},
		{UUID: 4, PackageUUID: 9, Name: "running", Running: true, ETA: 5, Speed: 10, Enabled: true},
	})
	l := s.Snapshot(state.Downloads).DlLinks
	if l[0].Speed != 0 || l[0].ETA != state.ETAUnknown {
		t.Errorf("not running: %+v", l[0])
	}
	if l[1].ETA != state.ETAUnknown {
		t.Errorf("eta: %+v", l[1])
	}
	if l[2].SkipReason != "DISK_FULL" || l[2].State != state.LinkSkipped {
		t.Errorf("skip: %+v", l[2])
	}
	if l[3].ETA != 5 || l[3].Speed != 10 || l[3].PackageID != "9" {
		t.Errorf("running: %+v", l[3])
	}
}

func TestSkipReasonFollowsTheDelta(t *testing.T) {
	s := loaded(t)
	// Recorded: the delta of a skipped link carries "skipreason" in lower case.
	s.ApplyEvent(dlEvent("LINK_UPDATE.skipped", fmt.Sprintf(`{"skipreason":"DISK_FULL","uuid":%d,"skipped":true}`, linkB)))
	l := link(t, s, linkB)
	if !l.Skipped || l.SkipReason != "DISK_FULL" || l.State != state.LinkSkipped {
		t.Errorf("link = %+v", l)
	}
	s.ApplyEvent(dlEvent("LINK_UPDATE.skipped", fmt.Sprintf(`{"uuid":%d,"skipped":false}`, linkB)))
	l = link(t, s, linkB)
	if l.Skipped || l.SkipReason != "" || l.State != state.LinkWaiting {
		t.Errorf("link = %+v", l)
	}
}

var failedStatus = jd.AdvancedStatus{"FinalLinkState": {ID: "OFFLINE", Label: "File not found"}}

func TestLinkStateOrder(t *testing.T) {
	tests := []struct {
		name string
		l    jd.DownloadLink
		want state.LinkState
	}{
		{"disabled wins over all", jd.DownloadLink{Enabled: false, ExtractionStatus: "ERROR_CRC", Finished: true, Skipped: true, Running: true}, state.LinkDisabled},
		{"extract error before finished", jd.DownloadLink{Enabled: true, ExtractionStatus: "ERROR_CRC", Finished: true, Skipped: true, Running: true}, state.LinkExtractError},
		{"extract error before failed", jd.DownloadLink{Enabled: true, ExtractionStatus: "ERROR_CRC", AdvancedStatus: failedStatus}, state.LinkExtractError},
		{"failed before finished", jd.DownloadLink{Enabled: true, Finished: true, Skipped: true, Running: true, AdvancedStatus: failedStatus}, state.LinkFailed},
		{"disabled before failed", jd.DownloadLink{Enabled: false, AdvancedStatus: failedStatus}, state.LinkDisabled},
		{"a successful final state is no failure", jd.DownloadLink{Enabled: true, Finished: true, AdvancedStatus: jd.AdvancedStatus{"FinalLinkState": {ID: "FINISHED"}}}, state.LinkFinished},
		{"finished before skipped", jd.DownloadLink{Enabled: true, ExtractionStatus: "SUCCESSFUL", Finished: true, Skipped: true, Running: true}, state.LinkFinished},
		{"skipped before running", jd.DownloadLink{Enabled: true, Skipped: true, Running: true}, state.LinkSkipped},
		{"running", jd.DownloadLink{Enabled: true, Running: true}, state.LinkRunning},
		{"waiting", jd.DownloadLink{Enabled: true}, state.LinkWaiting},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := state.New()
			tc.l.UUID = 1
			s.ReplaceDownloads(nil, []jd.DownloadLink{tc.l})
			if got := s.Snapshot(state.Downloads).DlLinks[0].State; got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStateFollowsDeltas(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.finished", fmt.Sprintf(`{"finished":true,"uuid":%d}`, linkA)))
	if got := link(t, s, linkA).State; got != state.LinkFinished {
		t.Errorf("state = %q", got)
	}
	s.ApplyEvent(dlEvent("LINK_UPDATE.enabled", fmt.Sprintf(`{"uuid":%d,"enabled":false}`, linkA)))
	if got := link(t, s, linkA).State; got != state.LinkDisabled {
		t.Errorf("state = %q", got)
	}
	f := s.Drain()[0].Links[fmt.Sprint(linkA)]
	if f["state"] != state.LinkDisabled {
		t.Errorf("patch = %+v", f)
	}
}

func TestUnknownIDAsksForOneReload(t *testing.T) {
	s := loaded(t)
	ev := dlEvent("LINK_UPDATE.bytesLoaded", `{"bytesLoaded":1,"uuid":4242}`)
	if eff := s.ApplyEvent(ev); eff.Reload != state.Downloads {
		t.Errorf("effect = %+v", eff)
	}
	// Another event for the same id must not ask again and again, or a phantom
	// id would keep the list reloading for ever.
	if eff := s.ApplyEvent(ev); eff.Reload != "" {
		t.Errorf("second effect = %+v", eff)
	}
	if p := s.Drain(); len(p) != 0 {
		t.Errorf("patches = %+v", p)
	}
	if eff := s.ApplyEvent(dlEvent("PACKAGE_UPDATE.speed", `{"speed":1,"uuid":4243}`)); eff.Reload != state.Downloads {
		t.Errorf("package effect = %+v", eff)
	}
	if eff := s.ApplyEvent(lgEvent("LINK_UPDATE.name", `{"name":"x","uuid":4244}`)); eff.Reload != state.Linkgrabber {
		t.Errorf("linkgrabber effect = %+v", eff)
	}

	// Once the id was loaded and vanished again it counts as new.
	s.ReplaceDownloads(nil, []jd.DownloadLink{{UUID: 4242, Enabled: true}})
	s.ReplaceDownloads(nil, nil)
	if eff := s.ApplyEvent(ev); eff.Reload != state.Downloads {
		t.Errorf("effect after a reload = %+v", eff)
	}
}

func TestUnknownEventIsIgnored(t *testing.T) {
	s := loaded(t)
	for _, ev := range []jd.Event{
		{Publisher: "something-new", EventID: "HELLO", EventData: json.RawMessage(`{"uuid":1}`)},
		{Publisher: jd.PublisherDownloads, EventID: "BRAND_NEW"},
		dlEvent("LINK_UPDATE.brandNewField", fmt.Sprintf(`{"brandNewField":1,"uuid":%d}`, linkA)),
	} {
		eff := s.ApplyEvent(ev)
		if !eff.Unknown || eff.Reload != "" || eff.Poll {
			t.Errorf("%s: effect = %+v", ev.EventID, eff)
		}
	}
	// Known fields we do not mirror are no news at all.
	if eff := s.ApplyEvent(dlEvent("LINK_UPDATE.statusIconKey", fmt.Sprintf(`{"statusIconKey":"kc.1","uuid":%d}`, linkA))); eff != (state.Effect{}) {
		t.Errorf("statusIconKey: %+v", eff)
	}
	// An unknown field next to a known one is skipped, the known one applied.
	s.ApplyEvent(dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"speed":5,"shiny":{"a":1},"uuid":%d}`, linkA)))
	if got := link(t, s, linkA).Speed; got != 5 {
		t.Errorf("speed = %d", got)
	}
	// Broken payloads never panic and change nothing.
	want := s.Snapshot(state.Downloads)
	for _, data := range []string{`[]`, `"x"`, `12`, `null`, `{`, `{"uuid":"nope"}`, `{"speed":5}`, fmt.Sprintf(`{"uuid":%d,"speed":"fast"}`, linkA)} {
		eff := s.ApplyEvent(jd.Event{Publisher: jd.PublisherDownloads, EventID: "LINK_UPDATE.speed", EventData: json.RawMessage(data)})
		if !eff.Unknown || eff.Reload != "" {
			t.Errorf("%s: effect = %+v", data, eff)
		}
	}
	if eff := s.ApplyEvent(jd.Event{Publisher: jd.PublisherDownloads, EventID: "LINK_UPDATE.speed"}); !eff.Unknown {
		t.Errorf("no payload: effect = %+v", eff)
	}
	if got := s.Snapshot(state.Downloads); !reflect.DeepEqual(got, want) {
		t.Errorf("mirror changed:\n%+v\nwant\n%+v", got, want)
	}
}

// A wrongly typed value costs that field only, and is no zero for the field the
// event names.
func TestWrongTypeKeepsTheRestOfTheDelta(t *testing.T) {
	s := loaded(t)
	eff := s.ApplyEvent(dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"speed":"fast","bytesLoaded":77,"name":"new.bin","uuid":%d}`, linkA)))
	if !eff.Unknown || eff.Reload != "" {
		t.Errorf("effect = %+v", eff)
	}
	l := link(t, s, linkA)
	if l.BytesLoaded != 77 || l.Name != "new.bin" {
		t.Errorf("the good fields were dropped: %+v", l)
	}
	if l.Speed != 50 {
		t.Errorf("the named field became %d", l.Speed)
	}
	// The field order does not matter, nor a second bad field.
	s.ApplyEvent(dlEvent("LINK_UPDATE.status", fmt.Sprintf(`{"uuid":%d,"enabled":"yes","finished":[1],"comment":"c"}`, linkA)))
	if l := link(t, s, linkA); l.Comment != "c" || !l.Enabled || l.Finished {
		t.Errorf("link = %+v", l)
	}
	// Without a usable uuid nothing can be applied.
	if eff := s.ApplyEvent(dlEvent("LINK_UPDATE.name", `{"uuid":"nope","name":"x"}`)); !eff.Unknown || eff.Reload != "" {
		t.Errorf("effect = %+v", eff)
	}
}

func TestEventEffects(t *testing.T) {
	tests := []struct {
		publisher, id, data string
		want                state.Effect
	}{
		{jd.PublisherDownloads, "ADD_PACKAGE", `{"afterUuid":-1,"uuid":5}`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "ADD_LINK", `null`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "ADD_CONTENT", `null`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "REMOVE_LINK", `{"uuids":[1]}`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "REMOVE_PACKAGE", `{"uuid":5}`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "REMOVE_CONTENT", `null`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherDownloads, "REFRESH_STRUCTURE", `null`, state.Effect{Reload: state.Downloads}},
		// Password and directory changes announce themselves only this way (recorded).
		{jd.PublisherDownloads, "REFRESH_CONTENT", `null`, state.Effect{Reload: state.Downloads}},
		{jd.PublisherLinkCollector, "CONTENT_ADDED", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "CONTENT_REMOVED", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "LINK_ADDED", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "STRUCTURE_REFRESH", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "DATA_REFRESH", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "LIST_LOADED", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "ABORT", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "DUPE_ADDED", `null`, state.Effect{Reload: state.Linkgrabber}},
		{jd.PublisherLinkCollector, "FILTERED_LINKS_EMPTY", `null`, state.Effect{}},
		{jd.PublisherLinkCollector, "FILTERED_LINKS_AVAILABLE", `null`, state.Effect{}},
		{jd.PublisherLinkCrawler, "STARTED", `{"jobId":1,"crawlerId":2}`, state.Effect{Poll: true}},
		{jd.PublisherLinkCrawler, "STOPPED", `{"jobId":1}`, state.Effect{Poll: true}},
		{jd.PublisherLinkCrawler, "FINISHED", `{"jobId":1}`, state.Effect{Poll: true}},
		{jd.PublisherWatchdog, "RUNNING", `null`, state.Effect{Poll: true}},
		{jd.PublisherWatchdog, "PAUSED", `null`, state.Effect{Poll: true}},
		{jd.PublisherWatchdog, "STOPPED", `null`, state.Effect{Poll: true}},
		{jd.PublisherWatchdog, "UPDATE", `null`, state.Effect{}},
		{jd.PublisherCaptchas, "NEW", `{"id":1}`, state.Effect{Poll: true}},
		{jd.PublisherCaptchas, "DONE", `{"id":1}`, state.Effect{Poll: true}},
		// Recorded: dialog events carry the bare id, not an object.
		{jd.PublisherDialogs, "NEW", `1793566756187`, state.Effect{Poll: true}},
		{jd.PublisherDialogs, "EXPIRED", `1793566785799`, state.Effect{Poll: true}},
	}
	for _, tc := range tests {
		t.Run(tc.publisher+"/"+tc.id, func(t *testing.T) {
			s := loaded(t)
			got := s.ApplyEvent(jd.Event{Publisher: tc.publisher, EventID: tc.id, EventData: json.RawMessage(tc.data)})
			if got != tc.want {
				t.Errorf("effect = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLinkgrabberDeltas(t *testing.T) {
	s := loaded(t)
	// Recorded: the linkcollector publisher sends deltas of its own.
	s.ApplyEvent(lgEvent("LINK_UPDATE.name", fmt.Sprintf(`{"name":"renamed.bin","uuid":%d}`, lgLink)))
	s.ApplyEvent(lgEvent("LINK_UPDATE.enabled", fmt.Sprintf(`{"uuid":%d,"enabled":false}`, lgLink)))
	s.ApplyEvent(lgEvent("PACKAGE_UPDATE.name", fmt.Sprintf(`{"name":"pk2","uuid":%d}`, lgPkg)))
	snap := s.Snapshot(state.Linkgrabber)
	if l := snap.LgLinks[0]; l.Name != "renamed.bin" || l.Enabled {
		t.Errorf("link = %+v", l)
	}
	if p := snap.LgPackages[0]; p.Name != "pk2" {
		t.Errorf("package = %+v", p)
	}
	p := onePatch(t, s)
	if p.List != state.Linkgrabber {
		t.Fatalf("patch = %+v", p)
	}
	if f := p.Links[fmt.Sprint(lgLink)]; f["name"] != "renamed.bin" || f["enabled"] != false || len(f) != 2 {
		t.Errorf("fields = %+v", f)
	}
}

// All deltas of a recorded listen answer apply without reload or complaint.
func TestRecordedEvents(t *testing.T) {
	raw, err := os.ReadFile("../jd/testdata/events_listen_downloads.json")
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Data []jd.Event `json:"data"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	links := map[int64]bool{}
	pkgs := map[int64]bool{}
	for _, e := range answer.Data {
		var d struct {
			UUID int64 `json:"uuid"`
		}
		_ = json.Unmarshal(e.EventData, &d)
		switch {
		case strings.HasPrefix(e.EventID, "LINK_UPDATE"):
			links[d.UUID] = true
		case strings.HasPrefix(e.EventID, "PACKAGE_UPDATE"):
			pkgs[d.UUID] = true
		}
	}
	var dp []jd.DownloadPackage
	for id := range pkgs {
		dp = append(dp, jd.DownloadPackage{UUID: id, Enabled: true})
	}
	var dl []jd.DownloadLink
	for id := range links {
		dl = append(dl, jd.DownloadLink{UUID: id, Enabled: true, Running: true, Speed: 1, ETA: 1})
	}
	s := state.New()
	s.ReplaceDownloads(dp, dl)
	for _, e := range answer.Data {
		eff := s.ApplyEvent(e)
		if eff.Unknown {
			t.Errorf("%s reported as unknown", e.EventID)
		}
		if eff.Reload != "" && e.EventID != "REFRESH_CONTENT" {
			t.Errorf("%s asks for a reload", e.EventID)
		}
	}
	if p := s.Drain(); len(p) != 1 || len(p[0].Links) == 0 || len(p[0].Packages) == 0 {
		t.Errorf("patches = %+v", p)
	}
}

func TestPatchesAreBundled(t *testing.T) {
	s := loaded(t)
	for i := 1; i <= 100; i++ {
		s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":%d,"uuid":%d}`, 50+i, linkA)))
		s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":%d,"uuid":%d}`, i, linkB)))
	}
	s.ApplyEvent(dlEvent("PACKAGE_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":7,"uuid":%d}`, pkgID)))
	s.ApplyEvent(lgEvent("LINK_UPDATE.name", fmt.Sprintf(`{"name":"n","uuid":%d}`, lgLink)))

	patches := s.Drain()
	if len(patches) != 2 {
		t.Fatalf("patches = %+v", patches)
	}
	dl := patches[0]
	if dl.List != state.Downloads || len(dl.Links) != 2 || len(dl.Packages) != 1 {
		t.Fatalf("patch = %+v", dl)
	}
	if got := dl.Links[fmt.Sprint(linkA)]["bytesLoaded"]; got != int64(150) {
		t.Errorf("latest value = %v", got)
	}
	if patches[1].List != state.Linkgrabber {
		t.Errorf("second patch = %+v", patches[1])
	}
	if p := s.Drain(); len(p) != 0 {
		t.Errorf("second drain = %+v", p)
	}
}

func TestPatchJSON(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.eta", fmt.Sprintf(`{"eta":-1,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("PACKAGE_UPDATE.eta", fmt.Sprintf(`{"eta":9,"uuid":%d}`, pkgID)))
	b, err := json.Marshal(onePatch(t, s))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"list":"downloads","links":{"%d":{"eta":null}},"packages":{"%d":{"eta":9}}}`, linkA, pkgID)
	if string(b) != want {
		t.Errorf("json = %s\nwant   %s", b, want)
	}

	// Without packages the member is still an object, as the contract says.
	s.ApplyEvent(dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"speed":3,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("LINK_UPDATE.running", fmt.Sprintf(`{"running":true,"uuid":%d}`, linkA)))
	b, _ = json.Marshal(s.Drain()[0])
	if !strings.Contains(string(b), `"packages":{}`) {
		t.Errorf("json = %s", b)
	}
}

// Contract: docs/api.md lists the members of each object, with ids as strings.
func TestDTOJSON(t *testing.T) {
	s := state.New()
	s.ReplaceDownloads(
		[]jd.DownloadPackage{{UUID: 1, Name: "p"}},
		[]jd.DownloadLink{{UUID: 2, PackageUUID: 1, Name: "l", ETA: -1}},
	)
	s.ReplaceLinkgrabber(
		[]jd.GrabberPackage{{UUID: 3, Name: "gp"}},
		[]jd.GrabberLink{{UUID: 4, PackageUUID: 3, Name: "gl", Variant: &jd.LinkVariant{ID: "v", Name: "V"}}},
	)
	check := func(name string, v any, want string) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		members := strings.Split(want, ",")
		if len(got) != len(members) {
			t.Errorf("%s: %d members, want %d: %s", name, len(got), len(members), b)
		}
		for _, m := range members {
			if _, ok := got[m]; !ok {
				t.Errorf("%s: member %q is missing: %s", name, m, b)
			}
		}
		if id, ok := got["id"]; ok {
			if _, isString := id.(string); !isString {
				t.Errorf("%s: id is %T", name, id)
			}
		}
	}
	dl, lg := s.Snapshot(state.Downloads), s.Snapshot(state.Linkgrabber)
	check("DlPackage", dl.DlPackages[0], "id,name,hosts,childCount,bytesTotal,bytesLoaded,speed,eta,enabled,finished,running,saveTo,status,priority,comment,downloadPassword")
	check("DlLink", dl.DlLinks[0], "id,packageId,name,host,url,bytesTotal,bytesLoaded,speed,eta,enabled,finished,running,skipped,skipReason,extractionStatus,status,priority,comment,downloadPassword,addedDate,finishedDate,finalState,state")
	check("LgPackage", lg.LgPackages[0], "id,name,hosts,childCount,bytesTotal,saveTo,onlineCount,offlineCount,unknownCount,tempUnknownCount,enabled,priority,comment,downloadPassword")
	check("LgLink", lg.LgLinks[0], "id,packageId,name,host,url,bytesTotal,availability,enabled,priority,variants,variant,comment,downloadPassword")
	check("Status", state.Status{}, "connection,connectionDetail,controller,speed,downloads,linkgrabber,captchas,dialogs,toggles,jd")
	check("DownloadTotals", state.DownloadTotals{}, "packages,links,bytesTotal,bytesLoaded,running,eta")
	check("GrabberTotals", state.GrabberTotals{}, "packages,links,bytesTotal,crawling")
	check("Toggles", state.Toggles{}, "limit,limitBytes,reconnect,premium,clipboard,stopAfter")
	check("JDInfo", state.JDInfo{}, "revision,docker,url")

	// A package without hosts has an empty array, and an unknown eta is null.
	b, _ := json.Marshal(dl)
	for _, part := range []string{`"hosts":[]`, `"eta":null`, `"id":"1"`, `"packageId":"1"`, `"id":"2"`, `"state":"disabled"`} {
		if !strings.Contains(string(b), part) {
			t.Errorf("%s not in %s", part, b)
		}
	}
	if !strings.HasPrefix(string(b), `{"packages":[`) || !strings.Contains(string(b), `"links":[`) {
		t.Errorf("snapshot json = %s", b)
	}
	b, _ = json.Marshal(lg)
	if !strings.Contains(string(b), `"variant":{"id":"v","name":"V"`) {
		t.Errorf("snapshot json = %s", b)
	}
	// An empty list is two empty arrays, never null.
	b, _ = json.Marshal(state.New().Snapshot(state.Linkgrabber))
	if string(b) != `{"packages":[],"links":[]}` {
		t.Errorf("empty = %s", b)
	}
}

func TestETAJSON(t *testing.T) {
	for raw, want := range map[int64]string{-1: "null", jd.ETAUnknown: "null", 0: "0", 42: "42"} {
		b, err := json.Marshal(state.NewETA(raw))
		if err != nil || string(b) != want {
			t.Errorf("eta %d = %s, %v", raw, b, err)
		}
	}
	var e state.ETA
	if err := json.Unmarshal([]byte("null"), &e); err != nil || e != state.ETAUnknown {
		t.Errorf("null = %d, %v", e, err)
	}
	if err := json.Unmarshal([]byte("5"), &e); err != nil || e != 5 {
		t.Errorf("5 = %d, %v", e, err)
	}
}

func TestReplaceReportsStructureAndPatchesTheRest(t *testing.T) {
	s := loaded(t)
	sameIDs := func(a int64) ([]jd.DownloadPackage, []jd.DownloadLink) {
		return []jd.DownloadPackage{{UUID: pkgID, Name: "pkg", BytesTotal: 300, BytesLoaded: 150, Speed: 50, ETA: 3, Enabled: true, Running: true, Hosts: []string{"a.example"}, SaveTo: "/new"}},
			[]jd.DownloadLink{
				{UUID: linkA, PackageUUID: pkgID, Name: "a.bin", BytesTotal: 100, BytesLoaded: a, Speed: 50, ETA: 1, Enabled: true, Running: true, Priority: jd.PriorityHigh},
				{UUID: linkB, PackageUUID: pkgID, Name: "b.bin", BytesTotal: 200, BytesLoaded: 100, Enabled: true},
			}
	}

	// Same ids in the same order: no structure change, only the difference as a patch.
	p, l := sameIDs(60)
	if s.ReplaceDownloads(p, l) {
		t.Error("structure change reported for changed fields only")
	}
	patch := onePatch(t, s)
	if len(patch.Links) != 1 || patch.Links[fmt.Sprint(linkA)]["bytesLoaded"] != int64(60) || len(patch.Links[fmt.Sprint(linkA)]) != 1 {
		t.Errorf("links = %+v", patch.Links)
	}
	if f := patch.Packages[fmt.Sprint(pkgID)]; f["saveTo"] != "/new" || len(f) != 1 {
		t.Errorf("packages = %+v", patch.Packages)
	}

	// Nothing different: nothing to do.
	if s.ReplaceDownloads(sameIDs(60)) {
		t.Error("structure change reported for no change")
	}
	if got := s.Drain(); len(got) != 0 {
		t.Errorf("patches = %+v", got)
	}

	// Another order, a link in another package, a removed link: structure changes.
	p, l = sameIDs(60)
	l[0], l[1] = l[1], l[0]
	if !s.ReplaceDownloads(p, l) {
		t.Error("order change not reported")
	}
	p, l = sameIDs(60)
	l[0], l[1] = l[1], l[0]
	l[0].PackageUUID = 5
	if !s.ReplaceDownloads(p, l) {
		t.Error("move between packages not reported")
	}
	p, l = sameIDs(60)
	if !s.ReplaceDownloads(p, l[:1]) {
		t.Error("removed link not reported")
	}
	if !s.ReplaceDownloads(nil, l[:1]) {
		t.Error("removed package not reported")
	}

	// A structure change makes the browser reload, so waiting patches are moot.
	s.ReplaceDownloads(sameIDs(1))
	s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":99,"uuid":%d}`, linkA)))
	s.ReplaceDownloads(nil, nil)
	if got := s.Drain(); len(got) != 0 {
		t.Errorf("patches for a list that reloads: %+v", got)
	}
}

func TestReplaceLinkgrabberReportsStructureAndPatchesTheRest(t *testing.T) {
	s := loaded(t)
	if s.ReplaceLinkgrabber(
		[]jd.GrabberPackage{{UUID: lgPkg, Name: "lg", BytesTotal: 40, ChildCount: 1, Enabled: true}},
		[]jd.GrabberLink{{UUID: lgLink, PackageUUID: lgPkg, Name: "c.bin", BytesTotal: 40, Availability: jd.AvailabilityOffline, Enabled: true}},
	) {
		t.Error("structure change reported")
	}
	if f := onePatch(t, s).Links[fmt.Sprint(lgLink)]; f["availability"] != "OFFLINE" || len(f) != 1 {
		t.Errorf("fields = %+v", f)
	}
	if !s.ReplaceLinkgrabber(nil, nil) {
		t.Error("removal not reported")
	}
}

func TestTotals(t *testing.T) {
	s := loaded(t)
	dl, lg := s.Totals()
	// 150 bytes are left at 50 bytes/s.
	want := state.DownloadTotals{Packages: 1, Links: 2, BytesTotal: 300, BytesLoaded: 150, Running: 1, ETA: 3}
	if dl != want {
		t.Errorf("downloads = %+v, want %+v", dl, want)
	}
	if lg != (state.GrabberTotals{Packages: 1, Links: 1, BytesTotal: 40}) {
		t.Errorf("linkgrabber = %+v", lg)
	}

	// Totals follow deltas.
	s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":75,"uuid":%d}`, linkA)))
	s.ApplyEvent(dlEvent("LINK_UPDATE.bytesTotal", fmt.Sprintf(`{"bytesTotal":150,"uuid":%d}`, linkA)))
	dl, _ = s.Totals()
	if dl.BytesTotal != 350 || dl.BytesLoaded != 175 {
		t.Errorf("after delta: %+v", dl)
	}

	// Without speed there is no estimate.
	s.ApplyEvent(dlEvent("LINK_UPDATE.running", fmt.Sprintf(`{"uuid":%d}`, linkA)))
	dl, _ = s.Totals()
	if dl.Running != 0 || dl.ETA != state.ETAUnknown {
		t.Errorf("no speed: %+v", dl)
	}

	dl, lg = state.New().Totals()
	if dl != (state.DownloadTotals{ETA: state.ETAUnknown}) || lg != (state.GrabberTotals{}) {
		t.Errorf("empty: %+v %+v", dl, lg)
	}
}

func TestTotalsETACountsWhatIsLeftToLoad(t *testing.T) {
	s := state.New()
	s.ReplaceDownloads(nil, []jd.DownloadLink{
		{UUID: 1, BytesTotal: 100, BytesLoaded: 0, Speed: 10, Running: true, Enabled: true},
		{UUID: 2, BytesTotal: 500, BytesLoaded: 500, Finished: true, Enabled: true},
		{UUID: 3, BytesTotal: 700, Skipped: true, Enabled: true},
		{UUID: 4, BytesTotal: 900},
		{UUID: 5, BytesTotal: 100, BytesLoaded: 50, Enabled: true},
	})
	dl, _ := s.Totals()
	if dl.ETA != 15 { // (100 + 50) / 10
		t.Errorf("eta = %d", dl.ETA)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	s := loaded(t)
	snap := s.Snapshot(state.Downloads)
	snap.DlLinks[0].Name = "changed"
	snap.DlPackages[0].Name = "changed"
	again := s.Snapshot(state.Downloads)
	if again.DlLinks[0].Name != "a.bin" || again.DlPackages[0].Name != "pkg" {
		t.Error("a snapshot writes through to the store")
	}
	before := s.Snapshot(state.Downloads)
	s.ApplyEvent(dlEvent("LINK_UPDATE.name", fmt.Sprintf(`{"name":"new","uuid":%d}`, linkA)))
	if before.DlLinks[0].Name != "a.bin" {
		t.Error("an event changed an existing snapshot")
	}
}

// Snapshots taken while events run see each event whole: this event sets two
// fields together, and no snapshot may show only one of them.
func TestSnapshotsAreConsistentUnderLoad(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE", fmt.Sprintf(`{"bytesLoaded":1,"bytesTotal":1,"uuid":%d}`, linkA)))
	stop := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		for i := int64(1); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			s.ApplyEvent(dlEvent("LINK_UPDATE", fmt.Sprintf(`{"bytesLoaded":%d,"bytesTotal":%d,"uuid":%d}`, i, i, linkA)))
		}
	}()
	go func() {
		defer writers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.Drain()
		}
	}()
	for range 2000 {
		for _, l := range s.Snapshot(state.Downloads).DlLinks {
			if l.ID == fmt.Sprint(linkA) && l.BytesLoaded != l.BytesTotal {
				t.Fatalf("torn link: %+v", l)
			}
		}
		s.Totals()
		s.Snapshot(state.Linkgrabber)
	}
	close(stop)
	writers.Wait()
}

func TestMarkStale(t *testing.T) {
	s := loaded(t)
	if s.Stale() || s.Snapshot(state.Downloads).Stale {
		t.Error("stale from the start")
	}
	s.MarkStale()
	if !s.Stale() {
		t.Error("not stale")
	}
	snap := s.Snapshot(state.Downloads)
	if !snap.Stale || !s.Snapshot(state.Linkgrabber).Stale {
		t.Errorf("snapshot not marked: %+v", snap)
	}
	for _, l := range snap.DlLinks {
		if l.Running || l.Speed != 0 || l.ETA != state.ETAUnknown || l.State == state.LinkRunning {
			t.Errorf("stale link = %+v", l)
		}
	}
	for _, p := range snap.DlPackages {
		if p.Running || p.Speed != 0 || p.ETA != state.ETAUnknown {
			t.Errorf("stale package = %+v", p)
		}
	}
	if l := snap.DlLinks[0]; l.Name != "a.bin" || l.BytesLoaded != 50 {
		t.Errorf("last known values lost: %+v", l)
	}
	dl, _ := s.Totals()
	if dl.Running != 0 || dl.ETA != state.ETAUnknown {
		t.Errorf("totals = %+v", dl)
	}
	// Browsers learn it through the normal patches.
	f := onePatch(t, s).Links[fmt.Sprint(linkA)]
	if f["running"] != false || f["speed"] != int64(0) || f["eta"] != state.ETAUnknown || f["state"] != state.LinkWaiting {
		t.Errorf("fields = %+v", f)
	}

	// A full load makes that list fresh again.
	s.ReplaceDownloads(nil, nil)
	if s.Snapshot(state.Downloads).Stale {
		t.Error("downloads still stale after replace")
	}
	if !s.Stale() {
		t.Error("the linkgrabber was not reloaded yet")
	}
	s.ReplaceLinkgrabber(nil, nil)
	if s.Stale() {
		t.Error("still stale after both lists were replaced")
	}
}

func TestMarkStaleClearsRunningStatusText(t *testing.T) {
	s := state.New()
	s.ReplaceDownloads(
		[]jd.DownloadPackage{
			{UUID: 1, Running: true, Enabled: true, Status: "Running"},
			{UUID: 2, Finished: true, Enabled: true, Status: "Finished"},
		},
		[]jd.DownloadLink{
			{UUID: 10, PackageUUID: 1, Running: true, Enabled: true, Status: "Download"},
			{UUID: 11, PackageUUID: 1, Running: true, Enabled: true, Status: "Starting..."},
			{UUID: 12, PackageUUID: 2, Finished: true, Enabled: true, Status: "Finished"},
			{UUID: 13, PackageUUID: 1, Skipped: true, Enabled: true, Status: "Skipped - Disk is full"},
			{UUID: 14, PackageUUID: 1, Running: true, Finished: true, Enabled: true, Status: "Extracting"},
		},
	)
	s.Drain()
	s.MarkStale()
	snap := s.Snapshot(state.Downloads)
	want := []string{"", "", "Finished", "Skipped - Disk is full", "Extracting"}
	for i, l := range snap.DlLinks {
		if l.Status != want[i] {
			t.Errorf("link %s: status = %q, want %q", l.ID, l.Status, want[i])
		}
	}
	if snap.DlPackages[0].Status != "" || snap.DlPackages[1].Status != "Finished" {
		t.Errorf("packages = %+v", snap.DlPackages)
	}
	if f := s.Drain()[0].Links["10"]; f["status"] != "" {
		t.Errorf("patch = %+v", f)
	}
}

func TestClear(t *testing.T) {
	s := loaded(t)
	s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":1,"uuid":%d}`, linkA)))
	s.MarkStale()
	s.Clear()
	if d := s.Snapshot(state.Downloads); len(d.DlLinks) != 0 || len(d.DlPackages) != 0 || d.Stale {
		t.Errorf("downloads = %+v", d)
	}
	if d := s.Snapshot(state.Linkgrabber); len(d.LgLinks) != 0 || len(d.LgPackages) != 0 {
		t.Errorf("linkgrabber = %+v", d)
	}
	if p := s.Drain(); len(p) != 0 {
		t.Errorf("patches = %+v", p)
	}
	if eff := s.ApplyEvent(dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"speed":1,"uuid":%d}`, linkA))); eff.Reload != state.Downloads {
		t.Errorf("effect = %+v", eff)
	}
}

func TestHostileNamesStayText(t *testing.T) {
	for name, value := range map[string]string{
		"script": `<script>alert(1)</script>`,
		"long":   strings.Repeat("x", 500),
		"rtl":    "‮gnp.exe",
		"emoji":  "🎉📦",
		"quotes": `"},"x":{"`,
		"lines":  "a\r\n\r\nevent: status\ndata: 1",
	} {
		t.Run(name, func(t *testing.T) {
			s := loaded(t)
			data, _ := json.Marshal(map[string]any{"uuid": linkA, "name": value})
			s.ApplyEvent(dlEvent("LINK_UPDATE.name", string(data)))
			if got := link(t, s, linkA).Name; got != value {
				t.Errorf("name = %q", got)
			}
			b, err := json.Marshal(onePatch(t, s))
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(string(b), "\r\n") {
				t.Errorf("raw line break in a patch, it would end the SSE frame: %q", b)
			}
			var back state.Patch
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if got := back.Links[fmt.Sprint(linkA)]["name"]; got != value {
				t.Errorf("round trip = %q", got)
			}
		})
	}
}

func TestControllerNormalized(t *testing.T) {
	for raw, want := range map[jd.ControllerState]string{
		jd.ControllerIdle: "IDLE", jd.ControllerStopped: "IDLE", jd.ControllerRunning: "RUNNING",
		jd.ControllerPause: "PAUSE", jd.ControllerStopping: "STOPPING", "BRAND_NEW": "IDLE", "": "IDLE",
	} {
		if got := state.NormalizeController(raw); got != want {
			t.Errorf("%q = %q, want %q", raw, got, want)
		}
	}
}

func big(n int) ([]jd.DownloadPackage, []jd.DownloadLink) {
	pkgs := make([]jd.DownloadPackage, 0, n/20+1)
	links := make([]jd.DownloadLink, 0, n)
	base := int64(1793562910000)
	for i := range n {
		pkg := base + int64(n) + int64(i/20)
		if i%20 == 0 {
			pkgs = append(pkgs, jd.DownloadPackage{UUID: pkg, Name: fmt.Sprintf("package %d", i/20), Hosts: []string{"files.example"}, SaveTo: "/output/some/quite/long/directory/name", Enabled: true})
		}
		links = append(links, jd.DownloadLink{
			UUID: base + int64(i), PackageUUID: pkg, Name: fmt.Sprintf("a-rather-usual-file-name-with-a-suffix-%06d.part01.rar", i),
			Host: "files.example", URL: fmt.Sprintf("https://files.example/download/abcdefghijklmnop/%06d", i),
			BytesTotal: 1 << 30, BytesLoaded: int64(i), Enabled: true, Running: i%10 == 0, Speed: 1000, ETA: 100, Status: "Download", Priority: jd.PriorityDefault,
		})
	}
	return pkgs, links
}

func heap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// The whole app has to stay below 150 MB with 10,000 links (Concept 10.8). The
// mirror and a snapshot with pending patches are a small part of that.
func TestMemoryWith10000Links(t *testing.T) {
	before := heap()
	s := state.New()
	s.ReplaceDownloads(big(10000))
	for i := range 10000 {
		s.ApplyEvent(dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":%d,"uuid":%d}`, i+7, int64(1793562910000)+int64(i))))
	}
	snap := s.Snapshot(state.Downloads)
	after := heap()
	used := int64(after) - int64(before)
	t.Logf("mirror, one snapshot and one pending patch per link: %.1f MB", float64(used)/(1<<20))
	if used > 30<<20 {
		t.Errorf("uses %d MB", used>>20)
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(snap)
}

// BenchmarkApplyAndDrain is one 500 ms batch with progress on all 10,000 links:
// three deltas per link, then the drain.
func BenchmarkApplyAndDrain(b *testing.B) {
	pkgs, links := big(10000)
	s := state.New()
	s.ReplaceDownloads(pkgs, links)
	s.Drain()
	events := make([][3]jd.Event, len(links))
	for i, l := range links {
		events[i] = [3]jd.Event{
			dlEvent("LINK_UPDATE.bytesLoaded", fmt.Sprintf(`{"bytesLoaded":%d,"uuid":%d}`, l.BytesLoaded+1, l.UUID)),
			dlEvent("LINK_UPDATE.speed", fmt.Sprintf(`{"uuid":%d,"speed":%d}`, l.UUID, l.Speed+1)),
			dlEvent("LINK_UPDATE.eta", fmt.Sprintf(`{"eta":%d,"uuid":%d}`, l.ETA+1, l.UUID)),
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for i := range events {
			s.ApplyEvent(events[i][0])
			s.ApplyEvent(events[i][1])
			s.ApplyEvent(events[i][2])
		}
		if p := s.Drain(); len(p) != 1 {
			b.Fatalf("patches = %d", len(p))
		}
		// Back to the start, so the next round changes every value again.
		b.StopTimer()
		s.ReplaceDownloads(pkgs, links)
		s.Drain()
		b.StartTimer()
	}
}

func BenchmarkSnapshot10000(b *testing.B) {
	pkgs, links := big(10000)
	s := state.New()
	s.ReplaceDownloads(pkgs, links)
	b.ReportAllocs()
	for b.Loop() {
		if len(s.Snapshot(state.Downloads).DlLinks) != 10000 {
			b.Fatal("links lost")
		}
	}
}

func BenchmarkReplace10000(b *testing.B) {
	pkgs, links := big(10000)
	s := state.New()
	s.ReplaceDownloads(pkgs, links)
	b.ReportAllocs()
	for b.Loop() {
		s.ReplaceDownloads(pkgs, links)
	}
}

// Recorded from the test JDownloader (revision 50639): one link that finished, one that
// went offline while its download started. The text of status says "File not found"; the
// machine readable verdict is advancedStatus.FinalLinkState.
func TestRecordedFailedLink(t *testing.T) {
	raw, err := os.ReadFile("../jd/testdata/dl_query_links_failed.json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Data []jd.DownloadLink `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Data) != 2 {
		t.Fatalf("%d links in the fixture", len(res.Data))
	}
	s := state.New()
	s.ReplaceDownloads([]jd.DownloadPackage{{UUID: res.Data[0].PackageUUID, Name: "a"}, {UUID: res.Data[1].PackageUUID, Name: "b"}}, res.Data)
	got := map[string]state.DlLink{}
	for _, l := range s.Snapshot(state.Downloads).DlLinks {
		got[l.Name] = l
	}
	if l := got["ok-rec2.bin"]; l.State != state.LinkFinished || l.FinalState != "FINISHED" {
		t.Errorf("finished link = %+v", l)
	}
	if l := got["410-rec.bin"]; l.State != state.LinkFailed || l.FinalState != "OFFLINE" || l.Finished {
		t.Errorf("offline link = %+v", l)
	}

	// Starting the link again takes the verdict back, without waiting for the next reload.
	failed := got["410-rec.bin"]
	id, _ := strconv.ParseInt(failed.ID, 10, 64)
	s.ApplyEvent(dlEvent("LINK_UPDATE.running", fmt.Sprintf(`{"running":true,"uuid":%d}`, id)))
	if l := link(t, s, id); l.State != state.LinkRunning || l.FinalState != "" {
		t.Errorf("after a restart = %+v", l)
	}
}
