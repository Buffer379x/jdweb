package jd_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
)

func TestCallRequestShape(t *testing.T) {
	s := newStub(t, map[string]reply{"/device/ping": ok(t, "device_ping")})
	var got bool
	err := s.client(jd.WithUserAgent("JDWeb/test")).Call(context.Background(), "/device/ping", &got, 1, "a", true, []int64{2, 3}, map[string]any{"x": 1})
	if err != nil || !got {
		t.Fatalf("Call = %v, %v", got, err)
	}
	r := s.one(t)
	if r.Method != "POST" || r.Path != "/device/ping" {
		t.Errorf("request = %s %s", r.Method, r.Path)
	}
	if r.ContentType != "application/json" {
		t.Errorf("Content-Type = %q", r.ContentType)
	}
	if r.Origin != "" {
		t.Errorf("Origin = %q, JDownloader answers 403 to any request with an Origin", r.Origin)
	}
	if r.AcceptEncoding != "identity" {
		t.Errorf("Accept-Encoding = %q, JDownloader answers a gzip request with deflate that Go cannot read", r.AcceptEncoding)
	}
	if r.UserAgent != "JDWeb/test" {
		t.Errorf("User-Agent = %q", r.UserAgent)
	}
	// Real values, not stringified.
	if want := `{"params":[1,"a",true,[2,3],{"x":1}]}`; string(r.Body) != want {
		t.Errorf("body = %s, want %s", r.Body, want)
	}
}

func TestCallWithoutParamsSendsEmptyArray(t *testing.T) {
	s := newStub(t, map[string]reply{"/device/ping": ok(t, "device_ping")})
	if err := s.client().Call(context.Background(), "/device/ping", nil); err != nil {
		t.Fatal(err)
	}
	if got := string(s.one(t).Body); got != `{"params":[]}` {
		t.Errorf("body = %s", got)
	}
}

func TestVoidAnswersLeaveOutUntouched(t *testing.T) {
	for _, name := range []string{"void_ok", "config_get_missing", "dl_stopmarked_link_none"} {
		t.Run(name, func(t *testing.T) {
			s := newStub(t, map[string]reply{"/x": ok(t, name)})
			out := []string{"keep"}
			if err := s.client().Call(context.Background(), "/x", &out); err != nil {
				t.Fatal(err)
			}
			if len(out) != 1 || out[0] != "keep" {
				t.Errorf("out = %v", out)
			}
		})
	}
	t.Run("null", func(t *testing.T) {
		s := newStub(t, map[string]reply{"/x": {200, []byte(`{"data":null}`)}})
		var n int64 = 7
		if err := s.client().Call(context.Background(), "/x", &n); err != nil || n != 7 {
			t.Errorf("n = %d, err = %v", n, err)
		}
	})
}

func TestErrorAnswers(t *testing.T) {
	tests := []struct {
		fixture    string
		status     int
		wantType   string
		wantSrc    string
		check      func(error) bool
		rawContain string
	}{
		{"err_bad_parameters", 400, "BAD_PARAMETERS", "DEVICE", nil, `"data" : "1"`},
		{"err_no_such_function_400", 400, "BAD_PARAMETERS", "DEVICE", nil, "/downloadsV2/removeLinks"},
		{"err_404_not_found", 404, "FILE_NOT_FOUND", "DEVICE", jd.IsNotFound, ""},
		{"err_403_bad_origin", 403, "AUTH_FAILED", "DEVICE", nil, "Bad Origin"},
		{"err_dialog_invalid_id", 400, "Invalid ID: 0", "DEVICE", nil, ""},
		{"err_dialog_answer_first", 400, "Answer Dialog 1793566158196 first", "DEVICE", nil, ""},
		{"err_501_empty", 501, "JD_NOT_IMPLEMENTED", "", jd.IsNotImplemented, ""},
		{"err_500_stacktrace", 500, "JD_INTERNAL_ERROR", "", jd.IsInternal, "java.lang.RuntimeException"},
		{"err_500_json", 500, "JD_INTERNAL_ERROR", "DEVICE", jd.IsInternal, "INTERNAL_SERVER_ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			s := newStub(t, map[string]reply{"/p": {tc.status, fixture(t, tc.fixture)}})
			var out []string
			err := s.client().Call(context.Background(), "/p", &out)
			var e *jd.Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v, want *jd.Error", err)
			}
			if e.Status != tc.status || e.Type != tc.wantType || e.Src != tc.wantSrc {
				t.Errorf("Error = {Status:%d Src:%q Type:%q}, want {%d %q %q}", e.Status, e.Src, e.Type, tc.status, tc.wantSrc, tc.wantType)
			}
			if !strings.Contains(e.Raw, tc.rawContain) {
				t.Errorf("Raw = %q, want it to contain %q", e.Raw, tc.rawContain)
			}
			if tc.check != nil && !tc.check(err) {
				t.Errorf("helper did not recognise %v", err)
			}
			if jd.IsConnection(err) {
				t.Error("IsConnection is true for an answer of JDownloader")
			}
			if e.Path != "/p" {
				t.Errorf("Path = %q", e.Path)
			}
		})
	}
}

