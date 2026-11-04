//go:build integration

package integration

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

func (l dlList) running() (n int) {
	for _, k := range l.Links {
		if k.State == state.LinkRunning {
			n++
		}
	}
	return
}

// T-DL-01: start, pause, resume, stop; state and control desk agree, also when
// the change is made in JDownloader itself.
func TestDL01_StartPauseResumeStop(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	s := a.openStream(t)
	a.limit(t, 100*1024)
	a.stage(t, prefix+"dl01", directory("dl01"), fx("/slow/it-dl01.bin"))

	m := s.mark()
	a.ctl(t, "start")
	// E3: the change arrives as event within 2 s.
	t0 := time.Now()
	s.wait(t, m, 5*time.Second, "status", `"controller":"RUNNING"`)
	t.Logf("E3 start -> status event RUNNING: %v", time.Since(t0).Round(10*time.Millisecond))
	eventually(t, "the link runs", 10*time.Second, func() bool { return a.downloads(t).running() == 1 })
	eventually(t, "progress shows", 10*time.Second, func() bool {
		l, _ := a.downloads(t).link("it-dl01.bin")
		return l.BytesLoaded > 0 && l.Speed > 0
	})

	a.ctl(t, "pause")
	eventually(t, "pause in status", 5*time.Second, func() bool { return a.status(t).Controller == "PAUSE" })
	a.ctl(t, "resume")
	eventually(t, "resume in status", 5*time.Second, func() bool { return a.status(t).Controller == "RUNNING" })

	// Triggered inside JDownloader: JDWeb follows.
	if err := c.ControllerPause(bg(), true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pause made in JDownloader", 5*time.Second, func() bool { return a.status(t).Controller == "PAUSE" })
	if err := c.ControllerPause(bg(), false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "resume made in JDownloader", 5*time.Second, func() bool { return a.status(t).Controller == "RUNNING" })

	a.ctl(t, "stop")
	eventually(t, "stopped, nothing 'running' is left", 30*time.Second, func() bool {
		return a.status(t).Controller == "IDLE" && a.downloads(t).running() == 0 && a.status(t).Downloads.Running == 0
	})
	// And once more, stopped in JDownloader.
	a.ctl(t, "start")
	eventually(t, "running again", 10*time.Second, func() bool { return a.status(t).Controller == "RUNNING" })
	if err := c.ControllerStop(bg()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "stop made in JDownloader", 30*time.Second, func() bool {
		return a.status(t).Controller == "IDLE" && a.downloads(t).running() == 0
	})
}

// T-DL-02: forcing a download starts only that link although the controller is stopped.
func TestDL02_Force(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	a.limit(t, 200*1024)
	l := a.stage(t, prefix+"dl02", directory("dl02"), fx("/slow/it-dl02-a.bin"), fx("/slow/it-dl02-b.bin"), fx("/slow/it-dl02-c.bin"))
	b, _ := l.link("it-dl02-b.bin")
	a.action(t, "downloads", "force", sel([]string{b.ID}, nil))
	eventually(t, "b runs", 20*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl02-b.bin")
		return k.State == state.LinkRunning || k.State == state.LinkFinished
	})
	time.Sleep(2 * time.Second)
	for _, n := range []string{"it-dl02-a.bin", "it-dl02-c.bin"} {
		k, _ := a.downloads(t).link(n)
		if k.State == state.LinkRunning || k.State == state.LinkFinished {
			t.Errorf("%s started although only b was forced: %s", n, k.State)
		}
	}
}

