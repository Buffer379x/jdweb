//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

func bg() context.Context { return context.Background() }

// direct is the client that talks to JDownloader without JDWeb.
func direct() *jd.Client { return jd.New(jdURL, jd.WithUserAgent("jdweb-integration-test")) }

// cleanJD checks that the instance is the test stack and empty, and wipes it
// again when the test ends. Every test starts with it.
func cleanJD(t *testing.T) *jd.Client {
	t.Helper()
	c := direct()
	if err := requireTestInstance(bg(), c); err != nil {
		t.Fatal("REFUSING to run: ", err)
	}
	wipe(t, c)
	if dockerOK() {
		// Extracted files are not removed with the links: a later run would meet them.
		_, _ = docker("exec", jdContainer(), "sh", "-c", "rm -rf "+outputDir+"/*")
	}
	t.Cleanup(func() {
		if t.Failed() {
			showLog(t)
		}
		wipe(t, c)
	})
	return c
}

// showLog prints the end of the log of the shared jdweb when a test failed.
func showLog(t *testing.T) {
	sharedMu.Lock()
	a := sharedApp
	sharedMu.Unlock()
	if a == nil {
		return
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(a.logs.String()), "\n") {
		if !strings.Contains(l, `"msg":"request"`) {
			lines = append(lines, l)
		}
	}
	if len(lines) > 25 && os.Getenv("JDWEB_IT_VERBOSE") == "" {
		lines = lines[len(lines)-25:]
	}
	if ps, err := direct().DownloadPackages(bg(), jd.DownloadPackageQuery{}); err == nil {
		for _, p := range ps {
			t.Logf("JDownloader download list: package %q (%d)", p.Name, p.UUID)
		}
	}
	for _, l := range lines {
		if len(l) > 300 {
			l = l[:300]
		}
		t.Log("jdweb log: " + l)
	}
}

