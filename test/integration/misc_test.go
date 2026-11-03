//go:build integration

package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Wire-order methods that had never run against a real JDownloader: check
// online, split by hoster, in both lists.
func TestWire_CheckOnlineAndSplit(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	g := a.add(t, prefix+"wire1", directory("wire1"), fx("/it-wire1-a.bin"), fx("/offline/it-wire1-off.bin"))
	a.action(t, "linkgrabber", "check-online", sel(lgLinkIDs(g), nil))
	eventually(t, "availability after the check", 60*time.Second, func() bool {
		l := a.grabber(t)
		on, _ := l.link("it-wire1-a.bin")
		off, _ := l.link("it-wire1-off.bin")
		return on.Availability == "ONLINE" && off.Availability == "OFFLINE"
	})
	// Split by hoster in the linkgrabber: both links are on one host name, the package may stay as it is.
	a.action(t, "linkgrabber", "split-by-hoster", sel(nil, lgPackageIDs(a.grabber(t))))
	t.Logf("wire: linkgrabber split-by-hoster ok; packages afterwards: %d", len(a.grabber(t).Packages))
	// Move on, then the same in the download list.
	g = a.grabber(t)
	t.Logf("wire: grabber before to-downloads: %d packages, %d links", len(g.Packages), len(g.Links))
	a.action(t, "linkgrabber", "to-downloads", map[string]any{"start": false, "packageIds": lgPackageIDs(g)})
	time.Sleep(8 * time.Second)
	t.Logf("wire: after 8 s: downloads %d links, grabber %d links, dialogs %d, crawling %v", len(a.downloads(t).Links), len(a.grabber(t).Links), a.status(t).Dialogs, a.status(t).Linkgrabber.Crawling)
	for _, k := range a.grabber(t).Links {
		t.Logf("wire: grabber link %s availability=%s enabled=%v", k.Name, k.Availability, k.Enabled)
	}
	if len(a.downloads(t).Links) == 0 {
		t.Log("wire: JDownloader moved nothing from a package with an offline link; trying the online link alone")
		on, _ := a.grabber(t).link("it-wire1-a.bin")
		a.action(t, "linkgrabber", "to-downloads", map[string]any{"start": false, "linkIds": []string{on.ID}})
	}
	eventually(t, "links in downloads", 30*time.Second, func() bool { return len(a.downloads(t).Links) >= 1 })
	t.Logf("wire: downloads after to-downloads: %d links, grabber still %d", len(a.downloads(t).Links), len(a.grabber(t).Links))
	d := a.downloads(t)
	a.action(t, "downloads", "check-online", sel(dlLinkIDs(d), nil))
	a.action(t, "downloads", "split-by-hoster", sel(nil, dlPackageIDs(d)))
	time.Sleep(2 * time.Second)
	if n := len(a.downloads(t).Links); n != len(d.Links) {
		t.Errorf("links after the split: %d, before %d", n, len(d.Links))
	}
	if got := a.status(t).Connection; got != "connected" {
		t.Errorf("connection %s", got)
	}
}

// T-LG-06: YouTube link (external traffic, for crawling only): variants, change and copy.
func TestLG06_Variants(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	a.must(t, "POST", "/api/v1/linkgrabber/add", map[string]any{"links": []string{"https://www.youtube.com/watch?v=jNQXAC9IVRw"}, "packageName": prefix + "lg06", "autostart": false}, nil, 200)
	var link struct {
		ID       string `json:"id"`
		Variants bool   `json:"variants"`
	}
	ok := false
	for deadline := time.Now().Add(120 * time.Second); time.Now().Before(deadline) && !ok; time.Sleep(2 * time.Second) {
		for _, k := range a.grabber(t).Links {
			if k.Variants {
				link.ID, link.Variants, ok = k.ID, true, true
			}
		}
	}
	if !ok {
		var names []string
		for _, k := range a.grabber(t).Links {
			names = append(names, k.Name)
		}
		t.Skipf("T-LG-06: no link with variants within 120 s (internet or YouTube plugin not available): links %v", names)
	}
	var vs struct {
		Variants []struct{ ID, Name, IconKey string } `json:"variants"`
	}
	a.must(t, "GET", "/api/v1/linkgrabber/variants/"+link.ID, nil, &vs, 200)
	t.Logf("T-LG-06 %d variants, first %v", len(vs.Variants), vs.Variants[:1])
	if len(vs.Variants) < 2 {
		t.Fatalf("variants = %+v", vs)
	}
	cur, _ := a.grabberLinkByID(t, link.ID)
	other := vs.Variants[len(vs.Variants)-1].ID
	if cur.Variant != nil && cur.Variant.ID == other {
		other = vs.Variants[0].ID
	}
	a.action(t, "linkgrabber", "set-variant", map[string]any{"linkId": link.ID, "variantId": other})
	eventually(t, "the variant changed", 30*time.Second, func() bool {
		k, ok := a.grabberLinkByID(t, link.ID)
		return ok && k.Variant != nil && k.Variant.ID == other
	})
	before := len(a.grabber(t).Links)
	a.action(t, "linkgrabber", "add-variant", map[string]any{"linkId": link.ID, "variantId": vs.Variants[0].ID})
	eventually(t, "the copy", 30*time.Second, func() bool { return len(a.grabber(t).Links) == before+1 })
	t.Logf("T-LG-06 set-variant and add-variant worked; links %d -> %d", before, len(a.grabber(t).Links))
}