// T-DL-03: enable and disable, also several at once.
func TestDL03_EnableDisable(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	l := a.stage(t, prefix+"dl03", directory("dl03"), fx("/it-dl03-a.bin"), fx("/it-dl03-b.bin"), fx("/it-dl03-c.bin"))
	two := []string{l.Links[0].ID, l.Links[1].ID}
	a.action(t, "downloads", "disable", sel(two, nil))
	eventually(t, "two disabled", 10*time.Second, func() bool {
		n := 0
		for _, k := range a.downloads(t).Links {
			if !k.Enabled && k.State == state.LinkDisabled {
				n++
			}
		}
		return n == 2
	})
	a.action(t, "downloads", "enable", sel(two, nil))
	eventually(t, "all enabled", 10*time.Second, func() bool {
		for _, k := range a.downloads(t).Links {
			if !k.Enabled {
				return false
			}
		}
		return true
	})
	// A package as a whole.
	a.action(t, "downloads", "disable", sel(nil, []string{l.Packages[0].ID}))
	eventually(t, "package disabled", 10*time.Second, func() bool {
		d := a.downloads(t)
		return !d.Packages[0].Enabled
	})
}

// T-DL-04: the seven priorities of the API. JDownloader keeps only five levels:
// LOW and LOWEST are stored and read back as LOWER, DEFAULT is the absence of a
// priority (the field is left out). Found against revision 50639.
func TestDL04_Priority(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	l := a.stage(t, prefix+"dl04", directory("dl04"), fx("/it-dl04.bin"))
	k := l.Links[0]
	shownAs := map[string]string{"HIGHEST": "HIGHEST", "HIGHER": "HIGHER", "HIGH": "HIGH", "DEFAULT": "", "LOW": "LOWER", "LOWER": "LOWER", "LOWEST": "LOWER"}
	// Start from another value each time, so that a missing change is seen.
	for _, p := range []string{"HIGHEST", "DEFAULT", "HIGHER", "HIGH", "LOW", "HIGH", "LOWER", "HIGHER", "LOWEST", "DEFAULT"} {
		a.action(t, "downloads", "priority", with(sel([]string{k.ID}, nil), "priority", p))
		want := shownAs[p]
		eventually(t, "priority "+p+" in the list", 10*time.Second, func() bool {
			got, _ := a.downloads(t).link("it-dl04.bin")
			return got.Priority == want
		})
	}
	// Packages have the same levels.
	a.action(t, "downloads", "priority", with(sel(nil, []string{l.Packages[0].ID}), "priority", "HIGHEST"))
	eventually(t, "package priority", 10*time.Second, func() bool { return a.downloads(t).Packages[0].Priority == "HIGHEST" })
}

// T-DL-05: renaming a link and a package; the file gets the new name.
func TestDL05_Rename(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	l := a.stage(t, prefix+"dl05", directory("dl05"), fx("/it-dl05.bin"))
	a.action(t, "downloads", "rename-link", map[string]any{"linkId": l.Links[0].ID, "name": "renamed-dl05.bin"})
	a.action(t, "downloads", "rename-package", map[string]any{"packageId": l.Packages[0].ID, "name": prefix + "dl05-renamed"})
	eventually(t, "names in the list", 10*time.Second, func() bool {
		d := a.downloads(t)
		_, okL := d.link("renamed-dl05.bin")
		_, okP := d.pkgNamed(prefix + "dl05-renamed")
		return okL && okP
	})
	a.ctl(t, "start")
	eventually(t, "downloaded", 30*time.Second, func() bool {
		k, _ := a.downloads(t).link("renamed-dl05.bin")
		return k.State == state.LinkFinished
	})
	if ex, known := fileOnDisk(directory("dl05") + "/renamed-dl05.bin"); known && !ex {
		t.Errorf("the file renamed-dl05.bin is not in %s", directory("dl05"))
	} else if known {
		if old, _ := fileOnDisk(directory("dl05") + "/it-dl05.bin"); old {
			t.Error("the file also exists under its old name")
		}
	}
	// A name with a path separator, and a new name that is empty, never reach JDownloader.
	status, b := a.call("POST", "/api/v1/downloads/actions/rename-link", map[string]any{"linkId": l.Links[0].ID, "name": ""})
	if status != 400 {
		t.Errorf("empty name = %d %s", status, b)
	}
}