func TestInternalErrorMessageHasNoStacktrace(t *testing.T) {
	s := newStub(t, map[string]reply{"/p": {500, fixture(t, "err_500_stacktrace")}})
	err := s.client().Call(context.Background(), "/p", nil)
	var e *jd.Error
	if !errors.As(err, &e) {
		t.Fatal(err)
	}
	if !strings.Contains(e.Raw, "at org.appwork") {
		t.Fatalf("Raw lost the stacktrace: %q", e.Raw)
	}
	if msg := err.Error(); strings.Contains(msg, "org.appwork") || strings.Contains(msg, "\n") {
		t.Errorf("Error() = %q, the stacktrace belongs into the log only", msg)
	}
	if len(e.Raw) > 4200 {
		t.Errorf("Raw has %d bytes, should be cut", len(e.Raw))
	}
}

func TestOtherServiceOnThePort(t *testing.T) {
	html := []byte("<html><body>Welcome to nginx</body></html>")
	tests := map[string]reply{
		"html 200":  {200, html},
		"html 404":  {404, html},
		"html 401":  {401, html},
		"empty 200": {200, nil},
		"json 200":  {200, []byte(`[1,2]`)},
	}
	for name, rep := range tests {
		t.Run(name, func(t *testing.T) {
			s := newStub(t, map[string]reply{"/device/ping": rep})
			var ok bool
			err := s.client().Call(context.Background(), "/device/ping", &ok)
			if !jd.IsInvalidResponse(err) {
				t.Errorf("err = %v, want JD_INVALID_RESPONSE", err)
			}
			if jd.IsConnection(err) || jd.IsNotFound(err) {
				t.Errorf("a foreign service must not look like a JDownloader error: %v", err)
			}
		})
	}
	t.Run("redirect is not followed", func(t *testing.T) {
		other := httptest.NewServer(http.NotFoundHandler())
		defer other.Close()
		srv := httptest.NewServer(http.RedirectHandler(other.URL, http.StatusFound))
		defer srv.Close()
		err := jd.New(srv.URL).Call(context.Background(), "/device/ping", nil)
		if !jd.IsInvalidResponse(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestConnectionErrors(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		srv := httptest.NewServer(nil)
		url := srv.URL
		srv.Close()
		err := jd.New(url).Call(context.Background(), "/device/ping", nil)
		if !jd.IsConnection(err) {
			t.Fatalf("err = %v, want a connection error", err)
		}
		var e *jd.Error
		if !errors.As(err, &e) || e.Status != 0 || e.Type != jd.TypeConnection {
			t.Errorf("Error = %+v", e)
		}
	})
	t.Run("connection dropped", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		defer srv.Close()
		if err := jd.New(srv.URL).Call(context.Background(), "/device/ping", nil); !jd.IsConnection(err) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("caller cancels", func(t *testing.T) {
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body) // the server notices a closed connection only after the body is read
			close(started)
			<-r.Context().Done()
		}))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-started; cancel() }()
		err := jd.New(srv.URL).Call(ctx, "/device/ping", nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if jd.IsConnection(err) {
			t.Error("a cancelled call is not a lost connection")
		}
	})
}

