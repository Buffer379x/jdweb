// Package jd is the typed client for the JDownloader local ("deprecated") API.
//
// It is the only package that knows JDownloader API paths. Every request is a
// POST with the body {"params":[...]} and no Origin header (JDownloader answers
// any request that carries an Origin with 403). JDownloader leaves out false,
// zero and null fields in its JSON, so a missing field means the zero value.
package jd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Default time limits: normal calls, the events long-poll, large addLinks.
const (
	DefaultTimeout     = 10 * time.Second
	DefaultPollTimeout = 30 * time.Second
	DefaultBulkTimeout = 60 * time.Second
)

// maxBody caps how much of a response is read. A list of 10,000 links with all
// fields is about 3 MB, so this leaves a wide margin and still protects the
// memory target of the whole app.
const maxBody = 32 << 20

// rawLimit caps the response text kept in Error.Raw.
const rawLimit = 4096

// Timeouts are the per-call time limits of a Client.
type Timeouts struct {
	Normal time.Duration // every call without a special rule
	Poll   time.Duration // events/listen, which blocks up to 25 s
	Bulk   time.Duration // adding large link lists and containers
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client. Its Timeout should stay zero, the
// Client applies its own per-call limits through the request context.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.hc = h } }

// WithTimeouts overrides the per-call time limits. Zero fields keep their default.
func WithTimeouts(t Timeouts) Option {
	return func(c *Client) {
		if t.Normal > 0 {
			c.timeouts.Normal = t.Normal
		}
		if t.Poll > 0 {
			c.timeouts.Poll = t.Poll
		}
		if t.Bulk > 0 {
			c.timeouts.Bulk = t.Bulk
		}
	}
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// Client talks to one JDownloader. It is safe for concurrent use.
type Client struct {
	base      string
	hc        *http.Client
	timeouts  Timeouts
	userAgent string
}

// New returns a client for the base URL, for example "http://192.168.1.5:3128".
// Run user input through NormalizeURL first.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		base:      strings.TrimRight(baseURL, "/"),
		timeouts:  Timeouts{Normal: DefaultTimeout, Poll: DefaultPollTimeout, Bulk: DefaultBulkTimeout},
		userAgent: "JDWeb",
	}
	for _, o := range opts {
		o(c)
	}
	if c.hc == nil {
		c.hc = &http.Client{Transport: &http.Transport{
			Proxy:               nil, // JDownloader is on the local network, never go through a proxy
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		}}
	}
	// A redirect means this is not JDownloader. Report it instead of following it.
	hc := *c.hc
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.hc = &hc
	return c
}

// BaseURL returns the base URL the client talks to.
func (c *Client) BaseURL() string { return c.base }

// timeoutFor picks the time limit of a path.
func (c *Client) timeoutFor(path string) time.Duration {
	switch path {
	case pathEventsListen:
		return c.timeouts.Poll
	case pathLGAddLinks, pathLGAddContainer:
		return c.timeouts.Bulk
	}
	return c.timeouts.Normal
}

// Call posts {"params":params} to a JDownloader path and decodes the "data"
// member of the answer into out. out may be nil. JDownloader answers void
// calls with "" or nothing at all, which leaves out untouched.
//
// Errors are *Error, except when ctx ends first, then ctx's error is returned.
func (c *Client) Call(ctx context.Context, path string, out any, params ...any) error {
	status, _, data, err := c.do(ctx, path, params)
	if err != nil {
		return err
	}
	return redact(path, decodeResponse(path, status, data, out))
}

