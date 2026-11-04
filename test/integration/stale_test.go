//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// staleID is an id JDownloader does not know: its ids count up from small numbers.
const staleID = 999999999

// What JDownloader does with a destructive call whose ids are all stale. The
// documentation says an empty SELECTED list means everything, so a list of ids
// that no longer exist could do the same. Each case adds a bystander to the
// download list and to the linkgrabber, sends the call to JDownloader directly
// and asks whether the bystanders are still there.
func TestStaleIDs_JDownloaderKeepsTheBystanders(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	type call struct {
		name string
		do   func(l jd.ListOps) error
	}
	calls := []call{
		{"cleanup DELETE_ALL SELECTED, unknown link id", func(l jd.ListOps) error {
			return l.Cleanup(bg(), []int64{staleID}, nil, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionSelected)
		}},
		{"cleanup DELETE_ALL SELECTED, unknown package id", func(l jd.ListOps) error {
			return l.Cleanup(bg(), nil, []int64{staleID}, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionSelected)
		}},
		{"cleanup DELETE_ALL SELECTED, unknown link and package id", func(l jd.ListOps) error {
			return l.Cleanup(bg(), []int64{staleID}, []int64{staleID + 1}, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionSelected)
		}},
		{"cleanup DELETE_FINISHED SELECTED, unknown link id", func(l jd.ListOps) error {
			return l.Cleanup(bg(), []int64{staleID}, nil, jd.CleanupDeleteFinished, jd.CleanupRemoveLinksOnly, jd.SelectionSelected)
		}},
		{"remove, unknown link id", func(l jd.ListOps) error { return l.Remove(bg(), []int64{staleID}, nil) }},
		{"remove, unknown package id", func(l jd.ListOps) error { return l.Remove(bg(), nil, []int64{staleID}) }},
		{"set-enabled false, unknown link id", func(l jd.ListOps) error { return l.SetEnabled(bg(), false, []int64{staleID}, nil) }},
	}
	for i, k := range calls {
		for _, list := range []string{"downloads", "linkgrabber"} {
			wipe(t, c)
			waitFor(t, "an empty mirror", 20*time.Second, func() bool {
				return len(a.downloads(t).Links) == 0 && len(a.grabber(t).Links) == 0
			})
			tag := "stale" + string(rune('a'+i)) + list[:1]
			name := "it-" + tag + ".bin"
			var l jd.ListOps
			if list == "downloads" {
				a.stage(t, prefix+tag, directory(tag), fx("/"+name))
				l = c.Downloads()
			} else {
				a.add(t, prefix+tag, directory(tag), fx("/"+name))
				l = c.Linkgrabber()
			}
			err := k.do(l)
			time.Sleep(2 * time.Second)
			kept := false
			if list == "downloads" {
				_, kept = a.downloads(t).link(name)
			} else {
				_, kept = a.grabber(t).link(name)
			}
			t.Logf("EXPERIMENT %-60s %-11s call error: %v, bystander kept: %v", k.name, list, err, kept)
			if !kept {
				t.Errorf("%s on %s: the bystander is gone", k.name, list)
			}
		}
	}
}

// JDWeb answers a cleanup of a selection that is gone with 409 selection_gone and
// does not call JDownloader's cleanup. The bystander stays. The same cleanup with
// one id that still exists reaches JDownloader and removes exactly that one.
func TestStaleIDs_CleanupAnswersSelectionGone(t *testing.T) {
	c := cleanJD(t)
	a := shared(t)
	for _, list := range []string{"downloads", "linkgrabber"} {
		wipe(t, c)
		waitFor(t, "an empty mirror", 20*time.Second, func() bool {
			return len(a.downloads(t).Links) == 0 && len(a.grabber(t).Links) == 0
		})
		tag := "stalegone-" + list[:1]
		keep, drop := "it-"+tag+"-keep.bin", "it-"+tag+"-drop.bin"
		var dropID string
		if list == "downloads" {
			l := a.stage(t, prefix+tag, directory(tag), fx("/"+keep), fx("/"+drop))
			d, _ := l.link(drop)
			dropID = d.ID
		} else {
			l := a.add(t, prefix+tag, directory(tag), fx("/"+keep), fx("/"+drop))
			d, _ := l.link(drop)
			dropID = d.ID
		}
		body := with(sel([]string{"999999999"}, []string{"999999998"}), "action", "DELETE_ALL", "mode", "REMOVE_LINKS_ONLY", "selection", "SELECTED")
		raw := a.must(t, "POST", "/api/v1/"+list+"/actions/cleanup", body, nil, 409)
		if !strings.Contains(string(raw), `"selection_gone"`) {
			t.Errorf("%s: answer %s, want the code selection_gone", list, raw)
		}
		count := func() int {
			if list == "downloads" {
				return len(a.downloads(t).Links)
			}
			return len(a.grabber(t).Links)
		}
		time.Sleep(time.Second)
		if n := count(); n != 2 {
			t.Errorf("%s: %d links after the stale cleanup, want 2", list, n)
		}
		// One id that exists next to a stale one: only that one goes.
		a.action(t, list, "cleanup", with(sel([]string{dropID, "999999999"}, nil), "action", "DELETE_ALL", "mode", "REMOVE_LINKS_ONLY", "selection", "SELECTED"))
		waitFor(t, list+": only the existing link is removed", 20*time.Second, func() bool {
			if count() != 1 {
				return false
			}
			if list == "downloads" {
				_, ok := a.downloads(t).link(keep)
				return ok
			}
			_, ok := a.grabber(t).link(keep)
			return ok
		})
	}
}
