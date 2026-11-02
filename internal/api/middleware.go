package api

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
)

// contentSecurityPolicy is the policy of Concept 9.3, character for character.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// securityHeaders puts the headers of Concept 9.3 on every response, errors
// and static files included. API answers are never cached unless a handler
// says otherwise.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if isAPI(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// recorder remembers status and size for the access log. It passes Flush and
// the ResponseController through, which event streams need.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// accessLog logs one line per request at debug level: method, path, status,
// size and time. Never the query, the headers or the body.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"bytes", rec.bytes, "ms", time.Since(start).Milliseconds(), "ip", s.auth.ClientIP(r))
	})
}

// recoverPanics turns a panic in a handler into a 500 answer and a log line.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil || rec == http.ErrAbortHandler {
				if rec != nil {
					panic(rec)
				}
				return
			}
			s.log.Error("panic in a handler", "path", r.URL.Path, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			writeError(w, newError(http.StatusInternalServerError, codeInternal, "Internal error."))
		}()
		next.ServeHTTP(w, r)
	})
}

// Time allowed for a big upload and for the answer after it.
var (
	bigReadWindow  = 2 * time.Minute
	bigWriteWindow = 2 * time.Minute
)

// bodyLimit caps the request body. Reading more fails with *http.MaxBytesError.
func bodyLimit(n int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > n {
			writeError(w, newError(http.StatusRequestEntityTooLarge, codeBadRequest, "The request is larger than %d bytes.", n))
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, n)
		}
		if n > defaultBodyLimit {
			// Big uploads may need longer than the server's read timeout, and the
			// write deadline of the server starts with the request: a slow upload
			// would leave no time to answer, the browser would not see the result
			// and would send it again (links added twice). So both move: the
			// body gets two minutes, the answer another two (JDownloader may
			// take a minute to collect).
			rc := http.NewResponseController(w)
			_ = rc.SetReadDeadline(time.Now().Add(bigReadWindow))
			_ = rc.SetWriteDeadline(time.Now().Add(bigReadWindow + bigWriteWindow))
		}
		next.ServeHTTP(w, r)
	})
}
