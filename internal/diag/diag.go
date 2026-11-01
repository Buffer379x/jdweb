// Package diag is the setup check: can JDWeb talk to JDownloader, which one is
// it, and what does the chosen address mean for the safety of the API.
package diag

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

// MinRevision is the JDownloader revision JDWeb was tested against.
const MinRevision = 50639

// Status is the outcome of one check.
type Status string

// The outcomes of a check.
const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Check is one result. MessageKey selects the translated text of the frontend,
// Detail holds the values for it in plain English.
type Check struct {
	ID         string `json:"id"`
	Status     Status `json:"status"`
	MessageKey string `json:"messageKey"`
	Detail     string `json:"detail"`
}

// Report is the result of the setup check.
type Report struct {
	Checks []Check `json:"checks"`
}

// The ids of the checks.
const (
	idReachable = "api-reachable"
	idRevision  = "jd-revision"
	idNoLogin   = "api-no-login"
	idSubnet    = "jd-outside-subnet"
)

// Run checks the JDownloader behind client, which is nil when no address is
// set. own holds the subnets of this machine, see OwnPrefixes.
func Run(ctx context.Context, client *jd.Client, own []netip.Prefix) Report {
	if client == nil {
		return Report{Checks: []Check{
			{idReachable, StatusFail, "diag.api-reachable.no-address", state.DetailNoURL},
			{idRevision, StatusFail, "diag.jd-revision.unknown", ""},
			noLogin("the JDownloader address"),
			{idSubnet, StatusOK, "diag.jd-outside-subnet.no-address", ""},
		}}
	}
	base := client.BaseURL()
	reachable := checkReachable(ctx, client)
	return Report{Checks: []Check{
		reachable,
		checkRevision(ctx, client, reachable),
		noLogin(base),
		checkHost(ctx, base, own),
	}}
}

func checkReachable(ctx context.Context, c *jd.Client) Check {
	ok, err := c.Ping(ctx)
	switch {
	case err == nil && ok:
		return Check{idReachable, StatusOK, "diag.api-reachable.ok", c.BaseURL()}
	case jd.IsConnection(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return Check{idReachable, StatusFail, "diag.api-reachable.no-answer", state.DetailNotReachable(c.BaseURL())}
	}
	return Check{idReachable, StatusFail, "diag.api-reachable.other-service", state.DetailOtherService}
}

func checkRevision(ctx context.Context, c *jd.Client, reachable Check) Check {
	if reachable.Status != StatusOK {
		return Check{idRevision, StatusFail, "diag.jd-revision.unknown", ""}
	}
	rev, err := c.CoreRevision(ctx)
	if err != nil {
		return Check{idRevision, StatusWarn, "diag.jd-revision.unknown", ""}
	}
	detail := strconv.FormatInt(rev, 10)
	if rev < MinRevision {
		return Check{idRevision, StatusWarn, "diag.jd-revision.old", detail}
	}
	return Check{idRevision, StatusOK, "diag.jd-revision.ok", detail}
}

// noLogin is risk R1: it is accepted and stays a warning for good. Acknowledging
// it is up to the API layer.
func noLogin(address string) Check {
	return Check{idNoLogin, StatusWarn, "diag.api-no-login.warn",
		"the JDownloader API has no login: whatever reaches " + address + " controls JDownloader"}
}

func checkHost(ctx context.Context, base string, own []netip.Prefix) Check {
	u, err := url.Parse(base)
	if err != nil {
		return Check{idSubnet, StatusWarn, "diag.jd-outside-subnet.unresolved", base}
	}
	host := u.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil {
		ips, lookupErr := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if lookupErr != nil || len(ips) == 0 {
			return Check{idSubnet, StatusWarn, "diag.jd-outside-subnet.unresolved", host}
		}
		ip = ips[0]
	}
	return CheckSubnet(ip, own)
}

// CheckSubnet judges the address of JDownloader. It warns when the address lies
// outside all own subnets (T-NET-03: it may be unreachable, and the path leaves
// this machine), and when it is in a private LAN range that is not an internal
// Docker network: then every device in that LAN reaches the API (R1, T-NET-02).
func CheckSubnet(ip netip.Addr, own []netip.Prefix) Check {
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return Check{idSubnet, StatusOK, "diag.jd-outside-subnet.ok", ip.String()}
	}
	var match netip.Prefix
	found := false
	for _, p := range own {
		if p.Contains(ip) && (!found || p.Bits() > match.Bits()) {
			match, found = p, true
		}
	}
	switch {
	case !found:
		return Check{idSubnet, StatusWarn, "diag.jd-outside-subnet.outside",
			ip.String() + " is outside the subnets of JDWeb"}
	case ip.IsPrivate() && !dockerInternal(match):
		return Check{idSubnet, StatusWarn, "diag.jd-outside-subnet.lan",
			ip.String() + " is in a private LAN: every device there reaches the JDownloader API without a login"}
	}
	return Check{idSubnet, StatusOK, "diag.jd-outside-subnet.ok", ip.String()}
}

var (
	dockerBridges = netip.MustParsePrefix("172.16.0.0/12")
	dockerSpare   = netip.MustParsePrefix("192.168.0.0/16")
)

// dockerInternal reports whether a subnet looks like a network Docker made: its
// default address pools are 172.17-31.0.0/16 and then /20 networks of 192.168.0.0/16.
func dockerInternal(p netip.Prefix) bool {
	return dockerBridges.Contains(p.Addr()) || (dockerSpare.Contains(p.Addr()) && p.Bits() == 20)
}

// OwnPrefixes returns the subnets of the interfaces of this machine.
func OwnPrefixes() ([]netip.Prefix, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		bits, _ := n.Mask.Size()
		ip = ip.Unmap()
		if ip.Is4() && bits > 32 {
			bits -= 96
		}
		out = append(out, netip.PrefixFrom(ip, bits).Masked())
	}
	return out, nil
}
