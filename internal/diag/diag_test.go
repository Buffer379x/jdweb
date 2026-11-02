package diag_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/Buffer379x/jdweb/internal/diag"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/testjd"
)

func prefixes(t *testing.T, s ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, p := range s {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

func check(t *testing.T, r diag.Report, id string) diag.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check %q in %+v", id, r)
	return diag.Check{}
}

func TestReachableJDownloader(t *testing.T) {
	srv := testjd.New(t)
	r := diag.Run(context.Background(), jd.New(srv.URL), prefixes(t, "127.0.0.0/8"))

	var ids []string
	for _, c := range r.Checks {
		ids = append(ids, c.ID)
		if c.MessageKey == "" {
			t.Errorf("%s has no message key", c.ID)
		}
	}
	if got := strings.Join(ids, ","); got != "api-reachable,jd-revision,api-no-login,jd-outside-subnet" {
		t.Errorf("checks = %s", got)
	}
	if c := check(t, r, "api-reachable"); c.Status != diag.StatusOK {
		t.Errorf("reachable = %+v", c)
	}
	if c := check(t, r, "jd-revision"); c.Status != diag.StatusOK || c.Detail != "50639" {
		t.Errorf("revision = %+v", c)
	}
	if c := check(t, r, "jd-outside-subnet"); c.Status != diag.StatusOK {
		t.Errorf("subnet = %+v", c)
	}
}

// R1 is accepted, not solved: the hint is always there.
func TestAPIWithoutLoginAlwaysWarns(t *testing.T) {
	srv := testjd.New(t)
	for name, c := range map[string]*jd.Client{"reachable": jd.New(srv.URL), "no address": nil, "unreachable": jd.New("http://127.0.0.1:1")} {
		r := diag.Run(context.Background(), c, prefixes(t, "127.0.0.0/8"))
		if got := check(t, r, "api-no-login"); got.Status != diag.StatusWarn || got.MessageKey == "" {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestNothingAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + ln.Addr().String()
	ln.Close()
	r := diag.Run(context.Background(), jd.New(url), prefixes(t, "127.0.0.0/8"))
	c := check(t, r, "api-reachable")
	if c.Status != diag.StatusFail {
		t.Errorf("reachable = %+v", c)
	}
	for _, part := range []string{"DeprecatedApiEnabled", "DeprecatedApiLocalhostOnly", url} {
		if !strings.Contains(c.Detail, part) {
			t.Errorf("detail %q does not name %q", c.Detail, part)
		}
	}
	if c := check(t, r, "jd-revision"); c.Status != diag.StatusFail {
		t.Errorf("revision = %+v", c)
	}
}

func TestAnotherServiceAnswers(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"html":       func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") },
		"404":        func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"ping false": func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"data":false}`) },
	} {
		t.Run(name, func(t *testing.T) {
			other := httptest.NewServer(h)
			defer other.Close()
			r := diag.Run(context.Background(), jd.New(other.URL), prefixes(t, "127.0.0.0/8"))
			c := check(t, r, "api-reachable")
			if c.Status != diag.StatusFail || !strings.Contains(c.Detail, "another service answers here") {
				t.Errorf("reachable = %+v", c)
			}
		})
	}
}

func TestNoAddress(t *testing.T) {
	r := diag.Run(context.Background(), nil, nil)
	if c := check(t, r, "api-reachable"); c.Status != diag.StatusFail {
		t.Errorf("reachable = %+v", c)
	}
	if c := check(t, r, "jd-outside-subnet"); c.Status != diag.StatusOK {
		t.Errorf("subnet without an address = %+v", c)
	}
}

func TestOldRevisionWarns(t *testing.T) {
	srv := testjd.New(t)
	srv.FailNext("/jd/getCoreRevision", 200, `{"data":49000}`)
	r := diag.Run(context.Background(), jd.New(srv.URL), nil)
	if c := check(t, r, "jd-revision"); c.Status != diag.StatusWarn || c.Detail != "49000" {
		t.Errorf("revision = %+v", c)
	}
}

// T-NET-03 and T-NET-02.
func TestSubnet(t *testing.T) {
	docker := []string{"127.0.0.0/8", "172.18.0.0/16"}
	lan := []string{"127.0.0.0/8", "192.168.1.0/24"}
	tests := []struct {
		name string
		ip   string
		own  []string
		want diag.Status
	}{
		{"loopback", "127.0.0.1", docker, diag.StatusOK},
		{"loopback without an interface list", "127.0.0.1", nil, diag.StatusOK},
		{"same Docker network", "172.18.0.5", docker, diag.StatusOK},
		{"Docker's default pool for many networks", "192.168.16.5", []string{"192.168.16.0/20"}, diag.StatusOK},
		{"other Docker network", "172.19.0.5", docker, diag.StatusWarn},
		{"LAN from a bridge", "192.168.1.5", docker, diag.StatusWarn},
		{"public address", "8.8.8.8", docker, diag.StatusWarn},
		{"same LAN", "192.168.1.5", lan, diag.StatusWarn},
		{"same 10.x LAN", "10.0.0.5", []string{"10.0.0.0/24"}, diag.StatusWarn},
		{"a LAN in the 172.16/12 range", "172.20.1.5", []string{"172.20.1.0/24"}, diag.StatusWarn},
		{"172.16.0.0/16 is no default of Docker", "172.16.0.9", []string{"172.16.0.0/16"}, diag.StatusWarn},
		{"a 192.168 LAN of Docker's size elsewhere", "10.1.0.5", []string{"10.1.0.0/20"}, diag.StatusWarn},
		{"no interface list", "172.18.0.5", nil, diag.StatusWarn},
		{"IPv6 loopback", "::1", docker, diag.StatusOK},
		{"IPv6 private", "fd00::5", []string{"fd00::/64"}, diag.StatusWarn},
		{"IPv4 in IPv6", "::ffff:172.18.0.5", docker, diag.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := diag.CheckSubnet(netip.MustParseAddr(tc.ip), prefixes(t, tc.own...))
			if c.ID != "jd-outside-subnet" || c.Status != tc.want || c.MessageKey == "" {
				t.Errorf("check = %+v, want %s", c, tc.want)
			}
			if tc.want == diag.StatusWarn && !strings.Contains(c.Detail, netip.MustParseAddr(tc.ip).Unmap().String()) {
				t.Errorf("detail %q does not name the address", c.Detail)
			}
		})
	}
	// The warnings say different things.
	keys := map[string]string{}
	for ip, own := range map[string][]string{"8.8.8.8": docker, "172.19.0.5": docker, "192.168.1.5": lan} {
		c := diag.CheckSubnet(netip.MustParseAddr(ip), prefixes(t, own...))
		if other, dup := keys[c.MessageKey]; dup {
			t.Errorf("%s and %s share the key %q", ip, other, c.MessageKey)
		}
		keys[c.MessageKey] = ip
	}
	// A private address outside all subnets may be a LAN, so R1 is named.
	c := diag.CheckSubnet(netip.MustParseAddr("172.20.9.9"), prefixes(t, lan...))
	if c.Status != diag.StatusWarn || !strings.Contains(c.Detail, "without a login") {
		t.Errorf("private outside = %+v", c)
	}
	if c := diag.CheckSubnet(netip.MustParseAddr("8.8.8.8"), prefixes(t, lan...)); strings.Contains(c.Detail, "LAN") {
		t.Errorf("public outside = %+v", c)
	}
}

func TestHostNameIsResolved(t *testing.T) {
	srv := testjd.New(t)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	r := diag.Run(context.Background(), jd.New("http://localhost:"+port), prefixes(t, "127.0.0.0/8", "::1/128"))
	if c := check(t, r, "jd-outside-subnet"); c.Status != diag.StatusOK {
		t.Errorf("subnet = %+v", c)
	}
	r = diag.Run(context.Background(), jd.New("http://no-such-host.invalid:3128"), nil)
	if c := check(t, r, "jd-outside-subnet"); c.Status != diag.StatusWarn || !strings.Contains(c.Detail, "no-such-host.invalid") {
		t.Errorf("unresolvable = %+v", c)
	}
}

func TestReportJSON(t *testing.T) {
	b, err := json.Marshal(diag.Report{Checks: []diag.Check{{ID: "x", Status: diag.StatusWarn, MessageKey: "k", Detail: "d"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"checks":[{"id":"x","status":"warn","messageKey":"k","detail":"d"}]}` {
		t.Errorf("json = %s", got)
	}
}

func TestOwnPrefixes(t *testing.T) {
	own, err := diag.OwnPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range own {
		found = found || p.Contains(netip.MustParseAddr("127.0.0.1"))
	}
	if !found {
		t.Errorf("no loopback in %v", own)
	}
}
