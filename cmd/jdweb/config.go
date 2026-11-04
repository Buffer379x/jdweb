package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/jd"
)

// Defaults of the settings, plus PUID and PGID of the Unraid convention.
const (
	defaultJDURL   = "http://jdownloader:3128"
	defaultListen  = ":7468"
	defaultDataDir = "/data"
	defaultPUID    = 99
	defaultPGID    = 100
)

// config is what the environment says. The address of JDownloader saved in the
// web interface wins over JDWEB_JD_URL, see serve.
type config struct {
	JDURL          string // normalized
	JDURLSource    string // "env" or "default"
	Listen         string
	DataDir        string
	TrustedProxies []netip.Prefix
	LogLevel       slog.Level
	PUID, PGID     int
}

// loadConfig reads the JDWEB_* variables. Every error names the
// variable and what is wrong with it.
func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		JDURL: defaultJDURL, JDURLSource: "default", Listen: defaultListen, DataDir: defaultDataDir,
		PUID: defaultPUID, PGID: defaultPGID,
	}
	if v := strings.TrimSpace(getenv("JDWEB_JD_URL")); v != "" {
		u, err := jd.NormalizeURL(v)
		if err != nil {
			return c, fmt.Errorf("JDWEB_JD_URL: %w", err)
		}
		c.JDURL, c.JDURLSource = u, "env"
	}
	if v := strings.TrimSpace(getenv("JDWEB_LISTEN")); v != "" {
		if _, port, err := net.SplitHostPort(v); err != nil {
			return c, fmt.Errorf("JDWEB_LISTEN must look like :7468 or 0.0.0.0:7468, got %q", v)
		} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("JDWEB_LISTEN has the port %q, which is not between 1 and 65535", port)
		}
		c.Listen = v
	}
	if v := strings.TrimSpace(getenv("JDWEB_DATA_DIR")); v != "" {
		c.DataDir = v
	}
	if v := getenv("JDWEB_TRUSTED_PROXIES"); strings.TrimSpace(v) != "" {
		p, err := auth.ParseTrustedProxies(v)
		if err != nil {
			return c, fmt.Errorf("JDWEB_TRUSTED_PROXIES: %w", err)
		}
		c.TrustedProxies = p
	}
	if v := strings.TrimSpace(getenv("JDWEB_LOG_LEVEL")); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			c.LogLevel = slog.LevelDebug
		case "info":
			c.LogLevel = slog.LevelInfo
		case "warn", "warning":
			c.LogLevel = slog.LevelWarn
		case "error":
			c.LogLevel = slog.LevelError
		default:
			return c, fmt.Errorf("JDWEB_LOG_LEVEL must be debug, info, warn or error, got %q", v)
		}
	}
	var err error
	if c.PUID, err = envID(getenv, "PUID", defaultPUID); err != nil {
		return c, err
	}
	if c.PGID, err = envID(getenv, "PGID", defaultPGID); err != nil {
		return c, err
	}
	return c, nil
}

func envID(getenv func(string) string, key string, def int) (int, error) {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a number, got %q", key, v)
	}
	return n, nil
}

// healthURL is the address the healthcheck asks: this process, on the port it
// listens on.
func healthURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1" + defaultListen + "/healthz"
	}
	if ip, err := netip.ParseAddr(host); host == "" || err == nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

var errNoAccount = auth.ErrNoAccount

func isNoAccount(err error) bool { return errors.Is(err, errNoAccount) }
