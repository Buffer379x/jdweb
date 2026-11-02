// Package api is the REST and server-sent-events interface of JDWeb as
// described in docs/api.md. It wires the state mirror, the sync engine, the
// event hub, the account service and the database to HTTP routes. The browser
// can only reach what a route here offers: there is no way to call an arbitrary
// JDownloader path.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
	"github.com/Buffer379x/jdweb/internal/store"
	jdsync "github.com/Buffer379x/jdweb/internal/sync"
)

// Body limits: JSON routes take 1 MiB, adding links and containers 64 MiB.
const (
	defaultBodyLimit = 1 << 20
	addLinksLimit    = 8 << 20
	containerLimit   = 64 << 20
	prefsLimit       = 64 << 10
)

// DefaultKeepalive is how often an idle event stream gets a comment line.
const DefaultKeepalive = 20 * time.Second

// Setting keys in the database.
const (
	settingJDURL        = "jd.url"
	settingAcknowledged = "diag.acknowledged"
	settingPrefs        = "ui.prefs"
)

// Options are the parts the API is made of.
type Options struct {
	Engine  *jdsync.Engine
	Store   *state.Store
	Hub     *sse.Hub
	Auth    *auth.Service
	DB      *store.DB
	Logger  *slog.Logger
	Version string
	// Static holds the built web app (web/dist). Nil serves no web app.
	Static fs.FS
	// OwnPrefixes are the subnets of this machine for the setup check.
	OwnPrefixes []netip.Prefix
	// URLSource says where the address at start came from when the user saved
	// none: "env" or "default".
	URLSource string
	// Keepalive overrides DefaultKeepalive (tests).
	Keepalive time.Duration
}

// Server is the HTTP side of JDWeb.
type Server struct {
	engine  *jdsync.Engine
	mirror  *state.Store
	hub     *sse.Hub
	auth    *auth.Service
	db      *store.DB
	log     *slog.Logger
	version string
	static  fs.FS
	own     []netip.Prefix
	source  string
	keep    time.Duration

	routes  []Route
	icons   *iconCache
	done    chan struct{}
	endOnce sync.Once

	revMu sync.Mutex
	revCh chan struct{} // closed when sessions were revoked, then replaced

	// setURL switches the engine; tests replace it.
	setURL func(string) error
}

// Route is one registered API route.
type Route struct {
	Method string
	Path   string // the full path, parameters as {name}
	Public bool   // reachable without a session

	handler http.HandlerFunc
	limit   int64 // body limit in bytes, 0 means defaultBodyLimit
}

// Pattern returns the route as ServeMux pattern.
func (r Route) Pattern() string { return r.Method + " " + r.Path }

// New returns the server. Options.Engine, Store, Hub, Auth and DB are required.
func New(o Options) *Server {
	s := &Server{
		engine: o.Engine, mirror: o.Store, hub: o.Hub, auth: o.Auth, db: o.DB, log: o.Logger, version: o.Version,
		static: o.Static, own: o.OwnPrefixes, source: o.URLSource, keep: o.Keepalive,
		icons: newIconCache(iconCacheEntries, iconCacheBytes), done: make(chan struct{}), revCh: make(chan struct{}),
		setURL: o.Engine.SetURL,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.keep <= 0 {
		s.keep = DefaultKeepalive
	}
	if s.source == "" {
		s.source = "default"
	}
	s.routes = s.buildRoutes()
	return s
}

// Routes returns every route of the API (not the web app files).
func (s *Server) Routes() []Route { return append([]Route(nil), s.routes...) }

// CloseStreams ends all open event streams and refuses new ones. Call it when
// the HTTP server shuts down, otherwise a stream keeps Shutdown waiting.
func (s *Server) CloseStreams() { s.endOnce.Do(func() { close(s.done) }) }

// sessionsChanged tells the open event streams that sessions were revoked
// (logout, sign out others, password change, reset). Each stream then checks
// its own session and ends if it is gone.
func (s *Server) sessionsChanged() {
	s.revMu.Lock()
	close(s.revCh)
	s.revCh = make(chan struct{})
	s.revMu.Unlock()
}

func (s *Server) revoked() <-chan struct{} {
	s.revMu.Lock()
	defer s.revMu.Unlock()
	return s.revCh
}

// Handler returns the handler for the whole site.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.routes {
		mux.Handle(rt.Pattern(), s.chain(rt))
	}
	mux.HandleFunc("/", s.handleFallback)
	var h http.Handler = mux
	h = s.accessLog(h)
	h = s.recoverPanics(h)
	return securityHeaders(h)
}

// chain wraps a route: session lookup, 401 for private routes, the Origin and
// header check for everything that changes state, the body limit.
func (s *Server) chain(rt Route) http.Handler {
	limit := rt.limit
	if limit == 0 {
		limit = defaultBodyLimit
	}
	var h http.Handler = rt.handler
	h = bodyLimit(limit, h)
	h = s.auth.RequireMutation(h)
	if !rt.Public {
		h = s.auth.RequireAuth(h)
	}
	return s.auth.Middleware(h)
}

// isAPI reports whether a path belongs to the API and not to the web app.
func isAPI(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// client returns the client of the current JDownloader address.
func (s *Server) client() (*jd.Client, error) {
	if c := s.engine.Client(); c != nil {
		return c, nil
	}
	return nil, errNoJD
}

// HTTPServer returns the http.Server for the handler: time limits against slow
// clients, and a write limit of 60 s that is longer than the throttle of a
// login under pressure (5 s) and shorter than forever. The event stream sets
// its own deadlines per write, see handleEvents.
func (s *Server) HTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr: addr, Handler: s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}
