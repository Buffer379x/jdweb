package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

// seed puts a package with two links into both lists of the fake.
type seeded struct {
	dlPkg, lgPkg int64
	dl, lg       []int64
}

func (e *env) seed() seeded {
	var s seeded
	s.dlPkg, s.dl = e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "dl-pkg"},
		testjd.Link{Name: "a.bin", BytesTotal: 100}, testjd.Link{Name: "b.bin", BytesTotal: 200})
	s.lgPkg, s.lg = e.jd.AddPackage(testjd.Linkgrabber, testjd.Package{Name: "lg-pkg"},
		testjd.Link{Name: "c.bin", BytesTotal: 300}, testjd.Link{Name: "d.bin", BytesTotal: 400})
	return s
}

func ids(ns ...int64) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = fmt.Sprint(n)
	}
	return out
}

// lastCall returns the last call JDownloader got on a path.
func (e *env) lastCall(path string) testjd.Call {
	e.t.Helper()
	calls := e.jdCalls(path)
	if len(calls) == 0 {
		e.t.Fatalf("no call to %s; calls: %v", path, e.jd.Calls())
	}
	return calls[len(calls)-1]
}

func js(ns ...int64) string { return mustJSONNums(ns) }

func mustJSONNums(ns []int64) string {
	if ns == nil {
		ns = []int64{}
	}
	return strings.ReplaceAll(fmt.Sprint(ns), " ", ",")
}

