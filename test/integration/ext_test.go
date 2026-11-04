//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type archiveInfo struct {
	ArchiveID        string            `json:"archiveId"`
	ArchiveName      string            `json:"archiveName"`
	Type             string            `json:"type"`
	ControllerID     json.Number       `json:"controllerId"`
	ControllerStatus string            `json:"controllerStatus"`
	States           map[string]string `json:"states"`
	Settings         struct {
		ArchiveID     string   `json:"archiveId"`
		ExtractPath   string   `json:"extractPath"`
		FinalPassword string   `json:"finalPassword"`
		Passwords     []string `json:"passwords"`
		AutoExtract   bool     `json:"autoExtract"`
	} `json:"settings"`
}

func (a *app) archives(t testing.TB, l dlList) []archiveInfo {
	t.Helper()
	var out []archiveInfo
	a.must(t, "POST", "/api/v1/extraction/archives", sel(dlLinkIDs(l), nil), &out, 200)
	return out
}

// extStatus is what JDWeb shows as extraction status of the link.
func (a *app) extStatus(t testing.TB, name string) string {
	k, _ := a.downloads(t).link(name)
	return k.ExtractionStatus
}

// pinArchive sets the extract path of the archive in l. JDownloader keeps the
// settings of an archive by its id across runs, the extract path of an earlier
// test included (found here: a path that no longer exists ends in ERROR).
func (a *app) pinArchive(t testing.TB, l dlList, extractPath string) {
	t.Helper()
	var arcs []archiveInfo
	t0 := time.Now()
	eventually(t, "JDownloader knows the archive", 30*time.Second, func() bool { arcs = a.archives(t, l); return len(arcs) == 1 })
	t.Logf("archive known after %v", time.Since(t0).Round(time.Second))
	t.Logf("archive %s: stored extract path %q", arcs[0].ArchiveName, arcs[0].Settings.ExtractPath)
	a.must(t, "PUT", "/api/v1/extraction/archives/"+arcs[0].ArchiveID, map[string]any{"extractPath": extractPath}, nil, 204)
}

func (a *app) downloadFinished(t testing.TB, l dlList, name string) {
	t.Helper()
	a.finish(t, l, name)
	k, _ := a.downloads(t).link(name)
	if k.State != "finished" {
		t.Fatalf("%s: state %s (%s), extraction %q", name, k.State, k.Status, k.ExtractionStatus)
	}
}

// T-EXT-01: a ZIP is extracted after the download (JDownloader's auto extract) and when asked.
func TestEXT01_Zip(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	dir := directory("ext01")
	l := a.stage(t, prefix+"ext01", dir, fx("/archiv.zip"))
	a.pinArchive(t, l, dir)
	a.downloadFinished(t, l, "archiv.zip")
	arcs := a.archives(t, a.downloads(t))
	if len(arcs) != 1 {
		t.Fatalf("archives = %+v", arcs)
	}
	t.Logf("T-EXT-01 archive: type=%s autoExtract=%v extractPath=%s states=%v", arcs[0].Type, arcs[0].Settings.AutoExtract, arcs[0].Settings.ExtractPath, arcs[0].States)
	// Auto extract may start the extraction by itself; if no success shows within 20 s, start it now.
	// Either way the test ends only on SUCCESS, so it cannot pass with an archive that was never extracted.
	autoDone := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && !autoDone; time.Sleep(300 * time.Millisecond) {
		autoDone = strings.Contains(a.extStatus(t, "archiv.zip"), "SUCCESS")
	}
	t.Logf("T-EXT-01 status after the download: %q (extracted by itself: %v)", a.extStatus(t, "archiv.zip"), autoDone)
	if !autoDone {
		var res struct {
			Started map[string]bool `json:"started"`
		}
		a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
		t.Logf("T-EXT-01 start now: %v", res.Started)
	}
	eventually(t, "extraction success", 60*time.Second, func() bool { return strings.Contains(a.extStatus(t, "archiv.zip"), "SUCCESS") })
	if ex, known := fileOnDisk(dir + "/hello.txt"); known && !ex {
		t.Error("hello.txt was not extracted")
	}
	if ex, known := fileOnDisk(dir + "/docs/readme.txt"); known && !ex {
		t.Error("docs/readme.txt was not extracted")
	}
}