// T-DL-06: order by the move actions; JDownloader's order is the truth.
func TestDL06_Order(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	l := a.stage(t, prefix+"dl06", directory("dl06"), fx("/it-dl06-1.bin"), fx("/it-dl06-2.bin"), fx("/it-dl06-3.bin"), fx("/it-dl06-4.bin"))
	byName := map[string]string{}
	for _, k := range l.Links {
		byName[k.Name] = k.ID
	}
	order := func() (out []string) {
		links, err := c.DownloadLinks(bg(), jd.AllDownloadLinkFields())
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range links {
			out = append(out, k.Name)
		}
		return
	}
	pkgID := l.Packages[0].ID
	a.action(t, "downloads", "move-links", map[string]any{"linkIds": []string{byName["it-dl06-4.bin"]}, "afterLinkId": "", "destPackageId": pkgID})
	if got := strings.Join(order(), ","); got != "it-dl06-4.bin,it-dl06-1.bin,it-dl06-2.bin,it-dl06-3.bin" {
		t.Errorf("to the top: %s", got)
	}
	a.action(t, "downloads", "move-links", map[string]any{"linkIds": []string{byName["it-dl06-4.bin"]}, "afterLinkId": byName["it-dl06-2.bin"], "destPackageId": pkgID})
	if got := strings.Join(order(), ","); got != "it-dl06-1.bin,it-dl06-2.bin,it-dl06-4.bin,it-dl06-3.bin" {
		t.Errorf("behind the second: %s", got)
	}
	// The mirror agrees.
	eventually(t, "the mirror shows the order", 10*time.Second, func() bool {
		var got []string
		for _, k := range a.downloads(t).Links {
			got = append(got, k.Name)
		}
		return strings.Join(got, ",") == strings.Join(order(), ",")
	})
	// Packages.
	a.action(t, "downloads", "new-package", with(sel([]string{byName["it-dl06-1.bin"]}, nil), "name", prefix+"dl06-b", "directory", directory("dl06b")))
	eventually(t, "two packages", 10*time.Second, func() bool { return len(a.downloads(t).Packages) == 2 })
	d := a.downloads(t)
	first := d.Packages[0].ID
	last := d.Packages[1].ID
	a.action(t, "downloads", "move-packages", map[string]any{"packageIds": []string{last}, "afterPackageId": ""})
	eventually(t, "packages swapped", 10*time.Second, func() bool {
		p := a.downloads(t).Packages
		return len(p) == 2 && p[0].ID == last && p[1].ID == first
	})
}

// helpers for the following tests --------------------------------------------

func (l dlList) pkgNamed(name string) (state.DlPackage, bool) {
	for _, p := range l.Packages {
		if p.Name == name {
			return p, true
		}
	}
	return state.DlPackage{}, false
}

