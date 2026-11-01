package auth

import (
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
	// verify checks a password against a hash; tests count its calls.
	verify func(hash, pw string) bool

	login *limiter // failed logins and setup attempts, by client address
	acct  *limiter // failed password checks of signed-in users

	mu         sync.Mutex // guards the three fields below
	setupToken string     // first-run token; memory only
	enroll     *enrollment
	lastStep   int64 // last accepted TOTP step, to refuse replays
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
		verify:  VerifyPassword,
		login:   newLimiter(cfg.Now),
		acct:    newLimiter(cfg.Now),
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

func (s *Service) limited(l *limiter, key string) error {
	if w := l.check(key); w > 0 {
		return &RateLimitError{RetryAfter: w}
	}
	return nil
}

func ipKey(ip string) string {
	if ip == "" {
		return "unknown"
	}
	return ip
}