func TestTimeLimits(t *testing.T) {
	c := jd.New("http://127.0.0.1:1")
	tests := map[string]time.Duration{
		"/device/ping":                      10 * time.Second,
		"/downloadsV2/queryLinks":           10 * time.Second,
		"/events/listen":                    30 * time.Second,
		"/linkgrabberv2/addLinks":           60 * time.Second,
		"/linkgrabberv2/addContainer":       60 * time.Second,
		"/linkgrabberv2/moveToDownloadlist": 10 * time.Second,
	}
	for path, want := range tests {
		if got := c.TimeoutFor(path); got != want {
			t.Errorf("TimeoutFor(%s) = %v, want %v", path, got, want)
		}
	}
	if jd.DefaultTimeout != 10*time.Second || jd.DefaultPollTimeout != 30*time.Second || jd.DefaultBulkTimeout != 60*time.Second {
		t.Error("default constants changed")
	}

	custom := jd.New("http://127.0.0.1:1", jd.WithTimeouts(jd.Timeouts{Normal: time.Second, Poll: 2 * time.Second}))
	if custom.TimeoutFor("/device/ping") != time.Second || custom.TimeoutFor("/events/listen") != 2*time.Second || custom.TimeoutFor("/linkgrabberv2/addLinks") != 60*time.Second {
		t.Error("WithTimeouts does not override per field")
	}
}

func TestTimeLimitIsAppliedPerPath(t *testing.T) {
	slow := func(d time.Duration) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-time.After(d):
				_, _ = w.Write([]byte(`{"data":true}`))
			case <-r.Context().Done():
			}
		}
	}
	srv := httptest.NewServer(slow(300 * time.Millisecond))
	defer srv.Close()
	c := jd.New(srv.URL, jd.WithTimeouts(jd.Timeouts{Normal: 60 * time.Millisecond, Poll: 2 * time.Second, Bulk: 2 * time.Second}))

	start := time.Now()
	err := c.Call(context.Background(), "/device/ping", nil)
	if !jd.IsConnection(err) {
		t.Fatalf("normal call: err = %v, want a timeout as connection error", err)
	}
	if d := time.Since(start); d > 250*time.Millisecond {
		t.Errorf("normal call took %v, the limit did not apply", d)
	}
	for _, path := range []string{"/events/listen", "/linkgrabberv2/addLinks"} {
		if err := c.Call(context.Background(), path, nil); err != nil {
			t.Errorf("%s: err = %v, the longer limit did not apply", path, err)
		}
	}
}

func TestMissingFieldsAreZeroValues(t *testing.T) {
	s := newStub(t, map[string]reply{"/downloadsV2/queryLinks": ok(t, "dl_query_links_running")})
	links, err := s.client().DownloadLinks(context.Background(), jd.AllDownloadLinkFields())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]jd.DownloadLink{}
	for _, l := range links {
		byName[l.Name] = l
	}
	if len(byName) != 4 {
		t.Fatalf("got %d links", len(byName))
	}
	fin := byName["archiv.zip"]
	if !fin.Finished || fin.Running || fin.Skipped || !fin.Enabled {
		t.Errorf("finished link = %+v", fin)
	}
	run := byName["big1.bin"]
	if run.Finished || !run.Running || run.Skipped || run.FinishedDate != 0 || run.ExtractionStatus != "" {
		t.Errorf("running link = %+v", run)
	}
	if run.UUID != 1793563980901 || run.PackageUUID != 1793564007950 || run.BytesTotal != 1048576 || run.Priority != jd.PriorityHigh {
		t.Errorf("running link ids/size = %+v", run)
	}
	if run.AdvancedStatus["PluginProgress"].Total != 1048576 {
		t.Errorf("AdvancedStatus = %+v", run.AdvancedStatus)
	}
}

func TestPackageETAUnknown(t *testing.T) {
	s := newStub(t, map[string]reply{"/downloadsV2/queryPackages": ok(t, "dl_query_packages_eta_unknown")})
	pkgs, err := s.client().DownloadPackages(context.Background(), jd.AllDownloadPackageFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatal(pkgs)
	}
	if pkgs[0].ETA != -2147483648 {
		t.Errorf("ETA = %d", pkgs[0].ETA)
	}
	if jd.ETAUnknown != -2147483648 {
		t.Error("ETAUnknown")
	}
	if _, known := jd.ETASeconds(pkgs[0].ETA); known {
		t.Error("eta=-2147483648 must count as unknown")
	}
	if _, known := jd.ETASeconds(-1); known {
		t.Error("eta=-1 must count as unknown")
	}
	if sec, known := jd.ETASeconds(193); !known || sec != 193 {
		t.Error("eta=193")
	}
	if sec, known := jd.ETASeconds(0); !known || sec != 0 {
		t.Error("eta=0 is known")
	}
	// A package that was not started has no eta field at all and no running flag.
	if p := pkgs[1]; p.Running || p.Finished || p.ETA != 0 || p.Name != "Pkg2" || !p.Enabled {
		t.Errorf("idle package = %+v", p)
	}
}