// do posts one call and returns status, Content-Type and the whole body.
func (c *Client) do(ctx context.Context, path string, params []any) (int, string, []byte, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(struct {
		Params []any `json:"params"`
	}{params})
	if err != nil {
		return 0, "", nil, &Error{Path: path, Type: TypeBadRequest, Raw: "cannot encode the params", cause: err}
	}

	cctx, cancel := context.WithTimeout(ctx, c.timeoutFor(path))
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, "", nil, &Error{Path: path, Type: TypeConnection, Raw: err.Error(), cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Without this Go asks for gzip, and JDownloader answers large lists with
	// "Content-Encoding: deflate" that net/http does not decode.
	req.Header.Set("Accept-Encoding", "identity")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, "", nil, c.transportError(ctx, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, "", nil, c.transportError(ctx, path, err)
	}
	if len(data) > maxBody {
		return 0, "", nil, &Error{Status: resp.StatusCode, Path: path, Type: TypeInvalidResponse, Raw: "response larger than limit"}
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), data, nil
}

// secretPaths are the calls whose params can hold a password or another
// secret. JDownloader echoes a rejected param in the error (in "data" or in
// "type"), so these errors keep no response text and no unknown type.
var secretPaths = map[string]bool{
	pathAccAdd: true, pathAccSetLogin: true, pathBasicAdd: true, pathBasicUpdate: true,
	pathExtPassword: true, pathExtSetSet: true, pathLGAddLinks: true,
	dlNamespace + "/setDownloadPassword": true, lgNamespace + "/setDownloadPassword": true,
	pathConfigSet: true, pathPluginSet: true,
}

// knownTypes are the error types that carry no echoed input.
var knownTypes = map[string]bool{
	TypeBadParameters: true, TypeAuthFailed: true, TypeFileNotFound: true, TypeNotAvailable: true,
	TypeInternal: true, TypeNotImplemented: true, TypeInvalidResponse: true, TypeConnection: true, TypeBadRequest: true,
}

// TypeRedacted replaces an error type of JDownloader that is not a known
// constant when the call carried a secret: JDownloader puts messages such as
// "Invalid ID: <input>" into the type.
const TypeRedacted = "JD_REDACTED"

// redact removes the response text and an unknown type from the error of a
// call with secret params. Status and path stay.
func redact(path string, err error) error {
	e := asError(err)
	if e == nil || !secretPaths[path] {
		return err
	}
	if e.Raw != "" {
		e.Raw = "(response text left out, the call carried a secret)"
	}
	if !knownTypes[e.Type] {
		e.Type = TypeRedacted
	}
	return err
}

// callImage posts a call that answers with image bytes instead of JSON. An
// error object of JDownloader is still returned as *Error.
func (c *Client) callImage(ctx context.Context, path string, params ...any) (data []byte, contentType string, err error) {
	status, ctype, body, err := c.do(ctx, path, params)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusOK && strings.HasPrefix(ctype, "image/") {
		return body, ctype, nil
	}
	if err := decodeResponse(path, status, body, nil); err != nil {
		return nil, "", err
	}
	return nil, "", &Error{Status: status, Path: path, Type: TypeInvalidResponse, Raw: "no image: " + ctype}
}

// transportError separates the caller giving up from JDownloader being unreachable.
func (c *Client) transportError(ctx context.Context, path string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &Error{Path: path, Type: TypeConnection, Raw: err.Error(), cause: err}
}

// envelope is the answer of JDownloader, a success or an error.
type envelope struct {
	Data json.RawMessage `json:"data"`
	Src  string          `json:"src"`
	Type string          `json:"type"`
}

func decodeResponse(path string, status int, body []byte, out any) error {
	var env envelope
	jsonErr := json.Unmarshal(body, &env)
	isJSON := jsonErr == nil

	switch {
	case status == http.StatusNotImplemented:
		return &Error{Status: status, Path: path, Type: TypeNotImplemented, Raw: clip(body)}
	case status == http.StatusInternalServerError:
		// A Java stacktrace as text or an error object. Both are an internal error of JDownloader.
		e := &Error{Status: status, Path: path, Type: TypeInternal, Raw: clip(body)}
		if isJSON {
			e.Src = env.Src
		}
		return e
	case status < 200 || status > 299:
		if isJSON && env.Type != "" {
			return &Error{Status: status, Path: path, Src: env.Src, Type: env.Type, Raw: clip(body)}
		}
		// Not an error object of JDownloader: some other service answers on this port.
		return &Error{Status: status, Path: path, Type: TypeInvalidResponse, Raw: clip(body)}
	}

	if !isJSON {
		return &Error{Status: status, Path: path, Type: TypeInvalidResponse, Raw: clip(body)}
	}
	if env.Type != "" && env.Data == nil {
		// An error object with a success status. Not seen so far, but never take it for data.
		return &Error{Status: status, Path: path, Src: env.Src, Type: env.Type, Raw: clip(body)}
	}
	if out == nil || isVoid(env.Data) {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return &Error{Status: status, Path: path, Type: TypeInvalidResponse, Raw: clip(body), cause: err}
	}
	return nil
}

// isVoid reports whether data carries no value. JDownloader sends "" (and no
// data member at all) for calls without a result, and null for unset values.
func isVoid(data json.RawMessage) bool {
	s := string(bytes.TrimSpace(data))
	return s == "" || s == "null" || s == `""`
}

// clip makes response bytes safe text: valid UTF-8, cut at 4 KB on a rune boundary.
func clip(b []byte) string {
	s := strings.ToValidUTF8(string(b), "\uFFFD")
	if len(s) <= rawLimit {
		return s
	}
	return cutRunes(s, rawLimit) + "..."
}

// cutRunes cuts s to at most max bytes without splitting a rune.
func cutRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// Error types. JDownloader's own types (BAD_PARAMETERS, FILE_NOT_FOUND, ...)
// are passed through unchanged. The JD_ and BAD_REQUEST types come from the client.
const (
	TypeBadParameters   = "BAD_PARAMETERS"
	TypeAuthFailed      = "AUTH_FAILED"
	TypeFileNotFound    = "FILE_NOT_FOUND"
	TypeNotAvailable    = "NOT_AVAILABLE"
	TypeInternal        = "JD_INTERNAL_ERROR"   // HTTP 500, usually a stacktrace
	TypeNotImplemented  = "JD_NOT_IMPLEMENTED"  // HTTP 501, unknown path
	TypeInvalidResponse = "JD_INVALID_RESPONSE" // not JSON of JDownloader: another service?
	TypeConnection      = "JD_CONNECTION"       // not reachable, refused, timeout
	TypeBadRequest      = "JD_BAD_REQUEST"      // the client could not build the request
)

// Error is a failed call.
type Error struct {
	Status int    // HTTP status, 0 if there was no answer
	Src    string // "DEVICE" for errors reported by JDownloader
	Type   string // JDownloader's type or one of the Type constants
	Raw    string // response text, cut at 4 KB, or the connection error text
	Path   string // the JDownloader path of the call

	cause error
}

func (e *Error) Error() string {
	raw := strings.Join(strings.Fields(strings.ToValidUTF8(e.Raw, "\uFFFD")), " ")
	if len(raw) > 160 {
		raw = cutRunes(raw, 160) + "..."
	}
	s := "jd: " + e.Path + ": " + e.Type
	if e.Status != 0 {
		s += " (HTTP " + strconv.Itoa(e.Status) + ")"
	}
	if raw != "" && e.Type != TypeInternal {
		s += ": " + raw
	}
	return s
}

func (e *Error) Unwrap() error { return e.cause }

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// IsConnection reports that JDownloader could not be reached at all.
func IsConnection(err error) bool { e := asError(err); return e != nil && e.Type == TypeConnection }

// IsNotImplemented reports HTTP 501, JDownloader does not know the path.
func IsNotImplemented(err error) bool {
	e := asError(err)
	return e != nil && e.Type == TypeNotImplemented
}

// IsInvalidResponse reports an answer that is not JDownloader's, for example
// another service on the port.
func IsInvalidResponse(err error) bool {
	e := asError(err)
	return e != nil && e.Type == TypeInvalidResponse
}

// IsInternal reports HTTP 500 (JD_INTERNAL_ERROR).
func IsInternal(err error) bool { e := asError(err); return e != nil && e.Type == TypeInternal }

// IsNotFound reports an HTTP 404 of JDownloader: an unknown event
// subscription, captcha job and so on. A 404 of another service is not one.
func IsNotFound(err error) bool {
	e := asError(err)
	return e != nil && e.Status == http.StatusNotFound && e.Type != TypeInvalidResponse
}

// ErrRejected is returned when JDownloader answers a call with false, for
// example a config value of the wrong type.
var ErrRejected = errors.New("jd: JDownloader rejected the request")

// ErrInvalidURL is wrapped by every NormalizeURL failure.
var ErrInvalidURL = errors.New("invalid JDownloader address")

// NormalizeURL turns what a user typed into the base URL of the API: a missing
// scheme becomes http://, scheme and host are lower case, trailing slashes are
// removed. Anything else that cannot be a JDownloader address is an error
// wrapping ErrInvalidURL.
func NormalizeURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidURL)
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, errors.Unwrap(err))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme %q is not http or https", ErrInvalidURL, u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("%w: no host", ErrInvalidURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: user and password are not supported", ErrInvalidURL)
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.Contains(s, "?") || strings.Contains(s, "#") {
		return "", fmt.Errorf("%w: no query or fragment allowed", ErrInvalidURL)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%w: port %q", ErrInvalidURL, p)
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}