// wipe removes everything the tests may have left: answers open dialogs, stops
// the downloads, removes all links and their files, puts the limits back.
func wipe(t testing.TB, c *jd.Client) {
	ctx, cancel := context.WithTimeout(bg(), 60*time.Second)
	defer cancel()
	ids, _ := c.DialogIDs(ctx)
	for _, id := range ids {
		_ = c.DialogAnswer(ctx, id, map[string]any{"closereason": "CANCEL"})
	}
	jobs, _ := c.Captchas(ctx)
	for _, j := range jobs {
		_ = c.CaptchaSkip(ctx, j.ID, jd.SkipSingle)
	}
	_ = c.RemoveStopMark(ctx)
	_ = c.ControllerStop(ctx)
	_ = c.AbortCollecting(ctx)
	_ = c.Linkgrabber().Cleanup(ctx, nil, nil, jd.CleanupDeleteAll, jd.CleanupRemoveLinksOnly, jd.SelectionAll)
	_ = c.ClearLinkgrabber(ctx)
	_ = c.Downloads().Cleanup(ctx, nil, nil, jd.CleanupDeleteAll, jd.CleanupRemoveLinksAndDeleteFiles, jd.SelectionAll)
	if err := c.SetSpeedLimit(ctx, false, 51200); err != nil {
		t.Errorf("cannot put the speed limit back: %v", err)
	}
	// Wait until nothing runs any more: STOPPING is a state that ends by itself.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := c.ControllerState(ctx)
		if err == nil && st != jd.ControllerStopping {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (a *app) status(t testing.TB) state.Status {
	t.Helper()
	var st state.Status
	a.must(t, "GET", "/api/v1/status", nil, &st, 200)
	return st
}

type dlList struct {
	Packages []state.DlPackage `json:"packages"`
	Links    []state.DlLink    `json:"links"`
}

type lgList struct {
	Packages []state.LgPackage `json:"packages"`
	Links    []state.LgLink    `json:"links"`
}

func (a *app) downloads(t testing.TB) dlList {
	t.Helper()
	var l dlList
	a.must(t, "GET", "/api/v1/downloads", nil, &l, 200)
	return l
}

func (a *app) grabber(t testing.TB) lgList {
	t.Helper()
	var l lgList
	a.must(t, "GET", "/api/v1/linkgrabber", nil, &l, 200)
	return l
}

func (l dlList) link(name string) (state.DlLink, bool) {
	for _, k := range l.Links {
		if k.Name == name {
			return k, true
		}
	}
	return state.DlLink{}, false
}

func (l lgList) link(name string) (state.LgLink, bool) {
	for _, k := range l.Links {
		if k.Name == name {
			return k, true
		}
	}
	return state.LgLink{}, false
}

// baseName is the file name JDownloader shows for a fixture URL.
func baseName(u string) string {
	u, _, _ = strings.Cut(u, "?")
	return u[strings.LastIndex(u, "/")+1:]
}

// add sends links to the linkgrabber through JDWeb and waits until JDownloader
// has crawled them and the mirror shows each by its file name.
func (a *app) add(t testing.TB, pkg, dir string, urls ...string) lgList {
	t.Helper()
	body := map[string]any{"links": urls, "packageName": pkg, "autostart": false, "autoExtract": false, "overwritePackagizerRules": true}
	if dir != "" {
		body["destinationFolder"] = dir
	}
	a.must(t, "POST", "/api/v1/linkgrabber/add", body, nil, 200)
	var names []string
	for _, u := range urls {
		names = append(names, baseName(u))
	}
	return a.waitGrabberNames(t, names...)
}

// waitGrabberNames waits until the linkgrabber of the mirror holds these links and is not crawling.
func (a *app) waitGrabberNames(t testing.TB, names ...string) lgList {
	t.Helper()
	var l lgList
	waitFor(t, fmt.Sprintf("%d links in the linkgrabber", len(names)), 120*time.Second, func() bool {
		l = a.grabber(t)
		for _, n := range names {
			if _, ok := l.link(n); !ok {
				return false
			}
		}
		return !a.status(t).Linkgrabber.Crawling
	})
	return l
}

// stage adds links, moves them to the download list without starting and waits
// until the mirror shows them there.
func (a *app) stage(t testing.TB, pkg, dir string, urls ...string) dlList {
	t.Helper()
	g := a.add(t, pkg, dir, urls...)
	a.must(t, "POST", "/api/v1/linkgrabber/actions/to-downloads", map[string]any{"start": false, "packageIds": lgPackageIDs(g)}, nil, 204)
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

func lgPackageIDs(l lgList) []string {
	out := []string{}
	for _, p := range l.Packages {
		out = append(out, p.ID)
	}
	return out
}

func lgLinkIDs(l lgList) []string {
	out := []string{}
	for _, k := range l.Links {
		out = append(out, k.ID)
	}
	return out
}

func dlPackageIDs(l dlList) []string {
	out := []string{}
	for _, p := range l.Packages {
		out = append(out, p.ID)
	}
	return out
}

func dlLinkIDs(l dlList) []string {
	out := []string{}
	for _, k := range l.Links {
		out = append(out, k.ID)
	}
	return out
}

// docker runs a docker command. The tests use it for the JDownloader test
// container only.
func docker(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// limit sets the speed limit through JDWeb.
func (a *app) limit(t testing.TB, bytesPerSecond int64) {
	t.Helper()
	a.must(t, "POST", "/api/v1/speedlimit", map[string]any{"enabled": true, "bytesPerSecond": bytesPerSecond}, nil, 204)
}

func (a *app) ctl(t testing.TB, action string) {
	t.Helper()
	a.must(t, "POST", "/api/v1/controller/"+action, nil, nil, 204)
}

func (a *app) action(t testing.TB, list, action string, body any) {
	t.Helper()
	a.must(t, "POST", "/api/v1/"+list+"/actions/"+action, body, nil, 204)
}

// sel is the base body of an action.
func sel(links []string, pkgs []string) map[string]any {
	if links == nil {
		links = []string{}
	}
	if pkgs == nil {
		pkgs = []string{}
	}
	return map[string]any{"linkIds": links, "packageIds": pkgs}
}

func with(m map[string]any, kv ...any) map[string]any {
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// jdContainer is the JDownloader container of the test stack. The tests run
// docker commands against this one container only.
func jdContainer() string {
	if v := os.Getenv("JDWEB_TEST_JD_CONTAINER"); v != "" {
		return v
	}
	return "jdweb-test-jd"
}

// dockerOK reports whether the JDownloader container can be reached with docker.
func dockerOK() bool {
	out, err := docker("container", "inspect", "-f", "{{.State.Running}}", jdContainer())
	return err == nil && out == "true"
}

// fileOnDisk asks the container whether a file exists. known is false without docker.
func fileOnDisk(path string) (exists, known bool) {
	if !dockerOK() {
		return false, false
	}
	_, err := docker("exec", jdContainer(), "test", "-e", path)
	return err == nil, true
}

func eventually(t testing.TB, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	waitFor(t, what, timeout, ok)
}

// fixtureIP is the address of the fixture container in the test network, a
// second host name for the same server. "" without docker.
func fixtureIP() string {
	out, err := docker("container", "inspect", "-f", "{{(index .NetworkSettings.Networks \"jdweb-test-net\").IPAddress}}", "jdweb-test-files")
	if err != nil || out == "" {
		return ""
	}
	return out
}