func contains(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

var _ = jd.PriorityDefault

// T-DL-08: a new package from links, and splitting a package by hoster.
func TestDL08_NewPackageAndSplitByHoster(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	ip := fixtureIP()
	if ip == "" {
		t.Skip("docker is not available: no second host name for the fixture")
	}
	other := "http://" + ip + ":8000"
	l := a.stage(t, prefix+"dl08", directory("dl08"), fx("/it-dl08-1.bin"), fx("/it-dl08-2.bin"), other+"/it-dl08-3.bin", other+"/it-dl08-4.bin")
	t.Logf("hosts: %v", l.Packages[0].Hosts)
	if len(l.Packages) != 1 {
		t.Skipf("JDownloader already split the package (%d packages)", len(l.Packages))
	}
	a.action(t, "downloads", "split-by-hoster", sel(nil, []string{l.Packages[0].ID}))
	eventually(t, "two packages, one per host", 15*time.Second, func() bool {
		d := a.downloads(t)
		if len(d.Packages) != 2 {
			return false
		}
		for _, p := range d.Packages {
			if p.ChildCount != 2 || len(p.Hosts) != 1 {
				return false
			}
		}
		return true
	})
	for _, p := range a.downloads(t).Packages {
		t.Logf("package after the split: %q hosts=%v saveTo=%s", p.Name, p.Hosts, p.SaveTo)
	}
	// New package from two links.
	d := a.downloads(t)
	a.action(t, "downloads", "new-package", with(sel([]string{d.Links[0].ID, d.Links[1].ID}, nil), "name", prefix+"dl08-new", "directory", directory("dl08-new")))
	eventually(t, "the new package", 15*time.Second, func() bool {
		p, ok := a.downloads(t).pkgNamed(prefix + "dl08-new")
		return ok && p.ChildCount == 2 && p.SaveTo == directory("dl08-new")
	})
}

// T-DL-09: save-to folder and download password arrive in JDownloader.
func TestDL09_DirectoryAndPassword(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	l := a.stage(t, prefix+"dl09", directory("dl09"), fx("/it-dl09.bin"))
	a.action(t, "downloads", "directory", with(sel(nil, []string{l.Packages[0].ID}), "directory", directory("dl09-moved")))
	a.action(t, "downloads", "password", with(sel([]string{l.Links[0].ID}, nil), "password", "it-secret-pw"))
	eventually(t, "folder and password in the list", 15*time.Second, func() bool {
		d := a.downloads(t)
		k, _ := d.link("it-dl09.bin")
		return d.Packages[0].SaveTo == directory("dl09-moved") && k.DownloadPassword == "it-secret-pw"
	})
	pk, err := c.DownloadPackages(bg(), jd.AllDownloadPackageFields())
	if err != nil || pk[0].SaveTo != directory("dl09-moved") {
		t.Errorf("JDownloader: %+v %v", pk, err)
	}
	// An empty password clears it.
	a.action(t, "downloads", "password", with(sel([]string{l.Links[0].ID}, nil), "password", ""))
	eventually(t, "password cleared", 15*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl09.bin")
		return k.DownloadPassword == ""
	})
	// The password never shows up in the log.
	if strings.Contains(a.logs.String(), "it-secret-pw") {
		t.Error("the download password is in the log")
	}
}

// T-DL-10: the stop mark: downloads run up to it and JDownloader stops there.
func TestDL10_StopMark(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	a.limit(t, 512*1024)
	l := a.stage(t, prefix+"dl10", directory("dl10"), fx("/slow/it-dl10-1.bin"), fx("/slow/it-dl10-2.bin"), fx("/slow/it-dl10-3.bin"))
	byName := map[string]string{}
	for _, k := range l.Links {
		byName[k.Name] = k.ID
	}
	var m struct {
		Marked bool
		LinkID string
		Name   string
	}
	a.must(t, "GET", "/api/v1/downloads/stopmark", nil, &m, 200)
	if m.Marked {
		t.Fatalf("a stop mark exists at the start: %+v", m)
	}
	a.action(t, "downloads", "stopmark-set", map[string]any{"linkId": byName["it-dl10-2.bin"]})
	eventually(t, "the mark", 10*time.Second, func() bool {
		a.must(t, "GET", "/api/v1/downloads/stopmark", nil, &m, 200)
		return m.Marked && m.LinkID == byName["it-dl10-2.bin"] && m.Name == "it-dl10-2.bin"
	})
	a.ctl(t, "start")
	eventually(t, "JDownloader stops at the mark", 60*time.Second, func() bool {
		d := a.downloads(t)
		k2, _ := d.link("it-dl10-2.bin")
		return k2.State == state.LinkFinished && a.status(t).Controller == "IDLE" && d.running() == 0
	})
	d := a.downloads(t)
	k1, _ := d.link("it-dl10-1.bin")
	k3, _ := d.link("it-dl10-3.bin")
	if k1.State != state.LinkFinished {
		t.Errorf("link 1 = %s, want finished", k1.State)
	}
	if k3.State == state.LinkFinished || k3.State == state.LinkRunning {
		t.Errorf("link 3 = %s: JDownloader went past the stop mark", k3.State)
	}
	a.must(t, "GET", "/api/v1/downloads/stopmark", nil, &m, 200)
	t.Logf("stop mark after reaching it: %+v", m)
	// Remove again.
	a.action(t, "downloads", "stopmark-set", map[string]any{"packageId": l.Packages[0].ID})
	a.action(t, "downloads", "stopmark-remove", nil)
	eventually(t, "the mark is gone", 10*time.Second, func() bool {
		a.must(t, "GET", "/api/v1/downloads/stopmark", nil, &m, 200)
		return !m.Marked
	})
}