// The actions of docs/api.md reach the right JDownloader call with the
// parameters in the order JDownloader expects, for both lists.
func TestActionsMapToJDownloaderCalls(t *testing.T) {
	e := newEnv(t)
	s := e.seed()
	dl, lg := s.dl, s.lg
	type tc struct {
		list, action string
		body         map[string]any
		path, params string
	}
	sel := func(l []int64, p int64) map[string]any {
		return map[string]any{"linkIds": ids(l...), "packageIds": ids(p)}
	}
	with := func(m map[string]any, kv ...any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	var cases []tc
	for _, l := range []struct {
		name, ns string
		links    []int64
		pkg      int64
	}{{"downloads", "/downloadsV2", dl, s.dlPkg}, {"linkgrabber", "/linkgrabberv2", lg, s.lgPkg}} {
		L, P := js(l.links...), js(l.pkg)
		sb := sel(l.links, l.pkg)
		cases = append(cases,
			tc{l.name, "enable", sb, l.ns + "/setEnabled", fmt.Sprintf("[true,%s,%s]", L, P)},
			tc{l.name, "disable", sb, l.ns + "/setEnabled", fmt.Sprintf("[false,%s,%s]", L, P)},
			tc{l.name, "remove", sb, l.ns + "/removeLinks", fmt.Sprintf("[%s,%s]", L, P)},
			tc{l.name, "check-online", sb, l.ns + "/startOnlineStatusCheck", fmt.Sprintf("[%s,%s]", L, P)},
			tc{l.name, "split-by-hoster", sb, l.ns + "/splitPackageByHoster", fmt.Sprintf("[%s,%s]", L, P)},
			tc{l.name, "priority", with(sb, "priority", "HIGH"), l.ns + "/setPriority", fmt.Sprintf("[\"HIGH\",%s,%s]", L, P)},
			tc{l.name, "rename-link", map[string]any{"linkId": ids(l.links[0])[0], "name": "new name.bin"}, l.ns + "/renameLink", fmt.Sprintf("[%d,\"new name.bin\"]", l.links[0])},
			tc{l.name, "rename-package", map[string]any{"packageId": ids(l.pkg)[0], "name": "New"}, l.ns + "/renamePackage", fmt.Sprintf("[%d,\"New\"]", l.pkg)},
			tc{l.name, "directory", map[string]any{"packageIds": ids(l.pkg), "directory": "/output/x"}, l.ns + "/setDownloadDirectory", fmt.Sprintf("[\"/output/x\",%s]", P)},
			tc{l.name, "password", with(sb, "password", "s3cret"), l.ns + "/setDownloadPassword", fmt.Sprintf("[%s,%s,\"s3cret\"]", L, P)},
			tc{l.name, "new-package", with(sb, "name", "Fresh", "directory", "/output/f"), l.ns + "/movetoNewPackage", fmt.Sprintf("[%s,%s,\"Fresh\",\"/output/f\"]", L, P)},
			tc{l.name, "move-links", map[string]any{"linkIds": ids(l.links[1]), "afterLinkId": ids(l.links[0])[0], "destPackageId": ids(l.pkg)[0]},
				l.ns + "/moveLinks", fmt.Sprintf("[[%d],%d,%d]", l.links[1], l.links[0], l.pkg)},
			tc{l.name, "move-links", map[string]any{"linkIds": ids(l.links[1]), "destPackageId": ids(l.pkg)[0]},
				l.ns + "/moveLinks", fmt.Sprintf("[[%d],-1,%d]", l.links[1], l.pkg)},
			tc{l.name, "move-packages", map[string]any{"packageIds": ids(l.pkg), "afterPackageId": "-1"}, l.ns + "/movePackages", fmt.Sprintf("[%s,-1]", P)},
			tc{l.name, "cleanup", with(sb, "action", "DELETE_FINISHED", "mode", "REMOVE_LINKS_ONLY", "selection", "SELECTED"),
				l.ns + "/cleanup", fmt.Sprintf("[%s,%s,\"DELETE_FINISHED\",\"REMOVE_LINKS_ONLY\",\"SELECTED\"]", L, P)},
		)
	}
	DL, DP := js(dl...), js(s.dlPkg)
	cases = append(cases,
		tc{"downloads", "force", sel(dl, s.dlPkg), "/downloadsV2/forceDownload", fmt.Sprintf("[%s,%s]", DL, DP)},
		tc{"downloads", "resume", sel(dl, s.dlPkg), "/downloadsV2/resumeLinks", fmt.Sprintf("[%s,%s]", DL, DP)},
		tc{"downloads", "reset", sel(dl, s.dlPkg), "/downloadsV2/resetLinks", fmt.Sprintf("[%s,%s]", DL, DP)},
		tc{"downloads", "unskip", with(sel(dl, s.dlPkg), "reason", "DISK_FULL"), "/downloadsV2/unskip", fmt.Sprintf("[%s,%s,\"DISK_FULL\"]", DP, DL)},
		tc{"downloads", "unskip", sel(dl, s.dlPkg), "/downloadsV2/unskip", fmt.Sprintf("[%s,%s,null]", DP, DL)},
		tc{"downloads", "stopmark-set", map[string]any{"linkId": ids(dl[0])[0]}, "/downloadsV2/setStopMark", fmt.Sprintf("[%d,-1]", dl[0])},
		tc{"downloads", "stopmark-set", map[string]any{"packageId": ids(s.dlPkg)[0]}, "/downloadsV2/setStopMark", fmt.Sprintf("[-1,%d]", s.dlPkg)},
		tc{"downloads", "stopmark-remove", map[string]any{}, "/downloadsV2/removeStopMark", "[]"},
		tc{"linkgrabber", "abort", map[string]any{}, "/linkgrabberv2/abort", "[]"},
		tc{"linkgrabber", "clear", map[string]any{}, "/linkgrabberv2/clearList", "[]"},
		tc{"linkgrabber", "set-variant", map[string]any{"linkId": ids(lg[0])[0], "variantId": "MP4_720"}, "/linkgrabberv2/setVariant", fmt.Sprintf("[%d,\"MP4_720\"]", lg[0])},
		tc{"linkgrabber", "add-variant", map[string]any{"linkId": ids(lg[0])[0], "variantId": "MP3", "afterLinkId": ids(lg[1])[0], "destPackageId": ids(s.lgPkg)[0]},
			"/linkgrabberv2/addVariantCopy", fmt.Sprintf("[%d,%d,%d,\"MP3\"]", lg[0], lg[1], s.lgPkg)},
	)
	for _, c := range cases {
		t.Run(c.list+"/"+c.action, func(t *testing.T) {
			e.jd.ResetCalls()
			r := e.post("/api/v1/"+c.list+"/actions/"+c.action, c.body)
			if r.status != 204 {
				t.Fatalf("status = %d: %s", r.status, r.body)
			}
			calls := e.jdCalls(c.path)
			if len(calls) != 1 {
				t.Fatalf("%d calls to %s, want 1; all: %v", len(calls), c.path, e.jd.Calls())
			}
			if got := argsJSON(calls[0]); got != c.params {
				t.Errorf("params = %s, want %s", got, c.params)
			}
			if calls[0].Origin != "" {
				t.Errorf("the call to JDownloader carries Origin %q", calls[0].Origin)
			}
		})
	}
}

// to-downloads moves, and starts the downloads only on request.
func TestToDownloads(t *testing.T) {
	e := newEnv(t)
	s := e.seed()
	body := map[string]any{"linkIds": ids(s.lg...), "packageIds": ids(s.lgPkg)}
	e.expect(e.post("/api/v1/linkgrabber/actions/to-downloads", body), 204)
	if len(e.jdCalls("/linkgrabberv2/moveToDownloadlist")) != 1 {
		t.Fatalf("calls: %v", e.jd.Calls())
	}
	if len(e.jdCalls("/downloadcontroller/start")) != 0 {
		t.Fatal("downloads were started without start:true")
	}
	body["start"] = true
	e.expect(e.post("/api/v1/linkgrabber/actions/to-downloads", body), 204)
	if len(e.jdCalls("/downloadcontroller/start")) != 1 {
		t.Fatalf("start:true did not start the downloads: %v", e.jd.Calls())
	}
}

func TestURLsAction(t *testing.T) {
	e := newEnv(t)
	_, links := e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "p"},
		testjd.Link{Name: "a", URL: "http://h/a"}, testjd.Link{Name: "b", URL: "http://h/b"})
	r := e.post("/api/v1/downloads/actions/urls", map[string]any{"linkIds": ids(links...)})
	e.expect(r, 200)
	var out struct {
		Links []struct{ ID, URL string }
	}
	r.json(t, &out)
	if len(out.Links) != 2 || out.Links[0].ID != fmt.Sprint(links[0]) || out.Links[0].URL != "http://h/a" || out.Links[1].URL != "http://h/b" {
		t.Fatalf("urls = %+v", out)
	}
	if strings.Contains(string(r.body), fmt.Sprintf(`"id":%d`, links[0])) {
		t.Fatal("ids are numbers in the answer")
	}
}

