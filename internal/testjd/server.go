// Package testjd is a controllable fake of the JDownloader local API for
// tests. It speaks the same wire protocol as JDownloader (POST with
// {"params":[...]}, answers {"data":...}, error objects, HTTP 501 for unknown
// paths, 403 for any request with an Origin header) and keeps real state:
// packages and links in the download list and the linkgrabber, event
// subscriptions with long-poll, captchas, dialogs and the download controller.
//
// It does not download anything. Tests move the state forward themselves with
// the mutators (AddPackage, UpdateLink, ...) and announce it with Emit.
package testjd

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Call is one request the server received.
type Call struct {
	Path        string
	Params      json.RawMessage // the "params" array as sent, "[]" if none
	Method      string
	Origin      string // the Origin header, a real JDownloader refuses requests that have one
	ContentType string
	Status      int // the HTTP status that was answered
}

// Args decodes the params array.
func (c Call) Args() []json.RawMessage {
	var a []json.RawMessage
	_ = json.Unmarshal(c.Params, &a)
	return a
}

type failure struct {
	path   string
	status int
	body   string
	hang   bool
}

// Server is a fake JDownloader. Create it with New or Start.
type Server struct {
	// URL is the base URL of the API, "http://127.0.0.1:port".
	URL string

	ts      *httptest.Server
	ln      net.Listener
	closing chan struct{}

	mu         sync.Mutex
	idSeq      int64
	lists      [2]listState
	calls      []Call
	failures   []failure
	down       bool
	pollWait   time.Duration
	subs       map[int64]*sub
	controller string
	stopMark   int64
	toggles    toggles
	config     map[string]any
	defaults   map[string]any
	captchas   []CaptchaJob
	dialogs    []Dialog
	revision   int64
	notDocker  bool
	archives   []Archive
}

type toggles struct {
	premium, clipboard, reconnect, stopAfter bool
}

// Start returns a running fake. Close it when done.
func Start() *Server {
	s := &Server{
		closing:    make(chan struct{}),
		pollWait:   25 * time.Second,
		subs:       map[int64]*sub{},
		controller: "IDLE",
		stopMark:   -1,
		toggles:    toggles{premium: true, clipboard: true, reconnect: true},
		config:     defaultConfig(),
		defaults:   defaultConfig(),
		revision:   50639,
	}
	for i := range s.lists {
		s.lists[i].counter = s.nextID() // JDownloader's counters are never 0
	}
	s.ts = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	s.ts.Start()
	s.ln = s.ts.Listener
	s.URL = s.ts.URL
	return s
}

// New starts a fake and closes it at the end of the test.
func New(t testing.TB) *Server {
	t.Helper()
	s := Start()
	t.Cleanup(s.Close)
	return s
}

// Close stops the server and ends all waiting listens.
func (s *Server) Close() {
	select {
	case <-s.closing:
	default:
		close(s.closing)
	}
	s.mu.Lock()
	for id, sb := range s.subs {
		sb.close()
		delete(s.subs, id)
	}
	s.mu.Unlock()
	s.ts.Close()
}

// SetPollTimeout sets how long events/listen waits for an event before it
// answers with an empty list. The default is 25 s like JDownloader.
func (s *Server) SetPollTimeout(d time.Duration) {
	s.mu.Lock()
	s.pollWait = d
	s.mu.Unlock()
}

// FailNext makes the next call to path answer with the given HTTP status and
// body instead of its normal answer. Several calls queue up and are used in
// order. A body that starts with "{" is sent as JSON, anything else as text.
func (s *Server) FailNext(path string, status int, body string) {
	s.mu.Lock()
	s.failures = append(s.failures, failure{path: path, status: status, body: body})
	s.mu.Unlock()
}

// SetDocker says whether system/getSystemInfos reports docker:true, which is
// the default like on the real test instance.
func (s *Server) SetDocker(docker bool) {
	s.mu.Lock()
	s.notDocker = !docker
	s.mu.Unlock()
}

// HangNext makes the next call to path never answer: it waits until the
// client gives up or the server closes. JDownloader does this for
// toolbar/togglePremium when premium is switched off and a confirmation dialog
// opens. The call shows up in the call log when the wait has ended.
func (s *Server) HangNext(path string) {
	s.mu.Lock()
	s.failures = append(s.failures, failure{path: path, hang: true})
	s.mu.Unlock()
}