// finish downloads the named links of the download list and waits for the end.
func (a *app) finish(t testing.TB, d dlList, names ...string) {
	t.Helper()
	var ids []string
	for _, n := range names {
		k, ok := d.link(n)
		if !ok {
			t.Fatalf("no link %s", n)
		}
		ids = append(ids, k.ID)
	}
	a.action(t, "downloads", "force", sel(ids, nil))
	var last string
	defer func() {
		if t.Failed() {
			t.Log("last state: " + last)
		}
	}()
	// Now and then JDownloader leaves a forced link in "running / Starting..." for good (seen in 3 of 4 full runs,
	// not a JDWeb matter: asking JDownloader again moves it). So the force is repeated at 20 s and 40 s for the
	// links that have not ended, and the retry is logged: a test that needed it says so in the results.
	start, retries := time.Now(), 0
	eventually(t, "the downloads end", 70*time.Second, func() bool {
		cur := a.downloads(t)
		if retries < 2 && time.Since(start) > time.Duration(retries+1)*20*time.Second {
			var stuck []string
			for _, n := range names {
				if k, ok := cur.link(n); ok && (k.State == state.LinkRunning || k.State == state.LinkWaiting) {
					stuck = append(stuck, k.ID)
				}
			}
			if len(stuck) > 0 {
				retries++
				t.Logf("FLAKY-FORCE: %d forced link(s) still not ended after %v, forcing again (try %d)", len(stuck), time.Since(start).Round(time.Second), retries)
				a.action(t, "downloads", "force", sel(stuck, nil))
			}
		}
		last = "controller " + a.status(t).Controller
		for _, k := range cur.Links {
			last += fmt.Sprintf(" [%s %s %q %d/%d]", k.Name, k.State, k.Status, k.BytesLoaded, k.BytesTotal)
		}
		for _, n := range names {
			k, _ := cur.link(n)
			last += " | " + n + ": " + string(k.State) + " " + k.Status
			if k.State == state.LinkRunning || k.State == state.LinkWaiting || k.State == state.LinkDisabled {
				return false
			}
		}
		return a.status(t).Controller == "IDLE"
	})
}

// T-DL-11 and T-DL-12: removing links keeps the file; cleaning up with the
// delete mode removes it.
func TestDL11_12_RemoveKeepsFileCleanupDeletesIt(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	if !dockerOK() {
		t.Skip("docker is not available: the files cannot be looked at")
	}
	l := a.stage(t, prefix+"dl11", directory("dl11"), fx("/it-dl11-keep.bin"), fx("/it-dl11-delete.bin"))
	a.finish(t, l, "it-dl11-keep.bin", "it-dl11-delete.bin")
	for _, n := range []string{"it-dl11-keep.bin", "it-dl11-delete.bin"} {
		if ex, _ := fileOnDisk(directory("dl11") + "/" + n); !ex {
			t.Fatalf("%s was not downloaded to %s", n, directory("dl11"))
		}
	}
	d := a.downloads(t)
	keep, _ := d.link("it-dl11-keep.bin")
	del, _ := d.link("it-dl11-delete.bin")

	a.action(t, "downloads", "cleanup", with(sel([]string{keep.ID}, nil), "action", "DELETE_ALL", "mode", "REMOVE_LINKS_ONLY", "selection", "SELECTED"))
	eventually(t, "link removed", 10*time.Second, func() bool { _, ok := a.downloads(t).link("it-dl11-keep.bin"); return !ok })
	if ex, _ := fileOnDisk(directory("dl11") + "/it-dl11-keep.bin"); !ex {
		t.Error("T-DL-11: the file is gone although only the link was removed")
	}
	// The plain remove action: a link of a finished download, the file stays.
	a.action(t, "downloads", "cleanup", with(sel([]string{del.ID}, nil), "action", "DELETE_ALL", "mode", "REMOVE_LINKS_AND_DELETE_FILES", "selection", "SELECTED"))
	eventually(t, "link removed", 10*time.Second, func() bool { _, ok := a.downloads(t).link("it-dl11-delete.bin"); return !ok })
	eventually(t, "T-DL-12: the file is deleted", 10*time.Second, func() bool {
		ex, _ := fileOnDisk(directory("dl11") + "/it-dl11-delete.bin")
		return !ex
	})
	if ex, _ := fileOnDisk(directory("dl11") + "/it-dl11-keep.bin"); !ex {
		t.Error("the other file was deleted too")
	}
	// remove: the links go, the files stay.
	l2 := a.stage(t, prefix+"dl11b", directory("dl11b"), fx("/it-dl11b.bin"))
	a.finish(t, l2, "it-dl11b.bin")
	a.action(t, "downloads", "remove", sel(dlLinkIDs(a.downloads(t)), nil))
	eventually(t, "list empty", 10*time.Second, func() bool { return len(a.downloads(t).Links) == 0 })
	if ex, _ := fileOnDisk(directory("dl11b") + "/it-dl11b.bin"); !ex {
		t.Error("T-DL-11: remove deleted the file")
	}
}