func (a *app) grabberLinkByID(t testing.TB, id string) (struct {
	Variant *struct{ ID string }
}, bool) {
	var out struct {
		Variant *struct{ ID string }
	}
	for _, k := range a.grabber(t).Links {
		if k.ID == id {
			if k.Variant != nil {
				out.Variant = &struct{ ID string }{k.Variant.ID}
			}
			return out, true
		}
	}
	return out, false
}

// Icons: hoster and file icons are proxied and cached; names that could leave the path are refused.
func TestIcons(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	get := func(path string) (*http.Response, []byte) {
		resp, err := a.http.Get(a.base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}
	for _, p := range []string{"/api/v1/icons/hoster/1fichier.com", "/api/v1/icons/file/zip", "/api/v1/icons/hoster/http%20links"} {
		t0 := time.Now()
		r, b := get(p)
		d1 := time.Since(t0)
		t0 = time.Now()
		r2, b2 := get(p)
		t.Logf("icons %s: %d %s %d bytes (%v), cached %d (%v) Cache-Control %q", p, r.StatusCode, r.Header.Get("Content-Type"), len(b), d1.Round(time.Millisecond), r2.StatusCode, time.Since(t0).Round(time.Millisecond), r.Header.Get("Cache-Control"))
		if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") || len(b) == 0 || len(b) != len(b2) {
			t.Errorf("%s: %d %s", p, r.StatusCode, r.Header.Get("Content-Type"))
		}
		if !strings.Contains(r.Header.Get("Cache-Control"), "max-age=86400") {
			t.Errorf("cache control %q", r.Header.Get("Cache-Control"))
		}
	}
	for _, p := range []string{"/api/v1/icons/hoster/..%2f..%2fetc", "/api/v1/icons/file/a%00b", "/api/v1/icons/hoster/no-such-host-at-all.invalid"} {
		r, b := get(p)
		t.Logf("icons %s: %d %s %d bytes", p, r.StatusCode, r.Header.Get("Content-Type"), len(b))
		// JDownloader answers an unknown name with its default image: that is an image, never the content of a path.
		if r.StatusCode == 200 && !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
			t.Errorf("%s = 200 %s", p, r.Header.Get("Content-Type"))
		}
		if r.StatusCode >= 500 {
			t.Errorf("%s = %d", p, r.StatusCode)
		}
	}
	// Without a session.
	anon := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := anon.Get(a.base + "/api/v1/icons/hoster/1fichier.com")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("icon without a session = %d", resp.StatusCode)
	}
}

// T-SYS-01 (part): system info, update check; the restart is in TestSYN04.
func TestSYS01_InfoAndUpdate(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	var sys map[string]any
	a.must(t, "GET", "/api/v1/system", nil, &sys, 200)
	t.Logf("T-SYS-01 system: revision=%v docker=%v headless=%v os=%v java=%v heapUsed=%v startedAt=%v", sys["revision"], sys["docker"], sys["headless"], sys["osFamily"], sys["javaVersion"], sys["heapUsed"], sys["startedAt"])
	if sys["docker"] != true || sys["revision"] == nil {
		t.Errorf("system = %v", sys)
	}
	a.must(t, "POST", "/api/v1/update/check", nil, nil, 204)
	var up struct {
		Available bool `json:"available"`
	}
	a.must(t, "GET", "/api/v1/update", nil, &up, 200)
	t.Logf("T-SYS-01 update available: %v", up.Available)
	// T-SYS-02: in Docker the operating system actions are not offered.
	for _, act := range []string{"shutdown", "hibernate", "standby"} {
		st, b := a.call("POST", "/api/v1/system/"+act, nil)
		if st != 403 || errorCode(b) != "forbidden" {
			t.Errorf("%s = %d %s", act, st, clip(b))
		}
	}
}
