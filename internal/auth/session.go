package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Buffer379x/jdweb/internal/store"
)

// LoginRequest is the body of POST /login plus client details.
type LoginRequest struct {
	Username  string
	Password  string
	TOTP      string // a six-digit code or a recovery code
	IP        string
	UserAgent string
}

// LoginResult carries the raw session token, which only exists here and in the cookie.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
}

// SessionInfo describes a session without exposing its token.
type SessionInfo struct {
	ID         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	Current    bool
}

func sessionID(tokenHash string) string { return tokenHash[:16] }

func trimmed(s string) string { return strings.TrimSpace(s) }

// dummyHash makes a login for an unknown user cost as much as a real one.
var dummyHash = func() string {
	h, err := HashPassword("jdweb dummy password for constant-cost logins")
	if err != nil {
		panic(err)
	}
	return h
}()

// Login checks the credentials and opens a session. Every failure looks the
// same to the caller; the user name is never revealed or logged.
func (s *Service) Login(ctx context.Context, req LoginRequest) (LoginResult, error) {
	key := ipKey(req.IP)
	if err := s.limited(s.login, key); err != nil {
		s.log.Warn("login blocked by rate limit", "ip", req.IP)
		return LoginResult{}, err
	}
	fail := func() (LoginResult, error) {
		s.login.fail(key)
		s.log.Warn("login failed", "ip", req.IP)
		return LoginResult{}, ErrInvalidCredentials
	}

	user, err := s.db.GetUser(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return LoginResult{}, err
	}
	nameOK := err == nil && tokensEqual(user.Username, trimmed(req.Username))
	if !nameOK {
		s.verify(dummyHash, req.Password)
		return fail()
	}
	if !s.verify(user.PasswordHash, req.Password) {
		return fail()
	}
	if user.TOTPSecret != "" {
		if strings.TrimSpace(req.TOTP) == "" {
			return LoginResult{}, ErrTOTPRequired
		}
		ok, err := s.checkSecondFactor(ctx, user.TOTPSecret, req.TOTP)
		if err != nil {
			return LoginResult{}, err
		}
		if !ok {
			return fail()
		}
	}

	res, err := s.newSession(ctx, req.UserAgent)
	if err != nil {
		return LoginResult{}, err
	}
	s.login.success(key)
	s.log.Info("login", "ip", req.IP)
	return res, nil
}

// checkSecondFactor accepts a TOTP code or a recovery code. Both work once.
func (s *Service) checkSecondFactor(ctx context.Context, secret, input string) (bool, error) {
	code := strings.ReplaceAll(strings.TrimSpace(input), " ", "")
	if raw, err := decodeSecret(secret); err == nil && len(code) == totpDigits && isDigits(code) {
		s.mu.Lock()
		defer s.mu.Unlock()
		step, ok := verifyTOTP(raw, code, s.now(), s.lastStep)
		if ok {
			s.lastStep = step
		}
		return ok, nil
	}
	return s.db.UseRecoveryCode(ctx, hashRecoveryCode(input), s.now())
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Service) newSession(ctx context.Context, userAgent string) (LoginResult, error) {
	tok, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now()
	if _, err := s.db.DeleteExpiredSessions(ctx, now); err != nil {
		return LoginResult{}, err
	}
	if len(userAgent) > maxUserAgent {
		userAgent = strings.ToValidUTF8(userAgent[:maxUserAgent], "")
	}
	exp := now.Add(SessionTTL)
	err = s.db.CreateSession(ctx, store.Session{
		TokenHash: hashToken(tok), CreatedAt: now, LastSeenAt: now, ExpiresAt: exp, UserAgent: userAgent,
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: tok, ExpiresAt: exp}, nil
}

// Logout deletes one session by the token hash from the request context.
func (s *Service) Logout(ctx context.Context, tokenHash string) error {
	return s.db.DeleteSession(ctx, tokenHash)
}

// SignOutOthers deletes every session except the current one.
func (s *Service) SignOutOthers(ctx context.Context, currentHash string) (int64, error) {
	return s.db.DeleteOtherSessions(ctx, currentHash)
}

// ListSessions returns the active sessions and marks the current one.
func (s *Service) ListSessions(ctx context.Context, currentHash string) ([]SessionInfo, error) {
	all, err := s.db.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]SessionInfo, 0, len(all))
	for _, x := range all {
		if !x.ExpiresAt.After(now) {
			continue
		}
		out = append(out, SessionInfo{
			ID: sessionID(x.TokenHash), CreatedAt: x.CreatedAt, LastSeenAt: x.LastSeenAt,
			ExpiresAt: x.ExpiresAt, UserAgent: x.UserAgent, Current: x.TokenHash == currentHash,
		})
	}
	return out, nil
}
