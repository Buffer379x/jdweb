package api

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/testjd"
)

func TestControllerRoutes(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		action, path, params string
	}{
		{"start", "/downloadcontroller/start", "[]"},
		{"stop", "/downloadcontroller/stop", "[]"},
		{"pause", "/downloadcontroller/pause", "[true]"},
		{"resume", "/downloadcontroller/pause", "[false]"},
	} {
		e.jd.ResetCalls()
		e.expect(e.post("/api/v1/controller/"+c.action, nil), 204)
		if got := argsJSON(e.lastCall(c.path)); got != c.params {
			t.Errorf("%s: params %s, want %s", c.action, got, c.params)
		}
	}
	e.expectErr(e.post("/api/v1/controller/explode", nil), 404, "not_found")
}

func TestSpeedLimit(t *testing.T) {
	e := newEnv(t)
	const gs = "org.jdownloader.settings.GeneralSettings"
	e.expect(e.post("/api/v1/speedlimit", map[string]any{"enabled": true, "bytesPerSecond": 1048576}), 204)
	if e.jd.Config(gs, "DownloadSpeedLimit") != int64(1048576) || e.jd.Config(gs, "DownloadSpeedLimitEnabled") != true {
		t.Fatalf("config = %v %v", e.jd.Config(gs, "DownloadSpeedLimit"), e.jd.Config(gs, "DownloadSpeedLimitEnabled"))
	}
	e.expect(e.post("/api/v1/speedlimit", map[string]any{"enabled": false, "bytesPerSecond": 0}), 204)
	if e.jd.Config(gs, "DownloadSpeedLimitEnabled") != false || e.jd.Config(gs, "DownloadSpeedLimit") != int64(1048576) {
		t.Fatal("switching off must keep the value")
	}
	for name, body := range map[string]any{
		"negative": map[string]any{"enabled": true, "bytesPerSecond": -1},
		"huge":     map[string]any{"enabled": true, "bytesPerSecond": int64(1) << 50},
		"string":   `{"enabled":true,"bytesPerSecond":"5"}`,
	} {
		e.jd.ResetCalls()
		if r := e.post("/api/v1/speedlimit", body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
		if len(e.jd.Calls()) != 0 {
			t.Errorf("%s: JDownloader was called", name)
		}
	}
}

func TestToggles(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	for _, c := range []struct{ name, path string }{
		{"clipboard", "/toolbar/toggleClipboardMonitoring"}, {"reconnect", "/toolbar/toggleAutomaticReconnect"},
		{"stop-after", "/toolbar/toggleStopAfterCurrentDownload"}, {"premium", "/toolbar/togglePremium"},
	} {
		before := len(e.jdCalls(c.path))
		r := e.post("/api/v1/toggles/"+c.name, nil)
		e.expect(r, 200)
		var out struct{ Enabled bool }
		r.json(t, &out)
		if len(e.jdCalls(c.path)) != before+1 {
			t.Errorf("%s: no call to %s", c.name, c.path)
		}
		// The fake started with clipboard, reconnect and premium on and stop-after off.
		wantOn := c.name == "stop-after"
		if out.Enabled != wantOn {
			t.Errorf("%s: enabled = %v, want %v", c.name, out.Enabled, wantOn)
		}
	}
	e.expectErr(e.post("/api/v1/toggles/speed", nil), 404, "not_found")
}

// A browser with an old view asks for a state: a switch that has it already is left alone.
func TestToggleWithAWantedStateDoesNotFlipTheWrongWay(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected() // clipboard is on in the fake
	e.jd.ResetCalls()
	r := e.post("/api/v1/toggles/clipboard", map[string]any{"enabled": true})
	e.expect(r, 200)
	if len(e.jdCalls("/toolbar/toggleClipboardMonitoring")) != 0 || !strings.Contains(string(r.body), `"enabled":true`) {
		t.Fatalf("a switch that was on was flipped: %s %v", r.body, e.jd.Calls())
	}
	r = e.post("/api/v1/toggles/clipboard", map[string]any{"enabled": false})
	e.expect(r, 200)
	if len(e.jdCalls("/toolbar/toggleClipboardMonitoring")) != 1 || !strings.Contains(string(r.body), `"enabled":false`) {
		t.Fatalf("clipboard not switched off: %s", r.body)
	}
}

// Switching premium off makes JDownloader ask in a dialog and not answer.
func TestPremiumOffWaitsForTheDialog(t *testing.T) {
	old := premiumOffWait
	premiumOffWait = 150 * time.Millisecond
	defer func() { premiumOffWait = old }()
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected() // premium is on in the fake
	e.jd.HangNext("/toolbar/togglePremium")
	start := time.Now()
	r := e.post("/api/v1/toggles/premium", map[string]any{"enabled": false})
	if r.status != 202 || !strings.Contains(string(r.body), `"pending":true`) {
		t.Fatalf("premium off = %d %s, want 202 pending", r.status, r.body)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	// Switching it on is a normal call and not special.
	e.jd.ResetCalls()
	e.expect(e.post("/api/v1/toggles/premium", map[string]any{"enabled": true}), 200)
}

func TestReconnect(t *testing.T) {
	e := newEnv(t)
	e.expect(e.post("/api/v1/reconnect", nil), 204)
	if len(e.jdCalls("/reconnect/doReconnect")) != 1 {
		t.Fatalf("calls: %v", e.jd.Calls())
	}
}

func TestSystemActions(t *testing.T) {
	e := newEnv(t) // the fake is a Docker JDownloader
	for _, c := range []struct{ action, path string }{{"restart", "/system/restartJD"}, {"exit", "/system/exitJD"}} {
		e.jd.ResetCalls()
		r := e.post("/api/v1/system/"+c.action, nil)
		if r.status != 202 || len(e.jdCalls(c.path)) != 1 {
			t.Errorf("%s: %d, calls %v", c.action, r.status, e.jd.Calls())
		}
	}
	// Machine actions are refused in Docker, and JDownloader is never asked.
	for _, a := range []string{"shutdown", "hibernate", "standby"} {
		e.jd.ResetCalls()
		r := e.post("/api/v1/system/"+a, nil)
		e.expectErr(r, 403, "forbidden")
		for _, p := range []string{"/system/shutdownOS", "/system/hibernateOS", "/system/standbyOS"} {
			if len(e.jdCalls(p)) != 0 {
				t.Errorf("%s: JDownloader got %s although docker is true", a, p)
			}
		}
	}
	// Outside Docker they go through.
	e.jd.SetDocker(false)
	for _, c := range []struct{ action, path string }{{"shutdown", "/system/shutdownOS"}, {"hibernate", "/system/hibernateOS"}, {"standby", "/system/standbyOS"}} {
		e.jd.ResetCalls()
		r := e.post("/api/v1/system/"+c.action, nil)
		if r.status != 202 || len(e.jdCalls(c.path)) != 1 {
			t.Errorf("%s outside docker: %d, calls %v", c.action, r.status, e.jd.Calls())
		}
	}
	if got := argsJSON(e.lastCall("/system/standbyOS")); got != "[]" {
		t.Errorf("params = %s", got)
	}
}

// docker:true is checked live, not taken from the last status.
func TestShutdownChecksDockerLive(t *testing.T) {
	e := newEnv(t, withRun, withFastSync, func(o *envOptions) { o.notDocker = true })
	e.waitConnected()
	if e.engine.Status().JD.Docker {
		t.Fatal("setup: status says docker")
	}
	e.jd.SetDocker(true) // JDownloader moved to Docker after the engine looked
	e.expectErr(e.post("/api/v1/system/shutdown", nil), 403, "forbidden")
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/v1/update")
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"available":false`) {
		t.Fatalf("update = %s", r.body)
	}
	e.expect(e.post("/api/v1/update/check", nil), 204)
	if len(e.jdCalls("/update/runUpdateCheck")) != 1 {
		t.Fatal("no update check")
	}
	if r := e.post("/api/v1/update/apply", nil); r.status != 202 || len(e.jdCalls("/update/restartAndUpdate")) != 1 {
		t.Fatalf("apply = %d, calls %v", r.status, e.jd.Calls())
	}
}

func TestSystemInfo(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	r := e.get("/api/v1/system")
	e.expect(r, 200)
	var out map[string]any
	r.json(t, &out)
	if out["docker"] != true || out["version"] != "1.2.3-test" || out["revision"] != float64(50639) || out["javaVersion"] == nil {
		t.Fatalf("system = %v", out)
	}
}

// Review Focus 1 and 3: the messages differ by cause and say what to do.
func TestJDownloaderFailuresHaveDistinctMessages(t *testing.T) {
	e := newEnv(t)
	e.seed()
	e.jd.Down()
	offline := e.post("/api/v1/downloads/actions/remove", map[string]any{"linkIds": []string{"1"}})
	e.expectErr(offline, 503, "jd_offline")
	e.jd.Up()

	e.jd.FailNext("/downloadsV2/removeLinks", 500, "java.lang.NullPointerException\n\tat org.appwork.Secret.stack(Secret.java:1)")
	internal := e.post("/api/v1/downloads/actions/remove", map[string]any{"linkIds": []string{"1"}})
	e.expectErr(internal, 502, "jd_error")

	e.jd.FailNext("/downloadsV2/removeLinks", 400, `{"src":"DEVICE","type":"BAD_PARAMETERS","data":"leaky detail"}`)
	refused := e.post("/api/v1/downloads/actions/remove", map[string]any{"linkIds": []string{"1"}})
	e.expectErr(refused, 502, "jd_error")

	e.jd.FailNext("/downloadsV2/removeLinks", 200, "<html>Router login</html>")
	other := e.post("/api/v1/downloads/actions/remove", map[string]any{"linkIds": []string{"1"}})
	e.expectErr(other, 502, "jd_error")

	seen := map[string]bool{}
	for name, r := range map[string]result{"offline": offline, "internal": internal, "refused": refused, "other service": other} {
		msg := r.errMessage()
		if seen[msg] {
			t.Errorf("%s: the message %q is used for another cause too", name, msg)
		}
		seen[msg] = true
		for _, leak := range []string{"NullPointer", "appwork", "Secret", "leaky", "Router", "<html>", "BAD_PARAMETERS", "/downloadsV2"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s: message %q leaks %q", name, msg, leak)
			}
		}
	}
	if !strings.Contains(offline.errMessage(), "address") || !strings.Contains(other.errMessage(), "another service") {
		t.Errorf("messages do not say what to check: %q / %q", offline.errMessage(), other.errMessage())
	}
}

func TestNoJDownloaderAddressYet(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.noJD = true })
	r := e.post("/api/v1/controller/start", nil)
	e.expectErr(r, 503, "jd_offline")
	if !strings.Contains(r.errMessage(), "No JDownloader address") {
		t.Errorf("message = %q", r.errMessage())
	}
	// Reading state still works.
	e.expect(e.get("/api/v1/status"), 200)
	e.expect(e.get("/api/v1/downloads"), 200)
}

func TestAddLinks(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/v1/linkgrabber/add", map[string]any{
		"links": []string{"http://a/1", "  ", "http://a/2\n"}, "packageName": "Pack", "destinationFolder": "/output/p",
		"extractPassword": "ep", "downloadPassword": "dp", "autostart": true, "autoExtract": false, "deepDecrypt": true,
		"overwritePackagizerRules": false, "priority": "HIGH",
	})
	e.expect(r, 200)
	var out struct{ JobID string }
	r.json(t, &out)
	if out.JobID == "" || strings.Contains(string(r.body), `"jobId":1`) && !strings.Contains(string(r.body), `"jobId":"`) {
		t.Fatalf("answer = %s", r.body)
	}
	c := e.lastCall("/linkgrabberv2/addLinks")
	for _, want := range []string{`"links":"http://a/1\nhttp://a/2"`, `"packageName":"Pack"`, `"destinationFolder":"/output/p"`,
		`"extractPassword":"ep"`, `"downloadPassword":"dp"`, `"autostart":true`, `"autoExtract":false`, `"deepDecrypt":true`,
		`"overwritePackagizerRules":false`, `"priority":"HIGH"`} {
		if !strings.Contains(argsJSON(c), want) {
			t.Errorf("params %s lack %s", argsJSON(c), want)
		}
	}
	for name, body := range map[string]any{
		"no links":     map[string]any{"links": []string{}},
		"blank links":  map[string]any{"links": []string{" ", ""}},
		"links string": `{"links":"http://x"}`,
		"bad priority": map[string]any{"links": []string{"http://x"}, "priority": "SOON"},
		"control name": map[string]any{"links": []string{"http://x"}, "packageName": "a\nb"},
	} {
		e.jd.ResetCalls()
		if r := e.post("/api/v1/linkgrabber/add", body); r.status != 400 || len(e.jd.Calls()) != 0 {
			t.Errorf("%s: %d %s, calls %d", name, r.status, r.body, len(e.jd.Calls()))
		}
	}
}

func TestAddContainer(t *testing.T) {
	e := newEnv(t)
	content := base64Of("http://a/1\nhttp://a/2")
	r := e.post("/api/v1/linkgrabber/container", map[string]any{"name": "Links.DLC", "contentBase64": content})
	e.expect(r, 200)
	c := e.lastCall("/linkgrabberv2/addContainer")
	if !strings.HasPrefix(argsJSON(c), `["dlc","data:application/octet-stream;base64,`) {
		t.Fatalf("params = %s", argsJSON(c))
	}
	for name, body := range map[string]any{
		"no extension":  map[string]any{"name": "links", "contentBase64": content},
		"odd extension": map[string]any{"name": "links.d/c", "contentBase64": content},
		"not base64":    map[string]any{"name": "a.dlc", "contentBase64": "***"},
		"empty":         map[string]any{"name": "a.dlc", "contentBase64": ""},
	} {
		e.jd.ResetCalls()
		if r := e.post("/api/v1/linkgrabber/container", body); r.status != 400 || len(e.jd.Calls()) != 0 {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
}

func TestBodyLimits(t *testing.T) {
	e := newEnv(t)
	huge := `{"linkIds":["1"],"name":"` + strings.Repeat("a", 2<<20) + `"}`
	r := e.post("/api/v1/downloads/actions/remove", huge)
	if r.status != 413 || r.errCode() != "bad_request" {
		t.Fatalf("2 MiB on a JSON route: %d %s", r.status, r.body)
	}
	// Chunked bodies without a length are cut too.
	r = e.do("PUT", "/api/v1/prefs", `{"a":"`+strings.Repeat("b", 70<<10)+`"}`)
	if r.status != 413 {
		t.Fatalf("70 KiB of prefs: %d %s", r.status, r.body)
	}
	// Adding links is capped at 8 MiB, only containers may be bigger.
	r = e.post("/api/v1/linkgrabber/add", `{"links":["`+strings.Repeat("a", 9<<20)+`"]}`)
	if r.status != 413 {
		t.Fatalf("9 MiB of links: %d", r.status)
	}
	r = e.post("/api/v1/linkgrabber/container", `{"name":"a.dlc","contentBase64":"`+strings.Repeat("A", 9<<20)+`"}`)
	if r.status == 413 {
		t.Fatalf("9 MiB container refused: %s", r.body)
	}
	// Links and containers may be big.
	links := make([]string, 0, 30000)
	for i := 0; i < 30000; i++ {
		links = append(links, fmt.Sprintf("https://example.com/files/%0100d", i))
	}
	r = e.post("/api/v1/linkgrabber/add", map[string]any{"links": links})
	e.expect(r, 200)
	if n := len(e.jdCalls("/linkgrabberv2/addLinks")); n != 1 {
		t.Fatalf("%d addLinks calls", n)
	}
}

func TestStopMarkRead(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/v1/downloads/stopmark")
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"marked":false`) {
		t.Fatalf("stopmark = %s", r.body)
	}
	_, links := e.jd.AddPackage(testjd.Downloads, testjd.Package{Name: "p"}, testjd.Link{Name: "stop-here"})
	e.expect(e.post("/api/v1/downloads/actions/stopmark-set", map[string]any{"linkId": fmt.Sprint(links[0])}), 204)
	r = e.get("/api/v1/downloads/stopmark")
	if !strings.Contains(string(r.body), `"marked":true`) || !strings.Contains(string(r.body), fmt.Sprintf(`"linkId":"%d"`, links[0])) ||
		!strings.Contains(string(r.body), `"name":"stop-here"`) {
		t.Fatalf("stopmark = %s", r.body)
	}
}

func TestVariantsAndFolders(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/v1/linkgrabber/variants/123")
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"variants":[]`) {
		t.Fatalf("variants = %s", r.body)
	}
	e.expectErr(e.get("/api/v1/linkgrabber/variants/abc"), 400, "bad_request")
	r = e.get("/api/v1/linkgrabber/folders")
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"folders":[`) {
		t.Fatalf("folders = %s", r.body)
	}
}

func base64Of(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// After a switch or the speed limit changed, the status is read again at once
// and does not wait for the next poll (the test's engine polls once per hour).
func TestToggleAndSpeedLimitRefreshTheStatus(t *testing.T) {
	e := newEnv(t, withRun, withFastSync)
	e.waitConnected()
	statusNow := func() (st struct {
		Toggles struct {
			Limit      bool
			LimitBytes int64
			StopAfter  bool
		}
	}) {
		e.get("/api/v1/status").json(t, &st)
		return st
	}
	within := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the status did not show %s within 2 s", what)
			}
		}
	}
	e.expect(e.post("/api/v1/toggles/stop-after", map[string]any{"enabled": true}), 200)
	within("stop-after", func() bool { return statusNow().Toggles.StopAfter })
	e.expect(e.post("/api/v1/speedlimit", map[string]any{"enabled": true, "bytesPerSecond": 123456}), 204)
	within("the speed limit", func() bool { st := statusNow(); return st.Toggles.Limit && st.Toggles.LimitBytes == 123456 })
}
