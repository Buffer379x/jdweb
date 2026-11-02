// jdweb is a web interface for JDownloader. One binary: the web server, the
// healthcheck for Docker and the commands for maintenance.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata" // time zones without a zoneinfo directory in the image

	"github.com/Buffer379x/jdweb/internal/api"
	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/diag"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/logmask"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/store"
	jdsync "github.com/Buffer379x/jdweb/internal/sync"
	"github.com/Buffer379x/jdweb/web"
)

// version is set at build time: -ldflags "-X main.version=0.1.0".
var version = "dev"

const usage = `jdweb %s - a web interface for JDownloader (unofficial, not affiliated with AppWork)

Commands:
  serve            start the web server (default)
  healthcheck      ask the running server whether it is healthy (exit code 0 = yes)
  reset-password   print a one-time token to set a new password in the browser
  version          print the version

Settings come from environment variables, see the README:
  JDWEB_JD_URL (default http://jdownloader:3128)   JDWEB_LISTEN (default :7468)
  JDWEB_DATA_DIR (default /data)                   JDWEB_TRUSTED_PROXIES
  JDWEB_LOG_LEVEL (default info)                   PUID, PGID (default 99, 100)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes a command and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck(os.Getenv, stdout)
	case "reset-password":
		err = resetPassword(os.Getenv, stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
	case "help", "--help", "-h":
		fmt.Fprintf(stdout, usage, version)
	default:
		fmt.Fprintf(stderr, usage, version)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "jdweb:", err)
		return 1
	}
	return 0
}

// newLogger writes JSON lines. Attributes that look like secrets are masked as
// a safety net, see logmask.
func newLogger(c config, w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: c.LogLevel, ReplaceAttr: logmask.ReplaceAttr}))
}

// checkDataDir refuses a data directory that must never be handed to
// PUID:PGID: the empty path and the root of the file system, also through a
// symbolic link. A typo in JDWEB_DATA_DIR must not chown a machine.
func checkDataDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("JDWEB_DATA_DIR is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("JDWEB_DATA_DIR %q: %w", dir, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if abs == string(filepath.Separator) || filepath.Dir(abs) == abs {
		return fmt.Errorf("JDWEB_DATA_DIR %q is the root of the file system. Use a folder of its own, for example /data", dir)
	}
	return nil
}

// writableHint is the one-line message for a data directory that cannot be
// written (Review Focus 5). Docker creates the folder of a bind mount as root.
func writableHint(dir string, uid, gid int, err error) error {
	return fmt.Errorf("the data directory %s is not writable (%v). Run `chown -R %d:%d <path of the folder on the host>`, "+
		"or start the container as root (--user 0:0) so that JDWeb sets the permissions itself and then drops root", dir, rootCause(err), uid, gid)
}

func rootCause(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// ensureWritable checks that files can be created in the data directory.
func ensureWritable(dir string, uid, gid int) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return writableHint(dir, uid, gid, err)
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return writableHint(dir, uid, gid, err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// openData prepares the data directory and opens the database: drop root,
// check the folder, migrate. Each failure is a message, not a stack trace.
func openData(c config, log *slog.Logger) (*store.DB, error) {
	if err := checkDataDir(c.DataDir); err != nil {
		return nil, err
	}
	if err := dropPrivileges(c.DataDir, c.PUID, c.PGID, log); err != nil {
		return nil, err
	}
	if err := ensureWritable(c.DataDir, c.PUID, c.PGID); err != nil {
		return nil, err
	}
	db, err := store.Open(filepath.Join(c.DataDir, "jdweb.db"))
	if err != nil {
		if errors.Is(err, store.ErrNotWritable) {
			return nil, writableHint(c.DataDir, c.PUID, c.PGID, err)
		}
		return nil, err
	}
	return db, nil
}

// chooseURL: the address saved in the web interface wins over JDWEB_JD_URL.
func chooseURL(db *store.DB, c config, log *slog.Logger) (url, source string, err error) {
	var saved string
	switch err := db.GetJSON(context.Background(), "jd.url", &saved); {
	case err == nil:
		if norm, nerr := jd.NormalizeURL(saved); nerr == nil {
			if c.JDURLSource == "env" && norm != c.JDURL {
				log.Info("using the JDownloader address saved in the web interface, not JDWEB_JD_URL", "url", norm)
			}
			return norm, "ui", nil
		}
		log.Warn("the saved JDownloader address is invalid and is ignored")
	case !errors.Is(err, store.ErrNotFound):
		return "", "", err
	}
	return c.JDURL, c.JDURLSource, nil
}

// staticFiles is where the web app comes from. Tests replace it, so that they do not depend on whether
// web/dist was built.
var staticFiles = web.Dist

func serve() error {
	c, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	log := newLogger(c, os.Stdout)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveWith(ctx, c, log, nil)
}

// serveWith runs the server until ctx ends (SIGTERM in production). ready, if
// set, is called with the address once the server accepts connections.
func serveWith(ctx context.Context, c config, log *slog.Logger, ready func(addr string)) error {
	db, err := openData(c, log)
	if err != nil {
		return err
	}
	defer db.Close()

	authSvc := auth.New(db, auth.Config{TrustedProxies: c.TrustedProxies, Logger: log})
	if err := authSvc.InitSetup(ctx); err != nil {
		return fmt.Errorf("prepare the setup: %w", err)
	}

	jdURL, source, err := chooseURL(db, c, log)
	if err != nil {
		return err
	}
	opts := []jd.Option{jd.WithUserAgent("JDWeb/" + version)}
	mirror, hub := state.New(), sse.New()
	engine := jdsync.New(jd.New(jdURL, opts...), mirror, hub, jdsync.Config{Logger: log, ClientOptions: opts})

	own, err := diag.OwnPrefixes()
	if err != nil {
		log.Warn("cannot read the network interfaces, the setup check skips the subnet hint", "err", err)
	}
	apiSrv := api.New(api.Options{
		Engine: engine, Store: mirror, Hub: hub, Auth: authSvc, DB: db, Logger: log, Version: version,
		Static: staticFiles(), OwnPrefixes: own, URLSource: c.JDURLSource,
	})

	ln, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", c.Listen, err)
	}
	httpSrv := apiSrv.HTTPServer(c.Listen)
	httpSrv.RegisterOnShutdown(apiSrv.CloseStreams)

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		if err := engine.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("the sync engine stopped", "err", err)
		}
	}()
	go housekeeping(runCtx, db, log)

	serveErr := make(chan error, 1)
	go func() {
		log.Info("JDWeb started", "version", version, "listen", c.Listen, "data", c.DataDir, "jd_url", jdURL, "jd_url_source", source)
		serveErr <- httpSrv.Serve(api.LimitListener(ln, maxConnections))
	}()
	if ready != nil {
		ready(ln.Addr().String())
	}

	select {
	case err := <-serveErr:
		stop()
		<-engineDone
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = httpSrv.Shutdown(sctx)
	if err != nil {
		_ = httpSrv.Close()
	}
	stop()
	<-engineDone
	return err
}

// maxConnections limits concurrent connections; every open browser tab holds one.
const maxConnections = 256

// housekeeping removes expired sessions once an hour.
func housekeeping(ctx context.Context, db *store.DB, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := db.DeleteExpiredSessions(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("cannot remove expired sessions", "err", err)
		} else if n > 0 {
			log.Debug("removed expired sessions", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// healthcheck asks the running server on its own port. Docker uses it.
func healthcheck(getenv func(string) string, out io.Writer) error {
	c, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	return checkHealth(healthURL(c.Listen), out)
}

func checkHealth(url string, out io.Writer) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("unhealthy: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("unhealthy: %s answered %d", url, resp.StatusCode)
	}
	fmt.Fprintln(out, "ok")
	return nil
}

// resetPassword prints a one-time token. The server picks it up from the
// database: the setup page then takes it instead of the setup token.
func resetPassword(getenv func(string) string, out io.Writer) error {
	c, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	log := newLogger(c, os.Stderr)
	// As root (docker exec in a container started with --user 0:0) the
	// database files must not end up owned by root: drop to PUID:PGID first.
	db, err := openData(c, log)
	if err != nil {
		return err
	}
	defer db.Close()
	svc := auth.New(db, auth.Config{Logger: log}) // no InitSetup: this is not the server
	tok, err := svc.ResetToken(context.Background())
	if err != nil {
		if isNoAccount(err) {
			return errors.New("there is no account yet. Start JDWeb and create it with the setup token from the log")
		}
		return err
	}
	fmt.Fprintf(out, "Reset token (valid for %s, works once):\n\n  %s\n\n", auth.ResetTokenValidFor().Round(time.Minute), tok)
	fmt.Fprintln(out, "Open JDWeb in the browser. It shows the setup page again: enter the token and a new password.")
	fmt.Fprintln(out, "The user name stays. Two-factor authentication is switched off and all sessions end.")
	return nil
}
