//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

type dialogDTO struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
	Answer     map[string]string `json:"answer"`
}

// T-DLG-01: JDownloader asks and the answer arrives. Trigger: switching the
// premium switch off opens a confirmation dialog (found against revision
// 50639); the request that switched blocks until the dialog is answered.
func TestDLG01_PremiumConfirmation(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	s := a.openStream(t)
	if !a.status(t).Toggles.Premium {
		a.call("POST", "/api/v1/toggles/premium", map[string]any{"enabled": true})
	}
	t.Cleanup(func() { _, _ = direct().TogglePremium(bg()) })
	m := s.mark()
	type res struct {
		status int
		body   []byte
	}
	done := make(chan res, 1)
	go func() {
		st, b := a.call("POST", "/api/v1/toggles/premium", map[string]any{"enabled": false})
		done <- res{st, b}
	}()
	ev := s.wait(t, m, 15*time.Second, "dialog", `"count":1`)
	t.Logf("T-DLG-01 dialog event after %v: %s", ev.At.Sub(time.Now()).Abs().Round(10*time.Millisecond), ev.Data)
	var list []dialogDTO
	a.must(t, "GET", "/api/v1/dialogs", nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("dialogs = %+v", list)
	}
	t.Logf("T-DLG-01 dialog type %s, properties %v", list[0].Type, list[0].Properties)
	var d dialogDTO
	a.must(t, "GET", "/api/v1/dialogs/"+list[0].ID, nil, &d, 200)
	t.Logf("T-DLG-01 answer fields: %v", d.Answer)
	if st := a.status(t); st.Dialogs != 1 {
		t.Errorf("status.dialogs = %d", st.Dialogs)
	}
	// Answering with a field the dialog does not know must not be taken for success unnoticed.
	st, b := a.call("POST", "/api/v1/dialogs/"+list[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "OK"}})
	t.Logf("T-DLG-01 answer OK: %d %s", st, clip(b))
	if st != 204 {
		t.Fatalf("answer = %d", st)
	}
	select {
	case r := <-done:
		t.Logf("T-DLG-01 the toggle request ended with %d %s", r.status, clip(r.body))
	case <-time.After(20 * time.Second):
		t.Error("the switching request does not end after the answer")
	}
	s.wait(t, m, 10*time.Second, "dialog", `"count":0`)
	eventually(t, "premium is off", 10*time.Second, func() bool { return !a.status(t).Toggles.Premium })
	// A second answer: the dialog is gone, a clear 404.
	st, b = a.call("POST", "/api/v1/dialogs/"+list[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "OK"}})
	if st != 404 {
		t.Errorf("second answer = %d %s", st, clip(b))
	}
	if st, _ := a.call("GET", "/api/v1/dialogs/"+list[0].ID, nil); st != 404 {
		t.Errorf("GET of the gone dialog = %d", st)
	}
}

// T-DLG-01 (second trigger): extraction target exists. With IfFileExistsAction
// at ASK_FOR_EACH_FILE the extraction asks per file.
func TestDLG01_FileExists(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	if !dockerOK() {
		t.Skip("docker is needed to look at the extracted files")
	}
	dir := directory("dlg01")
	l := a.stage(t, prefix+"dlg01", dir, fx("/archiv.zip"))
	// JDownloader keeps an extracted archive by its name for a while (a second one of the same name is not
	// recognised for minutes, found here), so this test has an archive name of its own.
	a.action(t, "downloads", "rename-link", map[string]any{"linkId": l.Links[0].ID, "name": "it-dlg-archive.zip"})
	eventually(t, "renamed", 10*time.Second, func() bool { _, ok := a.downloads(t).link("it-dlg-archive.zip"); return ok })
	l = a.downloads(t)
	a.pinArchive(t, l, dir)
	a.downloadFinished(t, l, "it-dlg-archive.zip")
	eventually(t, "first extraction", 30*time.Second, func() bool { return strings.Contains(a.extStatus(t, "it-dlg-archive.zip"), "SUCCESS") })
	var res struct {
		Started map[string]bool `json:"started"`
	}
	a.must(t, "POST", "/api/v1/extraction/start", sel(dlLinkIDs(a.downloads(t)), nil), &res, 200)
	var list []dialogDTO
	eventually(t, "the 'file exists' dialog", 30*time.Second, func() bool {
		a.must(t, "GET", "/api/v1/dialogs", nil, &list, 200)
		return len(list) == 1
	})
	t.Logf("T-DLG-01 second extraction opens: %s %v", list[0].Type, list[0].Properties)
	var d dialogDTO
	a.must(t, "GET", "/api/v1/dialogs/"+list[0].ID, nil, &d, 200)
	t.Logf("T-DLG-01 answer fields: %v", d.Answer)
	a.must(t, "POST", "/api/v1/dialogs/"+list[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "OK", "action": "OVERWRITE_FILE"}}, nil, 204)
	eventually(t, "dialog gone", 15*time.Second, func() bool { return a.status(t).Dialogs == 0 })
	// Further questions come one after the other (docs/api.md): answer until none is left.
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		a.must(t, "GET", "/api/v1/dialogs", nil, &list, 200)
		if len(list) == 0 {
			break
		}
		t.Logf("T-DLG-01 next dialog: %s", list[0].Properties["title"])
		a.must(t, "POST", "/api/v1/dialogs/"+list[0].ID+"/answer", map[string]any{"data": map[string]any{"closereason": "OK", "action": "OVERWRITE_FILE"}}, nil, 204)
	}
	// Overwrite is an answer that ends the extraction cleanly; a cancel leaves the archive in an error state in JDownloader.
	eventually(t, "the extraction ends", 30*time.Second, func() bool {
		return a.extStatus(t, "it-dlg-archive.zip") != "" && a.extStatus(t, "it-dlg-archive.zip") != "RUNNING"
	})
	t.Logf("T-DLG-01 extraction status after answering OVERWRITE_FILE: %q", a.extStatus(t, "it-dlg-archive.zip"))
}
