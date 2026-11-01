package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/Buffer379x/jdweb/internal/store"
)

const (
	// SessionTTL is the lifetime of a session. Activity moves it forward.
	SessionTTL = 30 * 24 * time.Hour
	// touchInterval is how often an active session is written to the database.
	touchInterval = 10 * time.Minute
	// resetTokenTTL is how long a token from `reset-password` stays valid.
	resetTokenTTL = time.Hour
	// enrollmentTTL is how long a TOTP secret waits for its confirmation code.
	enrollmentTTL = 10 * time.Minute

	maxUserAgent = 200
	// resetKey is the setting that holds the hashed reset token.
	resetKey = "auth.reset_token"
	// lastStepKey is the setting that holds the last accepted TOTP step, so a
	// code cannot be replayed after a restart.
	lastStepKey = "auth.totp_last_step"
)

var (
	ErrInvalidCredentials = errors.New("username, password or code is wrong")
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrRateLimited        = errors.New("too many failed attempts")
	ErrInvalidToken       = errors.New("setup token is wrong")
	ErrSetupClosed        = errors.New("setup is already completed")
	ErrNoAccount          = errors.New("no account exists yet")
	ErrWrongPassword      = errors.New("current password is wrong")
	ErrTOTPEnabled        = errors.New("two-factor is already enabled")
	ErrTOTPNotEnabled     = errors.New("two-factor is not enabled")
	ErrNoEnrollment       = errors.New("no two-factor enrollment in progress")
	ErrInvalidCode        = errors.New("code is wrong")
)

// RateLimitError tells the caller how long to wait. It matches ErrRateLimited.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string        { return ErrRateLimited.Error() }
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// Config configures a Service. All fields are optional.
type Config struct {
	// TrustedProxies are the peers whose X-Forwarded-* headers count.
	TrustedProxies []netip.Prefix
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Logger receives structured events. It never receives passwords or
	// tokens, except for the deliberate setup_token line of InitSetup.
	Logger *slog.Logger
}

// Service is the authentication facade for the HTTP layer.
type Service struct {
	db      *store.DB
	trusted []netip.Prefix
	now     func() time.Time
	log     *slog.Logger
	// verify checks a password against a hash; tests replace it.
	verify func(ctx context.Context, hash, pw string) (bool, error)
	// sleep waits for the throttle; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error

	login *limiter // failed logins by client address, plus global throttle
	setup *limiter // wrong setup and reset tokens by client address
	acct  *limiter // failed password checks of signed-in users

	forwardedWarn sync.Once

	mu         sync.Mutex // guards the two fields below and the TOTP step
	setupToken string     // first-run token; memory only
	enroll     *enrollment
}

type enrollment struct {
	secret  string
	expires time.Time
}

// New creates a Service on top of db.
func New(db *store.DB, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		db:      db,
		trusted: cfg.TrustedProxies,
		now:     cfg.Now,
		log:     cfg.Logger,
		verify:  VerifyPasswordContext,
		sleep:   sleepContext,
		login:   newLimiter(cfg.Now),
		setup:   newIPLimiter(cfg.Now),
		acct:    newIPLimiter(cfg.Now),
	}
}

// randomToken returns n random bytes as unpadded base64url.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// tokensEqual compares in constant time, independent of the lengths.
func tokensEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reserve counts an attempt on l before it is evaluated, or refuses it with a
// RateLimitError. The caller must settle the reservation (see attempt).
func (s *Service) reserve(l *limiter, ip string) (time.Duration, error) {
	wait, throttle, ok := l.reserve(ip)
	if !ok {
		return 0, &RateLimitError{RetryAfter: wait}
	}
	return throttle, nil
}

// attempt is a reserved try. Unless failed or succeeded is called, done
// refunds it: errors and "second factor missing" are not failures.
type attempt struct {
	l       *limiter
	ip      string
	settled bool
}

func (a *attempt) fail()    { a.settled = true }
func (a *attempt) succeed() { a.settled = true; a.l.success(a.ip) }
func (a *attempt) done() {
	if !a.settled {
		a.l.refund(a.ip)
	}
}

func ipKey(ip string) string { return limiterKey(ip) }

// currentStep returns the last accepted TOTP step. Call with s.mu held.
func (s *Service) lastStep(ctx context.Context) (int64, error) {
	var step int64
	err := s.db.GetJSON(ctx, lastStepKey, &step)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	return step, err
}

// acceptTOTP checks a six-digit code against the secret and, if it is fresh,
// stores its step so that it cannot be used again, even after a restart.
func (s *Service) acceptTOTP(ctx context.Context, secret []byte, code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, err := s.lastStep(ctx)
	if err != nil {
		return false, err
	}
	step, ok := verifyTOTP(secret, code, s.now(), last)
	if !ok {
		return false, nil
	}
	if err := s.db.SetJSON(ctx, lastStepKey, step); err != nil {
		return false, err // fail closed: an unrecorded step could be replayed
	}
	return true, nil
}