// T-EXT-02: password protected archive without and with the password.
func TestEXT02_Password(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	dir := directory("ext02")
	// JDownloader adds every password that worked to its global list and tries
	// the list on every archive (found here: "jdweb-pw" was found after one
	// earlier run). Empty it for the test, put it back afterwards.
	const extCfg = "org.jdownloader.extensions.extraction.ExtractionConfig"
	const extStore = "cfg/org.jdownloader.extensions.extraction.ExtractionExtension"
	oldList, err := direct().ConfigGet(bg(), extCfg, extStore, "PasswordList")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var v any
		_ = json.Unmarshal(oldList, &v)
		_ = direct().ConfigSet(bg(), extCfg, extStore, "PasswordList", v)
	})
	if err := direct().ConfigSet(bg(), extCfg, extStore, "PasswordList", []string{}); err != nil {
		t.Fatal(err)
	}
	l := a.stage(t, prefix+"ext02", dir, fx("/archiv-pw.zip"))
	// JDownloader keeps the settings of an archive across runs: start from a wrong password.
	arcs := a.archives(t, l)
	if len(arcs) != 1 {
		t.Fatalf("archives = %+v", arcs)
	}
	id := arcs[0].ArchiveID
	a.must(t, "PUT", "/api/v1/extraction/archives/"+id, map[string]any{"finalPassword": "wrong-pw", "passwords": []string{}, "extractPath": dir}, nil, 204)
	a.downloadFinished(t, l, "archiv-pw.zip")
	var res struct {
		Started map[string]bool `json:"started"`
	}
	a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
	time.Sleep(8 * time.Second)
	wrong := a.extStatus(t, "archiv-pw.zip")
	t.Logf("T-EXT-02 with a wrong password: status %q, dialogs %s", wrong, clip(a.must(t, "GET", "/api/v1/dialogs", nil, nil, 200)))
	if strings.Contains(wrong, "SUCCESS") {
		t.Error("extracted with a wrong password")
	}
	// JDownloader asks for the password in a dialog (a second trigger for T-DLG-01).
	var dlgs []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	a.must(t, "GET", "/api/v1/dialogs", nil, &dlgs, 200)
	if len(dlgs) != 1 || !strings.HasSuffix(dlgs[0].Type, "ExtractPasswordDialogInterface") {
		t.Fatalf("dialogs = %+v", dlgs)
	}
	t.Logf("T-EXT-02 dialog detail: %s", clip(a.must(t, "GET", "/api/v1/dialogs/"+dlgs[0].ID, nil, nil, 200)))
	a.must(t, "POST", "/api/v1/dialogs/"+dlgs[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "CANCEL"}}, nil, 204)
	eventually(t, "the dialog is gone", 10*time.Second, func() bool { return a.status(t).Dialogs == 0 })
	time.Sleep(2 * time.Second)
	t.Logf("T-EXT-02 status after cancelling the dialog: %q", a.extStatus(t, "archiv-pw.zip"))
	// Reveal gives the real value, the listing masks it.
	var rev struct {
		FinalPassword string   `json:"finalPassword"`
		Passwords     []string `json:"passwords"`
	}
	a.must(t, "POST", "/api/v1/extraction/archives/"+id+"/reveal", nil, &rev, 200)
	// JDownloader drops a final password that did not work.
	t.Logf("T-EXT-02 reveal after the failed attempt: %+v", rev)
	// The right one.
	a.must(t, "PUT", "/api/v1/extraction/archives/"+id, map[string]any{"finalPassword": "jdweb-pw"}, nil, 204)
	a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
	eventually(t, "extraction success with the password", 60*time.Second, func() bool { return strings.Contains(a.extStatus(t, "archiv-pw.zip"), "SUCCESS") })
	if ex, known := fileOnDisk(dir + "/hello.txt"); known && !ex {
		t.Error("hello.txt was not extracted")
	}
	// Archive password list: add, then clear with an empty list (T-CFG-05 part).
	a.must(t, "POST", "/api/v1/extraction/passwords", map[string]any{"password": "it-list-pw"}, nil, 204)
	a.must(t, "PUT", "/api/v1/extraction/archives/"+id, map[string]any{"passwords": []string{"it-a", "it-b"}}, nil, 204)
	a.must(t, "POST", "/api/v1/extraction/archives/"+id+"/reveal", nil, &rev, 200)
	t.Logf("T-EXT-02 passwords after the set: %v final %q", rev.Passwords, rev.FinalPassword)
	a.must(t, "PUT", "/api/v1/extraction/archives/"+id, map[string]any{"passwords": []string{}}, nil, 204)
	a.must(t, "POST", "/api/v1/extraction/archives/"+id+"/reveal", nil, &rev, 200)
	if len(rev.Passwords) != 0 {
		t.Errorf("passwords after the empty list: %v", rev.Passwords)
	}
	if rev.FinalPassword != "jdweb-pw" {
		t.Errorf("the final password changed with the list: %q", rev.FinalPassword)
	}
	// The final password set and clear attempt.
	a.must(t, "PUT", "/api/v1/extraction/archives/"+id, map[string]any{"finalPassword": "it-final-pw"}, nil, 204)
	a.must(t, "POST", "/api/v1/extraction/archives/"+id+"/reveal", nil, &rev, 200)
	if rev.FinalPassword != "it-final-pw" {
		t.Errorf("final password after the set: %q", rev.FinalPassword)
	}
	// Found against revision 50639: JDownloader ignores an empty string (and null) and answers success.
	// JDWeb reads back and tells the truth: 409.
	if code, body := a.call("PUT", "/api/v1/extraction/archives/"+id, map[string]any{"finalPassword": ""}); code != 409 || errorCode(body) != "jd_error" {
		t.Errorf("clearing the final password: %d %s, want 409 jd_error", code, clip(body))
	}
	a.must(t, "POST", "/api/v1/extraction/archives/"+id+"/reveal", nil, &rev, 200)
	if rev.FinalPassword != "it-final-pw" {
		t.Errorf("final password after the refused clear: %q", rev.FinalPassword)
	}
}

