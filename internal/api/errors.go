package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/store"
)

// Error codes of docs/api.md.
const (
	codeUnauthorized = "unauthorized"
	codeTOTPRequired = "totp_required"
	codeForbidden    = "forbidden"
	codeBadRequest   = "bad_request"
	codeNotFound     = "not_found"
	codeConflict     = "conflict"
	codeRateLimited  = "rate_limited"
	codeJDOffline    = "jd_offline"
	codeJDError      = "jd_error"
	codeInternal     = "internal"
)

// apiError is an error with the answer it maps to.
type apiError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func newError(status int, code, format string, args ...any) *apiError {
	return &apiError{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func badRequest(format string, args ...any) *apiError {
	return newError(http.StatusBadRequest, codeBadRequest, format, args...)
}

func notFound(format string, args ...any) *apiError {
	return newError(http.StatusNotFound, codeNotFound, format, args...)
}

// Messages of JDownloader failures. They never repeat what JDownloader said:
// that text can hold stacktraces and input the user typed.
const (
	msgOffline  = "JDownloader does not answer. Check that it is running and that the address in the connection settings is right."
	msgOther    = "The address answers, but not like JDownloader: another service seems to run there. Check the address and the port in the connection settings."
	msgGone     = "JDownloader no longer has this entry. It may have been removed or finished meanwhile; the list is being refreshed."
	msgInternal = "JDownloader reported an internal error while handling the request."
	msgRefused  = "JDownloader did not accept the request."
	msgNoURL    = "No JDownloader address is set. Enter it in the connection settings."
	msgBusy     = "Too many password checks are running. Try again in a moment."
)

// errNoJD is returned when no JDownloader address is set.
var errNoJD = newError(http.StatusServiceUnavailable, codeJDOffline, "%s", msgNoURL)

// toAPIError maps any error to the answer for the browser. The second result
// is the error to log, nil for the expected ones.
func toAPIError(err error) (*apiError, error) {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae, nil
	}
	var rl *auth.RateLimitError
	var mb *http.MaxBytesError
	var je *jd.Error
	switch {
	case errors.As(err, &rl):
		return &apiError{Status: http.StatusTooManyRequests, Code: codeRateLimited, RetryAfter: rl.RetryAfter,
			Message: "Too many failed attempts. Wait before trying again."}, nil
	case errors.Is(err, auth.ErrBusy):
		return &apiError{Status: http.StatusTooManyRequests, Code: codeRateLimited, RetryAfter: time.Second, Message: msgBusy}, nil
	case errors.As(err, &mb):
		return newError(http.StatusRequestEntityTooLarge, codeBadRequest, "The request is larger than %d bytes.", mb.Limit), nil

	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrNoAccount):
		return newError(http.StatusUnauthorized, codeUnauthorized, "Username, password or code is wrong."), nil
	case errors.Is(err, auth.ErrTOTPRequired):
		return newError(http.StatusUnauthorized, codeTOTPRequired, "A two-factor code is required."), nil
	case errors.Is(err, auth.ErrWrongPassword):
		// Not 401: the browser would take it for an expired session.
		return newError(http.StatusForbidden, codeForbidden, "The password is wrong."), nil
	case errors.Is(err, auth.ErrInvalidToken):
		return badRequest("The setup token is wrong."), nil
	case errors.Is(err, auth.ErrSetupClosed):
		return newError(http.StatusConflict, codeConflict, "Setup is already completed."), nil
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrNoEnrollment):
		return badRequest("%s.", capitalize(err.Error())), nil
	case errors.Is(err, auth.ErrTOTPEnabled), errors.Is(err, auth.ErrTOTPNotEnabled):
		return newError(http.StatusConflict, codeConflict, "%s.", capitalize(err.Error())), nil

	case errors.Is(err, jd.ErrInvalidURL):
		return badRequest("%s.", capitalize(err.Error())), nil
	case errors.Is(err, jd.ErrRejected):
		return badRequest("JDownloader refused the value. It has the wrong type, or the setting does not exist."), nil
	case errors.Is(err, store.ErrNotFound):
		return notFound("Not found."), nil

	case errors.As(err, &je):
		return jdError(je), err
	// A plain context error is not a JDownloader that does not answer: the jd
	// client reports its own timeouts as *jd.Error (above). Here the browser
	// left, or a database call ran out of time.
	case errors.Is(err, context.Canceled):
		return newError(http.StatusServiceUnavailable, codeInternal, "The request was cancelled."), nil
	case errors.Is(err, context.DeadlineExceeded):
		return newError(http.StatusInternalServerError, codeInternal, "Internal error: an operation took too long."), err
	}
	return newError(http.StatusInternalServerError, codeInternal, "Internal error."), err
}

// jdError maps a failed call to JDownloader (Review Focus 1, 2 and 3: the
// messages differ by cause and say what to do).
func jdError(e *jd.Error) *apiError {
	switch {
	case e.Type == jd.TypeConnection:
		return newError(http.StatusServiceUnavailable, codeJDOffline, "%s", msgOffline)
	case e.Type == jd.TypeInvalidResponse, e.Type == jd.TypeNotImplemented:
		return newError(http.StatusBadGateway, codeJDError, "%s", msgOther)
	case strings.HasPrefix(e.Type, "Answer Dialog "):
		return newError(http.StatusConflict, codeConflict, "JDownloader asks its questions one after the other. Answer the first open question first.")
	case e.Status == http.StatusNotFound, strings.HasPrefix(e.Type, "Invalid ID"):
		return newError(http.StatusNotFound, codeJDError, "%s", msgGone)
	case e.Type == jd.TypeInternal:
		return newError(http.StatusBadGateway, codeJDError, "%s", msgInternal)
	}
	return newError(http.StatusBadGateway, codeJDError, "%s", msgRefused)
}

func capitalize(s string) string {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeError sends the error shape of docs/api.md.
func writeError(w http.ResponseWriter, e *apiError) {
	var b errorBody
	b.Error.Code, b.Error.Message = e.Code, e.Message
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if e.RetryAfter > 0 {
		secs := int((e.RetryAfter + time.Second - 1) / time.Second)
		h.Set("Retry-After", strconv.Itoa(max(secs, 1)))
	}
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(b)
}

// fail answers a request with the mapping of err and logs the unexpected ones.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	ae, logErr := toAPIError(err)
	if logErr != nil {
		s.log.Warn("request failed", "method", r.Method, "path", r.URL.Path, "status", ae.Status, "err", logErr)
	}
	writeError(w, ae)
}