func TestStatusTextIsPlainText(t *testing.T) {
	s := newStub(t, map[string]reply{"/downloadsV2/queryLinks": ok(t, "dl_query_links_running")})
	links, err := s.client().DownloadLinks(context.Background(), jd.AllDownloadLinkFields())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		if l.Name == "off.bin" {
			if !strings.HasPrefix(l.Status, "Network problem:") || !strings.Contains(l.Status, "\n") {
				t.Errorf("status = %q", l.Status)
			}
			return
		}
	}
	t.Fatal("off.bin missing")
}

func TestSkipReasonFromAdvancedStatus(t *testing.T) {
	s := newStub(t, map[string]reply{"/downloadsV2/queryLinks": ok(t, "dl_query_links_skipped")})
	links, err := s.client().DownloadLinks(context.Background(), jd.AllDownloadLinkFields())
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	l := links[0]
	if !l.Skipped || l.AdvancedStatus.SkipReason() != jd.SkipReasonDiskFull {
		t.Errorf("link = %+v", l)
	}
	if (jd.AdvancedStatus{}).SkipReason() != "" {
		t.Error("no skip reason expected")
	}
}

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{
		"http://192.168.1.5:3128":    "http://192.168.1.5:3128",
		"http://192.168.1.5:3128/":   "http://192.168.1.5:3128",
		"http://192.168.1.5:3128///": "http://192.168.1.5:3128",
		"192.168.1.5:3128":           "http://192.168.1.5:3128",
		"  jd.local:3128/  ":         "http://jd.local:3128",
		"jd.local":                   "http://jd.local",
		"localhost:3128":             "http://localhost:3128",
		"HTTP://JD.Local:3128":       "http://jd.local:3128",
		"https://jd.example.org/":    "https://jd.example.org",
		"https://example.org/jd/":    "https://example.org/jd",
		"http://[::1]:3128/":         "http://[::1]:3128",
		"[::1]:3128":                 "http://[::1]:3128",
		"http://jdownloader:3128":    "http://jdownloader:3128",
		"http://127.0.0.1:13128":     "http://127.0.0.1:13128",
	}
	for in, want := range good {
		got, err := jd.NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "   ", "http://", "://host", "ftp://host:3128", "file:///etc/passwd", "javascript:alert(1)",
		"http://host:99999", "http://host:0", "http://host:abc", "http://exa mple.com", "http://host:3128/?a=b",
		"http://host:3128#frag", "http://user:pw@host:3128", "http:///path",
	}
	for _, in := range bad {
		got, err := jd.NormalizeURL(in)
		if err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
			continue
		}
		if !errors.Is(err, jd.ErrInvalidURL) {
			t.Errorf("NormalizeURL(%q) error does not wrap ErrInvalidURL: %v", in, err)
		}
	}
}

// A JSON answer that does not fit the expected shape can hold a secret (a
// password, an API key): the body is not kept, so it cannot reach a log.
func TestShapeMismatchDoesNotKeepTheBody(t *testing.T) {
	s := newStub(t, map[string]reply{"/x": {200, []byte(`{"data":{"password":"hunter2-secret"}}`)}})
	var out int
	err := s.client().Call(context.Background(), "/x", &out)
	if !jd.IsInvalidResponse(err) {
		t.Fatalf("err = %v, want JD_INVALID_RESPONSE", err)
	}
	var e *jd.Error
	if !errors.As(err, &e) {
		t.Fatalf("not a *jd.Error: %v", err)
	}
	for _, text := range []string{e.Raw, err.Error(), fmt.Sprint(errors.Unwrap(err))} {
		if strings.Contains(text, "hunter2") {
			t.Errorf("the body leaked into %q", text)
		}
	}
}