func TestUnknownAndMisplacedActions(t *testing.T) {
	e := newEnv(t)
	e.seed()
	for _, p := range []string{
		"/api/v1/downloads/actions/nope", "/api/v1/linkgrabber/actions/nope", "/api/v1/downloads/actions/",
		// each list has actions the other does not
		"/api/v1/linkgrabber/actions/force", "/api/v1/linkgrabber/actions/stopmark-set", "/api/v1/linkgrabber/actions/unskip",
		"/api/v1/downloads/actions/to-downloads", "/api/v1/downloads/actions/clear", "/api/v1/downloads/actions/abort",
		"/api/v1/downloads/actions/add-variant", "/api/v1/accounts/actions/nope", "/api/v1/extensions/nope",
		// JDownloader paths are not reachable by name
		"/api/v1/downloads/actions/..%2f..%2fsystem%2fexitJD", "/api/v1/downloads/actions/system/exitJD",
	} {
		r := e.post(p, map[string]any{"linkIds": []string{"1"}})
		if r.status != 404 || r.errCode() != "not_found" {
			t.Errorf("POST %s: %d %s, want 404 not_found", p, r.status, r.body)
		}
	}
	if calls := e.jd.Calls(); len(calls) != 0 {
		t.Errorf("%d calls reached JDownloader: %v", len(calls), calls[0].Path)
	}
}

