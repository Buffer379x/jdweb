// Package testguard is the safety gate of every test that talks to a real
// JDownloader. It is imported by tests only, never by the program.
//
// The same rule is written in test/e2e/support/guard.ts; keep Rule identical.
package testguard

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// Rule is the one definition of what a test instance is.
const Rule = "A JDownloader counts as a test instance only if all of these hold: " +
	"(1) the URL host is 127.0.0.1, or JDWEB_TEST_ALLOW_REMOTE is exactly that URL; " +
	"(2) its device name is \"jdweb-test-stack\", which scripts/teststack.sh up sets; " +
	"(3) every package name starts with the prefix of the tests; " +
	"(4) no dialog and no captcha is open."

// Marker is the harmless setting scripts/teststack.sh up gives the test stack.
const (
	MarkerInterface = "org.jdownloader.api.myjdownloader.MyJDownloaderSettings"
	MarkerKey       = "DeviceName"
	MarkerValue     = "jdweb-test-stack"
)

// Require returns nil only for a test instance (see Rule). rawURL is the
// address the tests were given, prefix the start of the package names the
// tests create. It changes nothing in JDownloader. When JDWEB_TEST_JD_CONTAINER
// is set, the port that container publishes must be the port of the URL.
func Require(ctx context.Context, c *jd.Client, rawURL, prefix string) error {
	refuse := func(format string, a ...any) error {
		return fmt.Errorf("REFUSING to run against %s: %s\n%s", rawURL, fmt.Sprintf(format, a...), Rule)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return refuse("not an address")
	}
	if u.Hostname() != "127.0.0.1" && strings.TrimRight(os.Getenv("JDWEB_TEST_ALLOW_REMOTE"), "/") != strings.TrimRight(rawURL, "/") {
		return refuse("the host is %q", u.Hostname())
	}
	if name := os.Getenv("JDWEB_TEST_JD_CONTAINER"); name != "" {
		out, err := exec.CommandContext(ctx, "docker", "port", name).CombinedOutput()
		if err != nil {
			return refuse("JDWEB_TEST_JD_CONTAINER=%s: %v", name, err)
		}
		if !strings.Contains(string(out), ":"+u.Port()+"\n") && !strings.HasSuffix(strings.TrimSpace(string(out)), ":"+u.Port()) {
			return refuse("the container %s does not publish port %s (docker port: %s)", name, u.Port(), strings.TrimSpace(string(out)))
		}
	}
	if ok, err := c.Ping(ctx); err != nil || !ok {
		return refuse("JDownloader does not answer: %v", err)
	}
	raw, err := c.ConfigGet(ctx, MarkerInterface, "", MarkerKey)
	if err != nil {
		return refuse("cannot read the device name: %v", err)
	}
	if got := strings.Trim(string(raw), `"`); got != MarkerValue {
		return refuse("the device name is %q, not %q", got, MarkerValue)
	}
	dl, err := c.DownloadPackages(ctx, jd.DownloadPackageQuery{})
	if err != nil {
		return refuse("%v", err)
	}
	lg, err := c.GrabberPackages(ctx, jd.GrabberPackageQuery{})
	if err != nil {
		return refuse("%v", err)
	}
	for _, p := range dl {
		if !strings.HasPrefix(p.Name, prefix) {
			return refuse("the download list holds the package %q (prefix %q)", p.Name, prefix)
		}
	}
	for _, p := range lg {
		if !strings.HasPrefix(p.Name, prefix) {
			return refuse("the linkgrabber holds the package %q (prefix %q)", p.Name, prefix)
		}
	}
	if ids, err := c.DialogIDs(ctx); err != nil || len(ids) > 0 {
		return refuse("a dialog is open (%d) or cannot be read (%v)", len(ids), err)
	}
	if cs, err := c.Captchas(ctx); err != nil || len(cs) > 0 {
		return refuse("a captcha is open (%d) or cannot be read (%v)", len(cs), err)
	}
	return nil
}