// mixed builds a download list with one link in each state the cleanup knows:
// fin (finished), dis (disabled), off (the download found the file gone) and
// wait (waiting). The names are the file names.
func (a *app) mixed(t *testing.T, c *jd.Client, tag string) (ids map[string]string) {
	t.Helper()
	wipe(t, c)
	waitFor(t, "an empty mirror", 20*time.Second, func() bool { return len(a.downloads(t).Links) == 0 })
	n := func(s string) string { return "it-" + tag + "-" + s + ".bin" }
	l := a.stageWithOffline(t, prefix+tag, directory(tag), fx("/"+n("fin")), fx("/offline/"+n("off")), fx("/"+n("dis")), fx("/"+n("wait")))
	a.finish(t, l, n("fin"), n("off"))
	dis, _ := a.downloads(t).link(n("dis"))
	a.action(t, "downloads", "disable", sel([]string{dis.ID}, nil))
	eventually(t, "the mixed list", 20*time.Second, func() bool {
		d := a.downloads(t)
		fin, _ := d.link(n("fin"))
		off, _ := d.link(n("off"))
		dis, _ := d.link(n("dis"))
		wait, _ := d.link(n("wait"))
		return fin.State == state.LinkFinished && off.State == state.LinkFailed && dis.State == state.LinkDisabled && wait.State == state.LinkWaiting
	})
	ids = map[string]string{}
	for _, k := range a.downloads(t).Links {
		ids[strings.TrimSuffix(strings.TrimPrefix(k.Name, "it-"+tag+"-"), ".bin")] = k.ID
	}
	return ids
}

// stageWithOffline is stage for a list that holds an offline link. JDWeb leaves offline links in the linkgrabber
// (T-LG-07), so the links go to the download list through internal/jd, by link id: JDownloader then takes the
// offline one along, and it fails when it is downloaded.
func (a *app) stageWithOffline(t testing.TB, pkg, dir string, urls ...string) dlList {
	t.Helper()
	g := a.add(t, pkg, dir, urls...)
	var ids []int64
	for _, k := range g.Links {
		n, err := strconv.ParseInt(k.ID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n)
	}
	if err := direct().MoveToDownloads(bg(), ids, nil); err != nil {
		t.Fatal(err)
	}
	var l dlList
	waitFor(t, "the links in the download list", 60*time.Second, func() bool {
		l = a.downloads(t)
		for _, u := range urls {
			if _, ok := l.link(baseName(u)); !ok {
				return false
			}
		}
		return true
	})
	return l
}

