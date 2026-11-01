package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Buffer379x/jdweb/internal/store"
)

const (
	// CookieName is the session cookie.
	CookieName = "jdweb_session"
	// RequestHeader must be "1" on every state-changing request.
	RequestHeader = "X-JDWeb-Request"
)

type ctxKey struct{}

// CurrentSession identifies the session of a request. It never holds the token.
type CurrentSession struct {
	ID        string // short public identifier
	TokenHash string // key of the session in the database
	ExpiresAt time.Time
}

// SessionFromContext returns the session that Middleware found for the request.
func SessionFromContext(ctx context.Context) (CurrentSession, bool) {
	s, ok := ctx.Value(ctxKey{}).(CurrentSession)
	return s, ok
}

// ParseTrustedProxies parses a comma-separated list of CIDR ranges or single
// addresses, as in JDWEB_TRUSTED_PROXIES.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, errors.New("trusted proxy " + item + " is neither an address nor a CIDR range")
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().WithZone("").Unmap(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.WithZone("").Unmap(), true
	}
	return netip.Addr{}, false
}

func (s *Service) isTrusted(a netip.Addr) bool {
	for _, p := range s.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (s *Service) peer(r *http.Request) (netip.Addr, bool) { return parseAddr(r.RemoteAddr) }

func (s *Service) fromTrustedProxy(r *http.Request) bool {
	a, ok := s.peer(r)
	return ok && s.isTrusted(a)
}

// ClientIP is the address rate limiting and logs use. It is the peer address,
// unless the peer is a trusted proxy: then it is the first address in
// X-Forwarded-For, read from the right, that is not itself a trusted proxy.
func (s *Service) ClientIP(r *http.Request) string {
	peer, ok := s.peer(r)
	if !ok {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}
	if !s.isTrusted(peer) {
		if len(s.trusted) == 0 && r.Header.Get("X-Forwarded-For") != "" {
			s.forwardedWarn.Do(func() {
				s.log.Warn("X-Forwarded-For header ignored because JDWEB_TRUSTED_PROXIES is empty; behind a reverse proxy set it to the proxy address, otherwise rate limiting and logs see only the proxy",
					"peer", peer.String())
			})
		}
		return peer.String()
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseAddr(hops[i])
		if !ok {
			return peer.String() // do not trust a header we cannot read
		}
		if !s.isTrusted(a) || i == 0 {
			return a.String()
		}
	}
	return peer.String()
}

// IsSecure reports whether the browser reached JDWeb over HTTPS: directly, or
// through a trusted proxy that says so.
func (s *Service) IsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !s.fromTrustedProxy(r) {
		return false
	}
	first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(first), "https")
}

// requestHost is the host the browser used. X-Forwarded-Host counts only from a trusted proxy.
func (s *Service) requestHost(r *http.Request) string {
	if s.fromTrustedProxy(r) {
		if first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return r.Host
}

// SetSessionCookie sends the session cookie. It is HttpOnly and SameSite=Strict,
// and Secure when the request came over HTTPS.
func (s *Service) SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	maxAge := int(expires.Unix() - s.now().Unix())
	if maxAge <= 0 {
		maxAge = -1 // MaxAge 0 would mean "no Max-Age"; an expired session must delete the cookie
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.IsSecure(r), SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie tells the browser to drop the session cookie.
func (s *Service) ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.IsSecure(r), SameSite: http.SameSiteStrictMode,
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Middleware looks up the session cookie and puts the session on the request
// context. It never rejects: public routes need it too. Use RequireAuth for
// protected routes. A session that was last seen a while ago is renewed.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cur, ok := s.lookup(w, r); ok {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, cur))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) lookup(w http.ResponseWriter, r *http.Request) (CurrentSession, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return CurrentSession{}, false
	}
	h := hashToken(c.Value)
	sess, err := s.db.GetSession(r.Context(), h)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("session lookup failed", "err", err)
		}
		return CurrentSession{}, false
	}
	now := s.now()
	if !sess.ExpiresAt.After(now) {
		s.db.DeleteSession(r.Context(), h)
		return CurrentSession{}, false
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		exp := now.Add(SessionTTL)
		if err := s.db.TouchSession(r.Context(), h, now, exp); err != nil {
			s.log.Error("session renewal failed", "err", err)
		} else {
			sess.ExpiresAt = exp
			s.SetSessionCookie(w, r, c.Value, exp)
		}
	}
	return CurrentSession{ID: sessionID(h), TokenHash: h, ExpiresAt: sess.ExpiresAt}, true
}

// RequireAuth answers 401 unless Middleware found a session.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := SessionFromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireMutation guards every request that is not GET, HEAD or OPTIONS: the
// header X-JDWeb-Request must be "1" and Origin must name the host the
// request was sent to. Otherwise the answer is 403.
func (s *Service) RequireMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(RequestHeader) != "1" {
			writeError(w, http.StatusForbidden, "forbidden", "Missing "+RequestHeader+" header.")
			return
		}
		if !s.originMatches(r) {
			writeError(w, http.StatusForbidden, "forbidden", "Origin does not match this site.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) originMatches(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return strings.EqualFold(u.Host, s.requestHost(r))
}