// T-EXT-03: cancel a running extraction of the big archive, and extract it now.
func TestEXT03_CancelAndNow(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	dir := directory("ext03")
	l := a.stage(t, prefix+"ext03", dir, fx("/archiv-big.zip"))
	a.pinArchive(t, l, dir)
	a.downloadFinished(t, l, "archiv-big.zip")
	var res struct {
		Started map[string]bool `json:"started"`
	}
	a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
	t.Logf("T-EXT-03 started: %v", res.Started)
	var q []archiveInfo
	eventually(t, "the archive in the queue", 30*time.Second, func() bool {
		a.must(t, "GET", "/api/v1/extraction/queue", nil, &q, 200)
		return len(q) > 0 && q[0].ControllerID.String() != "-1"
	})
	t.Logf("T-EXT-03 queue: %+v", q[0])
	a.must(t, "POST", "/api/v1/extraction/cancel", map[string]any{"id": q[0].ControllerID.String()}, nil, 204)
	eventually(t, "queue empty after the cancel", 30*time.Second, func() bool {
		a.must(t, "GET", "/api/v1/extraction/queue", nil, &q, 200)
		return len(q) == 0
	})
	t.Logf("T-EXT-03 status after the cancel: %q", a.extStatus(t, "archiv-big.zip"))
	// A second cancel of a finished controller is a clean 404.
	st, b := a.call("POST", "/api/v1/extraction/cancel", map[string]any{"id": "999999"})
	if st != 404 && st != 204 {
		t.Errorf("cancel of an unknown controller = %d %s", st, clip(b))
	}
	// Extract now, and let it end.
	a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
	eventually(t, "extraction success", 180*time.Second, func() bool { return strings.Contains(a.extStatus(t, "archiv-big.zip"), "SUCCESS") })
}
