package auth

import (
	"context"
	"errors"

	"github.com/Buffer379x/jdweb/internal/store"
)

// Account is what GET /session reports about the user.
type Account struct {
	Username    string
	TOTPEnabled bool
}

// Enrollment is the answer to POST /totp/enroll.
type Enrollment struct {
	Secret string // Base32, for manual entry
	URI    string // otpauth:// Key URI
	QRSVG  string // the URI as a QR code
}

// Account returns the user name and whether two-factor is on.
func (s *Service) Account(ctx context.Context) (Account, error) {
	u, err := s.db.GetUser(ctx)
	if err != nil {
		return Account{}, err
	}
	return Account{Username: u.Username, TOTPEnabled: u.TOTPSecret != ""}, nil
}

// checkPassword verifies the signed-in user's password with rate limiting, so
// a stolen session cannot be used to guess the password. The attempt is
// reserved before the slow check; the returned attempt must be settled with
// succeed (already done here on success) and closed with done.
func (s *Service) checkPassword(ctx context.Context, user store.User, pw string) (*attempt, error) {
	if _, err := s.reserve(s.acct, "account"); err != nil {
		return nil, err
	}
	att := &attempt{l: s.acct, ip: "account"}
	ok, err := s.verify(ctx, user.PasswordHash, pw)
	if err != nil {
		att.done()
		return nil, err
	}
	if !ok {
		att.fail()
		s.log.Warn("password check failed for a signed-in session")
		return nil, ErrWrongPassword
	}
	att.succeed()
	return att, nil
}

// ChangePassword sets a new password and signs out every session except keepHash.
func (s *Service) ChangePassword(ctx context.Context, current, next, keepHash string) error {
	user, err := s.db.GetUser(ctx)
	if err != nil {
		return err
	}
	if _, err := s.checkPassword(ctx, user, current); err != nil {
		return err
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hash, err := HashPasswordContext(ctx, next)
	if err != nil {
		return err
	}
	if err := s.db.UpdatePassword(ctx, hash, s.now()); err != nil {
		return err
	}
	if _, err := s.db.DeleteOtherSessions(ctx, keepHash); err != nil {
		return err
	}
	s.log.Info("password changed, other sessions signed out")
	return nil
}

// EnrollTOTP creates a pending secret after the password was confirmed, so a
// stolen session cannot switch on a second factor that locks the owner out.
// The secret becomes active only after ConfirmTOTP; a restart or the timeout
// discards it.
func (s *Service) EnrollTOTP(ctx context.Context, password string) (Enrollment, error) {
	user, err := s.db.GetUser(ctx)
	if err != nil {
		return Enrollment{}, err
	}
	if user.TOTPSecret != "" {
		return Enrollment{}, ErrTOTPEnabled
	}
	if _, err := s.checkPassword(ctx, user, password); err != nil {
		return Enrollment{}, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return Enrollment{}, err
	}
	uri := otpauthURI(secret, user.Username)
	svg, err := qrSVG(uri)
	if err != nil {
		return Enrollment{}, err
	}
	s.mu.Lock()
	s.enroll = &enrollment{secret: secret, expires: s.now().Add(enrollmentTTL)}
	s.mu.Unlock()
	return Enrollment{Secret: secret, URI: uri, QRSVG: svg}, nil
}

// ConfirmTOTP activates two-factor when code matches the pending secret and
// returns the ten recovery codes. They are shown once; only hashes are stored.
func (s *Service) ConfirmTOTP(ctx context.Context, code string) ([]string, error) {
	if _, err := s.reserve(s.acct, "account"); err != nil {
		return nil, err
	}
	att := &attempt{l: s.acct, ip: "account"}
	defer att.done()
	s.mu.Lock()
	pending := s.enroll
	s.mu.Unlock()
	if pending == nil || !s.now().Before(pending.expires) {
		return nil, ErrNoEnrollment
	}
	raw, err := decodeSecret(pending.secret)
	if err != nil {
		return nil, err
	}
	ok, err := s.acceptTOTP(ctx, raw, trimmedDigits(code))
	if err != nil {
		return nil, err
	}
	if !ok {
		att.fail()
		return nil, ErrInvalidCode
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = hashRecoveryCode(c)
	}
	if err := s.db.EnableTOTP(ctx, pending.secret, hashes, s.now()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.enroll = nil
	s.mu.Unlock()
	att.succeed()
	s.log.Info("two-factor enabled")
	return codes, nil
}

func trimmedDigits(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != ' ' {
			out = append(out, r)
		}
	}
	return string(out)
}

// DisableTOTP switches two-factor off after the password was confirmed.
func (s *Service) DisableTOTP(ctx context.Context, password string) error {
	user, err := s.db.GetUser(ctx)
	if err != nil {
		return err
	}
	if user.TOTPSecret == "" {
		return ErrTOTPNotEnabled
	}
	if _, err := s.checkPassword(ctx, user, password); err != nil {
		return err
	}
	if err := s.db.DisableTOTP(ctx, s.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	s.log.Info("two-factor disabled")
	return nil
}
