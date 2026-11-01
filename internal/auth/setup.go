package auth

import (
	"context"
	"errors"
	"time"

	"github.com/Buffer379x/jdweb/internal/store"
)

// SetupRequest is the body of POST /setup plus the client address.
type SetupRequest struct {
	Token    string
	Username string
	Password string
	IP       string
}

// resetRecord is stored in the setting table. Only the hash of the token is kept.
type resetRecord struct {
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
}

// InitSetup is called once when the server starts. If no account exists it
// creates the one-time setup token, keeps it in memory and logs it once as the
// structured field setup_token. This is the only place a secret is logged.
func (s *Service) InitSetup(ctx context.Context) error {
	n, err := s.db.UserCount(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupToken != "" {
		return nil
	}
	tok, err := randomToken(24)
	if err != nil {
		return err
	}
	s.setupToken = tok
	s.log.Warn("JDWeb is not set up yet: open the web interface and create the account with this one-time token",
		"setup_token", tok)
	return nil
}

func (s *Service) activeReset(ctx context.Context) (resetRecord, bool, error) {
	var rec resetRecord
	err := s.db.GetJSON(ctx, resetKey, &rec)
	if errors.Is(err, store.ErrNotFound) {
		return rec, false, nil
	}
	if err != nil {
		return rec, false, err
	}
	return rec, s.now().Unix() < rec.ExpiresAt, nil
}

// SetupRequired reports whether POST /setup can currently succeed: before the
// account exists, or while a reset token from `reset-password` is valid.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.db.UserCount(ctx)
	if err != nil {
		return false, err
	}
	if n == 0 {
		return true, nil
	}
	_, ok, err := s.activeReset(ctx)
	return ok, err
}

// Setup creates the account with the setup token, or, after `reset-password`,
// replaces the password of the existing account with the reset token. Both
// tokens work once.
func (s *Service) Setup(ctx context.Context, req SetupRequest) error {
	// Setup and reset have their own per-address limiter. They never feel the
	// global login pressure, so the owner can always recover the account.
	key := ipKey(req.IP)
	if _, err := s.reserve(s.setup, key); err != nil {
		return err
	}
	att := &attempt{l: s.setup, ip: key}
	defer att.done()
	n, err := s.db.UserCount(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return s.firstSetup(ctx, req, att)
	}
	return s.resetSetup(ctx, req, att)
}

func (s *Service) firstSetup(ctx context.Context, req SetupRequest, att *attempt) error {
	s.mu.Lock()
	want := s.setupToken
	s.mu.Unlock()
	if want == "" || !tokensEqual(want, req.Token) {
		att.fail()
		s.log.Warn("setup refused: wrong token", "ip", req.IP)
		return ErrInvalidToken
	}
	if err := ValidateUsername(req.Username); err != nil {
		return err
	}
	if err := ValidatePassword(req.Password); err != nil {
		return err
	}
	hash, err := HashPasswordContext(ctx, req.Password)
	if err != nil {
		return err
	}
	switch err := s.db.CreateUser(ctx, trimmed(req.Username), hash, s.now()); {
	case errors.Is(err, store.ErrExists):
		return ErrSetupClosed
	case err != nil:
		return err
	}
	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()
	att.succeed()
	s.log.Info("account created", "ip", req.IP)
	return nil
}

func (s *Service) resetSetup(ctx context.Context, req SetupRequest, att *attempt) error {
	rec, ok, err := s.activeReset(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSetupClosed
	}
	if !tokensEqual(rec.Hash, hashToken(req.Token)) {
		att.fail()
		s.log.Warn("password reset refused: wrong token", "ip", req.IP)
		return ErrInvalidToken
	}
	if err := ValidatePassword(req.Password); err != nil {
		return err
	}
	hash, err := HashPasswordContext(ctx, req.Password)
	if err != nil {
		return err
	}
	switch err := s.db.ResetCredentials(ctx, hash, s.now(), resetKey); {
	case errors.Is(err, store.ErrNotFound):
		return ErrSetupClosed // used by a concurrent request
	case err != nil:
		return err
	}
	att.succeed()
	s.log.Info("password reset completed, two-factor and all sessions cleared", "ip", req.IP)
	return nil
}

// ResetToken creates a one-time token for `jdweb reset-password` and stores
// its hash in the setting table, where the running server picks it up. It
// returns the token for the CLI to print. A new call replaces the old token.
func (s *Service) ResetToken(ctx context.Context) (string, error) {
	n, err := s.db.UserCount(ctx)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", ErrNoAccount
	}
	tok, err := randomToken(24)
	if err != nil {
		return "", err
	}
	rec := resetRecord{Hash: hashToken(tok), ExpiresAt: s.now().Add(resetTokenTTL).Unix()}
	if err := s.db.SetJSON(ctx, resetKey, rec); err != nil {
		return "", err
	}
	return tok, nil
}

// ResetTokenValidFor is the lifetime of a reset token, for the CLI message.
func ResetTokenValidFor() time.Duration { return resetTokenTTL }