// Down makes the server unreachable: open connections are closed and new ones
// are refused, like JDownloader while it restarts. State is kept.
func (s *Server) Down() {
	s.mu.Lock()
	if s.down {
		s.mu.Unlock()
		return
	}
	s.down = true
	s.mu.Unlock()
	_ = s.ln.Close()
	s.ts.CloseClientConnections()
}

// Up makes a server that is Down reachable again on the same address.
func (s *Server) Up() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.down {
		return
	}
	addr := strings.TrimPrefix(s.URL, "http://")
	var ln net.Listener
	var err error
	for i := 0; i < 50; i++ { // the port may need a moment to become free
		if ln, err = net.Listen("tcp", addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		panic("testjd: cannot listen again on " + addr + ": " + err.Error())
	}
	s.ln = ln
	s.ts.Listener = ln
	s.down = false
	go func() { _ = s.ts.Config.Serve(ln) }()
}

// DropSubscription forgets every event subscription, as JDownloader does when
// it restarts: the next listen fails with 404 and getsubscriptionstatus
// answers subscriptionid -1.
func (s *Server) DropSubscription() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sb := range s.subs {
		sb.close()
		delete(s.subs, id)
	}
}

// Calls returns the calls received since the start or the last ResetCalls.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// CallsTo returns the calls to one path.
func (s *Server) CallsTo(path string) []Call {
	var out []Call
	for _, c := range s.Calls() {
		if c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

// ResetCalls clears the call log.
func (s *Server) ResetCalls() {
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
}

// serve is the http handler.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	call := Call{Path: r.URL.Path, Method: r.Method, Origin: r.Header.Get("Origin"), ContentType: r.Header.Get("Content-Type"), Params: json.RawMessage("[]")}

	status, ctype, out := s.answer(r.Context(), r, body, &call)
	call.Status = status
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()

	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

func (s *Server) answer(ctx context.Context, r *http.Request, body []byte, call *Call) (int, string, []byte) {
	if call.Origin != "" {
		return errorObject(http.StatusForbidden, "AUTH_FAILED", "Bad Origin")
	}
	if f, ok := s.takeFailure(call.Path); ok {
		if f.hang {
			select {
			case <-ctx.Done():
			case <-s.closing:
			}
			return http.StatusServiceUnavailable, "text/plain", nil
		}
		ct := "text/plain; charset=UTF-8"
		if strings.HasPrefix(f.body, "{") {
			ct = "application/json"
		}
		return f.status, ct, []byte(f.body)
	}
	h, ok := handlers[call.Path]
	if !ok {
		return http.StatusNotImplemented, "", nil
	}
	var req struct {
		Params []json.RawMessage `json:"params"`
	}
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, "text/plain", []byte("POST only")
	}
	if err := json.Unmarshal(body, &req); err != nil {
		// JDownloader answers a body it cannot parse with a Java stacktrace as text.
		return http.StatusInternalServerError, "text; charset=UTF-8", []byte("java.lang.RuntimeException: " + err.Error() + "\n\tat org.appwork.remoteapi.RemoteAPI.parseParameters(RemoteAPI.java:479)")
	}
	if req.Params != nil {
		call.Params = mustMarshal(req.Params)
	}
	if !h.blocking {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	res, aerr := h.fn(ctx, s, args(req.Params))
	if aerr != nil {
		return errorObject(aerr.status, aerr.typ, aerr.data)
	}
	if b, ok := res.(binaryResult); ok {
		return http.StatusOK, b.contentType, b.data
	}
	return http.StatusOK, "application/json", mustMarshal(map[string]any{"data": res})
}

func (s *Server) takeFailure(path string) (failure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.failures {
		if f.path == path {
			s.failures = append(s.failures[:i], s.failures[i+1:]...)
			return f, true
		}
	}
	return failure{}, false
}

func errorObject(status int, typ string, data any) (int, string, []byte) {
	return status, "application/json", mustMarshal(map[string]any{"src": "DEVICE", "type": typ, "data": data})
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("testjd: " + err.Error())
	}
	return b
}
