package jd_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// fixture reads a response recorded from the JDownloader test stack.
func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type reply struct {
	status int
	body   []byte
}

// binary reads a recorded binary response.
func binary(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ok is a 200 answer with a recorded body.
func ok(t testing.TB, name string) reply { return reply{200, fixture(t, name)} }

// void is the answer of a call without a result.
var void = reply{200, []byte(`{"data":""}`)}

type request struct {
	Method, Path, Origin, ContentType, UserAgent, AcceptEncoding string
	Body                                                         []byte
}

// stub is a JDownloader that answers fixed replies per path and records requests.
type stub struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]reply
	ctypes   map[string]string // Content-Type per path, JSON by default
	requests []request
}

func newStub(t testing.TB, routes map[string]reply) *stub {
	t.Helper()
	s := &stub{routes: routes, ctypes: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, request{r.Method, r.URL.Path, r.Header.Get("Origin"), r.Header.Get("Content-Type"), r.Header.Get("User-Agent"), r.Header.Get("Accept-Encoding"), body})
		rep, found := s.routes[r.URL.Path]
		ctype := s.ctypes[r.URL.Path]
		s.mu.Unlock()
		if !found {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if ctype == "" {
			ctype = "application/json"
		}
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(rep.status)
		_, _ = w.Write(rep.body)
	}))
	t.Cleanup(s.Close)
	return s
}

// one returns the single request the stub received.
func (s *stub) one(t testing.TB) request {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) != 1 {
		t.Fatalf("got %d requests, want 1: %+v", len(s.requests), s.requests)
	}
	return s.requests[0]
}

func (s *stub) client(opts ...jd.Option) *jd.Client { return jd.New(s.URL, opts...) }