// Bad input is answered with 400 before JDownloader is called.
func TestActionValidation(t *testing.T) {
	e := newEnv(t)
	s := e.seed()
	one := ids(s.dl[0])
	full := func(extra map[string]any) map[string]any {
		m := map[string]any{"linkIds": one, "packageIds": []string{}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for name, c := range map[string]struct {
		path string
		body any
		want string // part of the message
	}{
		"id is a number":         {"downloads/actions/remove", `{"linkIds":[123]}`, "strings"},
		"id is no number":        {"downloads/actions/remove", `{"linkIds":["abc"]}`, "valid id"},
		"id is zero":             {"downloads/actions/remove", `{"linkIds":["0"]}`, "valid id"},
		"id is negative":         {"downloads/actions/remove", `{"linkIds":["-5"]}`, "valid id"},
		"id has a sign":          {"downloads/actions/remove", `{"linkIds":["+5"]}`, "valid id"},
		"id has a space":         {"downloads/actions/remove", `{"linkIds":[" 5"]}`, "valid id"},
		"id overflows":           {"downloads/actions/remove", `{"linkIds":["99999999999999999999"]}`, "valid id"},
		"id leading zero":        {"downloads/actions/remove", `{"linkIds":["05"]}`, "valid id"},
		"nothing selected":       {"downloads/actions/remove", `{}`, "Select at least one"},
		"empty body":             {"downloads/actions/remove", ``, ""},
		"not json":               {"downloads/actions/remove", `{`, "not valid JSON"},
		"two json values":        {"downloads/actions/remove", `{"linkIds":["1"]}{}`, "exactly one"},
		"priority unknown":       {"downloads/actions/priority", full(map[string]any{"priority": "URGENT"}), "priority must be"},
		"priority missing":       {"downloads/actions/priority", full(nil), "priority must be"},
		"priority lower case":    {"downloads/actions/priority", full(map[string]any{"priority": "high"}), "priority must be"},
		"cleanup without mode":   {"downloads/actions/cleanup", full(map[string]any{"action": "DELETE_ALL", "selection": "ALL"}), "mode is required"},
		"cleanup bad mode":       {"downloads/actions/cleanup", full(map[string]any{"action": "DELETE_ALL", "mode": "REMOVE", "selection": "ALL"}), "mode must be"},
		"cleanup bad action":     {"downloads/actions/cleanup", full(map[string]any{"action": "DELETE", "mode": "REMOVE_LINKS_ONLY", "selection": "ALL"}), "action must be"},
		"cleanup no action":      {"downloads/actions/cleanup", full(map[string]any{"mode": "REMOVE_LINKS_ONLY", "selection": "ALL"}), "action must be"},
		"cleanup bad selection":  {"downloads/actions/cleanup", full(map[string]any{"action": "DELETE_ALL", "mode": "REMOVE_LINKS_ONLY", "selection": "EVERYTHING"}), "selection must be"},
		"cleanup no selection":   {"downloads/actions/cleanup", full(map[string]any{"action": "DELETE_ALL", "mode": "REMOVE_LINKS_ONLY"}), "selection must be"},
		"cleanup selected empty": {"linkgrabber/actions/cleanup", map[string]any{"action": "DELETE_ALL", "mode": "REMOVE_LINKS_ONLY", "selection": "SELECTED"}, "at least one"},
		"rename without name":    {"downloads/actions/rename-link", map[string]any{"linkId": one[0], "name": "  "}, "name is required"},
		"rename without id":      {"downloads/actions/rename-link", map[string]any{"name": "x"}, "linkId is required"},
		"rename name control":    {"downloads/actions/rename-package", map[string]any{"packageId": one[0], "name": "a\x00b"}, "control"},
		"directory missing":      {"downloads/actions/directory", map[string]any{"packageIds": one}, "directory is required"},
		"directory no package":   {"downloads/actions/directory", map[string]any{"directory": "/x"}, "at least one package"},
		"password missing":       {"downloads/actions/password", full(nil), "password is required"},
		"new package no name":    {"downloads/actions/new-package", full(nil), "name is required"},
		"move-links no dest":     {"downloads/actions/move-links", map[string]any{"linkIds": one}, "destPackageId is required"},
		"move-links bad after":   {"downloads/actions/move-links", map[string]any{"linkIds": one, "destPackageId": one[0], "afterLinkId": "x"}, "valid id"},
		"move-packages none":     {"downloads/actions/move-packages", map[string]any{}, "at least one package"},
		"unskip bad reason":      {"downloads/actions/unskip", full(map[string]any{"reason": "BORED"}), "reason must"},
		"stopmark both":          {"downloads/actions/stopmark-set", map[string]any{"linkId": one[0], "packageId": one[0]}, "exactly one"},
		"stopmark neither":       {"downloads/actions/stopmark-set", map[string]any{}, "exactly one"},
		"set-variant no variant": {"linkgrabber/actions/set-variant", map[string]any{"linkId": one[0]}, "variantId is required"},
		"add-variant gone link":  {"linkgrabber/actions/add-variant", map[string]any{"linkId": "777", "variantId": "MP3"}, "no longer"},
	} {
		t.Run(name, func(t *testing.T) {
			r := e.post("/api/v1/"+c.path, c.body)
			if r.status != 400 && !(strings.Contains(name, "gone") && r.status == 404) {
				t.Fatalf("status = %d: %s", r.status, r.body)
			}
			if c.want != "" && !strings.Contains(r.errMessage(), c.want) {
				t.Errorf("message = %q, want it to hold %q", r.errMessage(), c.want)
			}
			for _, call := range e.jd.Calls() {
				t.Fatalf("JDownloader was called (%s) although the request was invalid", call.Path)
			}
		})
	}
}

// The default mode of a cleanup is not assumed: all enums valid is the only way through.
func TestCleanupAllEnumValuesReachJDownloader(t *testing.T) {
	e := newEnv(t)
	s := e.seed()
	for _, a := range []string{"DELETE_ALL", "DELETE_DISABLED", "DELETE_FAILED", "DELETE_FINISHED", "DELETE_OFFLINE", "DELETE_DUPE", "DELETE_MODE"} {
		for _, m := range []string{"REMOVE_LINKS_ONLY", "REMOVE_LINKS_AND_RECYCLE_FILES", "REMOVE_LINKS_AND_DELETE_FILES"} {
			for _, sel := range []string{"SELECTED", "UNSELECTED", "ALL", "NONE"} {
				e.jd.ResetCalls()
				r := e.post("/api/v1/downloads/actions/cleanup", map[string]any{
					"linkIds": ids(s.dl...), "action": a, "mode": m, "selection": sel,
				})
				if r.status != 204 {
					t.Fatalf("%s %s %s: %d %s", a, m, sel, r.status, r.body)
				}
				c := e.lastCall("/downloadsV2/cleanup")
				want := fmt.Sprintf(`"%s","%s","%s"]`, a, m, sel)
				if !strings.HasSuffix(argsJSON(c), want) {
					t.Fatalf("params = %s, want suffix %s", argsJSON(c), want)
				}
			}
		}
	}
}

// Only ALL goes without ids.
func TestCleanupAllNeedsNoIDs(t *testing.T) {
	e := newEnv(t)
	e.expect(e.post("/api/v1/downloads/actions/cleanup", map[string]any{"action": "DELETE_FINISHED", "mode": "REMOVE_LINKS_ONLY", "selection": "ALL"}), 204)
	if got := argsJSON(e.lastCall("/downloadsV2/cleanup")); got != `[[],[],"DELETE_FINISHED","REMOVE_LINKS_ONLY","ALL"]` {
		t.Errorf("params = %s", got)
	}
}

// Review Focus 2: an action on an entry JDownloader dropped answers with
// success, and the list is reloaded so the browser converges.
func TestActionOnVanishedEntryRefreshesTheList(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	s := e.seed()
	e.waitConnected()
	eventually(t, "both links in the mirror", func() bool { return len(e.mirror.Snapshot(state.Downloads).DlLinks) == 2 })

	// JDownloader drops a link on its own, without any event.
	e.jd.RemoveLink(testjd.Downloads, s.dl[0])
	time.Sleep(60 * time.Millisecond)
	if n := len(e.mirror.Snapshot(state.Downloads).DlLinks); n != 2 {
		t.Fatalf("the mirror changed without an event (%d links), the test proves nothing", n)
	}
	r := e.post("/api/v1/downloads/actions/remove", map[string]any{"linkIds": ids(s.dl[0])})
	e.expect(r, 204)
	eventually(t, "the vanished link leaves the mirror", func() bool {
		links := e.mirror.Snapshot(state.Downloads).DlLinks
		return len(links) == 1 && links[0].ID == fmt.Sprint(s.dl[1])
	})
	// And an action on an id that never existed is no error either.
	e.expect(e.post("/api/v1/downloads/actions/disable", map[string]any{"linkIds": []string{"424242"}}), 204)
	e.expect(e.post("/api/v1/downloads/actions/priority", map[string]any{"packageIds": []string{"424242"}, "priority": "HIGH"}), 204)
}

// A captcha or dialog that went away is a clear jd_error, not a crash or a 500.
func TestGoneCaptchaAndDialogGiveUnderstandableErrors(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/captchas/999/solve", map[string]any{"result": "abc"}},
		{"POST", "/api/v1/captchas/999/skip", map[string]any{"type": "SINGLE"}},
		{"GET", "/api/v1/captchas/999", nil},
		{"POST", "/api/v1/dialogs/999/answer", map[string]any{"data": map[string]any{"closereason": "OK"}}},
		{"GET", "/api/v1/dialogs/999", nil},
	} {
		r := e.do(c.method, c.path, c.body)
		if r.status != 404 || r.errCode() != "jd_error" {
			t.Errorf("%s %s: %d %s, want 404 jd_error", c.method, c.path, r.status, r.body)
			continue
		}
		msg := r.errMessage()
		if !strings.Contains(msg, "gone") && !strings.Contains(msg, "no longer") {
			t.Errorf("%s %s: message %q does not say the entry is gone", c.method, c.path, msg)
		}
		for _, bad := range []string{"NOT_AVAILABLE", "Invalid ID", "jd:", "/captcha/", "/dialogs/", "HTTP"} {
			if strings.Contains(msg, bad) {
				t.Errorf("%s %s: message %q leaks %q", c.method, c.path, msg, bad)
			}
		}
	}
}

// Without afterLinkId and destPackageId a variant copy goes behind its original.
func TestAddVariantDefaultsToThePlaceOfTheOriginal(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	s := e.seed()
	e.waitConnected()
	eventually(t, "the linkgrabber is loaded", func() bool { return len(e.mirror.Snapshot(state.Linkgrabber).LgLinks) == 2 })
	e.expect(e.post("/api/v1/linkgrabber/actions/add-variant", map[string]any{"linkId": ids(s.lg[1])[0], "variantId": "MP3"}), 204)
	got := argsJSON(e.lastCall("/linkgrabberv2/addVariantCopy"))
	if want := fmt.Sprintf(`[%d,%d,%d,"MP3"]`, s.lg[1], s.lg[1], s.lgPkg); got != want {
		t.Fatalf("params = %s, want %s", got, want)
	}
}