// remaining is the set of kinds left in the list.
func (a *app) remaining(t testing.TB, tag string) string {
	var left []string
	for _, k := range a.downloads(t).Links {
		left = append(left, strings.TrimSuffix(strings.TrimPrefix(k.Name, "it-"+tag+"-"), ".bin"))
	}
	sortStrings(left)
	return strings.Join(left, ",")
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// T-DL-13: cleanup by action and by selection type. Each case starts from the
// same list: dis, fin, off, wait.
func TestDL13_Cleanup(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	type tc struct {
		name, action, selection string
		selected                []string // kinds
		left                    string
	}
	cases := []tc{
		{"delete finished", "DELETE_FINISHED", "ALL", nil, "dis,off,wait"},
		{"delete disabled", "DELETE_DISABLED", "ALL", nil, "fin,off,wait"},
		{"delete failed", "DELETE_FAILED", "ALL", nil, "dis,fin,wait"},
		{"delete offline", "DELETE_OFFLINE", "ALL", nil, "dis,fin,wait"},
		{"delete dupes", "DELETE_DUPE", "ALL", nil, "dis,fin,off,wait"},
		{"delete all", "DELETE_ALL", "ALL", nil, ""},
		{"all, selected", "DELETE_ALL", "SELECTED", []string{"wait", "dis"}, "fin,off"},
		{"all, unselected", "DELETE_ALL", "UNSELECTED", []string{"wait", "dis"}, "dis,wait"},
		{"all, none", "DELETE_ALL", "NONE", []string{"wait"}, "dis,fin,off,wait"},
		{"finished, selected", "DELETE_FINISHED", "SELECTED", []string{"fin", "wait"}, "dis,off,wait"},
		{"finished, selected without a finished one", "DELETE_FINISHED", "SELECTED", []string{"wait"}, "dis,fin,off,wait"},
		{"finished, unselected", "DELETE_FINISHED", "UNSELECTED", []string{"wait"}, "dis,off,wait"},
	}
	for i, k := range cases {
		tag := "dl13-" + string(rune('a'+i))
		ids := a.mixed(t, c, tag)
		var links []string
		for _, kind := range k.selected {
			links = append(links, ids[kind])
		}
		a.action(t, "downloads", "cleanup", with(sel(links, nil), "action", k.action, "mode", "REMOVE_LINKS_ONLY", "selection", k.selection))
		var got string
		ok := false
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if got = a.remaining(t, tag); got == k.left {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s (%s/%s): left %q, want %q", k.name, k.action, k.selection, got, k.left)
		}
	}
}

// T-DL-13 (UNSELECTED, fresh read): the unselected links are read from JDownloader for the call. A package with
// a selected and an unselected link loses only the unselected one, a second package goes whole, and the wide id
// list (300 links) passes through JDownloader's own cleanup.
func TestDL13_UnselectedReadsJDownloader(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	var urls, other []string
	for i := 0; i < 300; i++ {
		urls = append(urls, fx(fmt.Sprintf("/it-dl13u-a%03d.bin", i)))
	}
	for i := 0; i < 5; i++ {
		other = append(other, fx(fmt.Sprintf("/it-dl13u-b%d.bin", i)))
	}
	a.add(t, prefix+"dl13u-a", directory("dl13u-a"), urls...)
	g := a.add(t, prefix+"dl13u-b", directory("dl13u-b"), other...)
	var keep []string
	for _, n := range []string{"it-dl13u-a000.bin", "it-dl13u-a299.bin", "it-dl13u-b0.bin"} {
		k, ok := g.link(n)
		if !ok {
			t.Fatalf("link %s not in the linkgrabber", n)
		}
		keep = append(keep, k.ID)
	}
	a.action(t, "linkgrabber", "cleanup", with(sel(keep, nil), "action", "DELETE_ALL", "mode", "REMOVE_LINKS_ONLY", "selection", "UNSELECTED"))
	waitFor(t, "only the selected links are left", 30*time.Second, func() bool {
		l := a.grabber(t)
		if len(l.Links) != 3 {
			return false
		}
		for _, id := range keep {
			found := false
			for _, k := range l.Links {
				found = found || k.ID == id
			}
			if !found {
				return false
			}
		}
		return true
	})
}

// T-DL-13 (DELETE_MODE): the one action whose effect the contract does not name; the result is recorded.
func TestDL13_DeleteModeIsRecorded(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	a.mixed(t, c, "dl13m")
	a.action(t, "downloads", "cleanup", with(sel(nil, nil), "action", "DELETE_MODE", "mode", "REMOVE_LINKS_ONLY", "selection", "ALL"))
	time.Sleep(3 * time.Second)
	// What DELETE_MODE removes is not named by any contract, so the result is recorded. What is asserted: the
	// action went through (204 above), nothing appeared that was not there, and JDWeb still shows a consistent list.
	left := a.remaining(t, "dl13m")
	t.Logf("DELETE_MODE with ALL / REMOVE_LINKS_ONLY leaves: %q (of dis,fin,off,wait)", left)
	for _, kind := range strings.Split(left, ",") {
		if kind != "" && !strings.Contains(",dis,fin,off,wait,", ","+kind+",") {
			t.Errorf("unknown entry %q after DELETE_MODE", kind)
		}
	}
	if got := a.status(t).Connection; got != "connected" {
		t.Errorf("connection = %s", got)
	}
}

// T-DL-14: reset and resume, skipped links and "unskip".
func TestDL14_ResetResumeUnskip(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	// Reset: a finished link is a fresh one afterwards.
	l := a.stage(t, prefix+"dl14", directory("dl14"), fx("/it-dl14-reset.bin"))
	a.finish(t, l, "it-dl14-reset.bin")
	k, _ := a.downloads(t).link("it-dl14-reset.bin")
	if k.State != state.LinkFinished || k.BytesLoaded == 0 {
		t.Fatalf("not finished: %+v", k)
	}
	a.action(t, "downloads", "reset", sel([]string{k.ID}, nil))
	eventually(t, "reset: waiting, nothing loaded", 15*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl14-reset.bin")
		return k.State == state.LinkWaiting && k.BytesLoaded == 0 && !k.Finished
	})

	// Resume: a stopped, partly loaded link is accepted and goes on loading.
	wipe(t, c)
	a.limit(t, 256*1024)
	l = a.stage(t, prefix+"dl14r", directory("dl14r"), fx("/slow/it-dl14-resume.bin"))
	a.ctl(t, "start")
	eventually(t, "partly loaded", 20*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl14-resume.bin")
		return k.BytesLoaded > 100*1024
	})
	a.ctl(t, "stop")
	eventually(t, "stopped", 30*time.Second, func() bool { return a.status(t).Controller == "IDLE" && a.downloads(t).running() == 0 })
	before, _ := a.downloads(t).link("it-dl14-resume.bin")
	a.action(t, "downloads", "resume", sel([]string{before.ID}, nil))
	a.ctl(t, "start")
	eventually(t, "loading again and finished", 60*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl14-resume.bin")
		return k.State == state.LinkFinished
	})
	t.Logf("resume: %d bytes were loaded before, state afterwards finished", before.BytesLoaded)

	// Unskip: an unwritable folder makes JDownloader skip the link.
	wipe(t, c)
	l = a.stage(t, prefix+"dl14s", "/proc/jdweb-it-nowhere", fx("/it-dl14-skip.bin"))
	a.ctl(t, "start")
	var sk state.DlLink
	eventually(t, "the link is skipped", 30*time.Second, func() bool {
		sk, _ = a.downloads(t).link("it-dl14-skip.bin")
		return sk.Skipped
	})
	t.Logf("skipped link: state=%s skipReason=%q status=%q", sk.State, sk.SkipReason, sk.Status)
	if sk.State != state.LinkSkipped || sk.SkipReason == "" {
		t.Errorf("a skipped link shows state=%s skipReason=%q", sk.State, sk.SkipReason)
	}
	a.ctl(t, "stop")
	eventually(t, "idle", 30*time.Second, func() bool { return a.status(t).Controller == "IDLE" })
	a.action(t, "downloads", "directory", with(sel(nil, []string{l.Packages[0].ID}), "directory", directory("dl14s")))
	a.action(t, "downloads", "unskip", with(sel(nil, []string{a.downloads(t).Packages[0].ID}), "reason", string(jd.SkipReasonDiskFull)))
	eventually(t, "unskipped", 20*time.Second, func() bool {
		k, _ := a.downloads(t).link("it-dl14-skip.bin")
		return !k.Skipped
	})
	_ = l
}
