package auth

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/store"
)

const (
	testUser = "admin"
	testPass = "correct horse battery"
	otherPW  = "another long passphrase"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type env struct {
	t   *testing.T
	db  *store.DB
	svc *Service
	clk *fakeClock
	log *lockedBuffer
	ctx context.Context
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "jdweb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, clk: newClock(), log: &lockedBuffer{}, ctx: context.Background()}
	cfg.Now = e.clk.Now
	cfg.Logger = slog.New(slog.NewJSONHandler(e.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.svc = New(db, cfg)
	return e
}

// withAccount creates the account directly, bypassing the setup token.
func (e *env) withAccount() *env {
	e.t.Helper()
	h, err := HashPassword(testPass)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.CreateUser(e.ctx, testUser, h, e.clk.Now()); err != nil {
		e.t.Fatal(err)
	}
	return e
}

func (e *env) login(ip string) (LoginResult, error) {
	return e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: testPass, IP: ip, UserAgent: "test-agent"})
}

func (e *env) mustLogin() LoginResult {
	e.t.Helper()
	res, err := e.login("10.0.0.1")
	if err != nil {
		e.t.Fatalf("login: %v", err)
	}
	return res
}

func sha256hex(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (e *env) logLines() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(e.log.String()), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			e.t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// setupToken returns the token logged by InitSetup.
func (e *env) setupToken() string {
	e.t.Helper()
	var found []string
	for _, m := range e.logLines() {
		if tok, ok := m["setup_token"].(string); ok {
			found = append(found, tok)
		}
	}
	if len(found) != 1 {
		e.t.Fatalf("setup_token logged %d times, want exactly once", len(found))
	}
	return found[0]
}

func TestInitSetupLogsTokenOnceWhenNoAccount(t *testing.T) {
	e := newEnv(t, Config{})
	if err := e.svc.InitSetup(e.ctx); err != nil {
		t.Fatal(err)
	}
	tok := e.setupToken()
	if len(tok) < 24 {
		t.Fatalf("token too short: %q", tok)
	}
	required, err := e.svc.SetupRequired(e.ctx)
	if err != nil || !required {
		t.Fatalf("SetupRequired = %v, %v", required, err)
	}
	// Calling again must not print a second token.
	if err := e.svc.InitSetup(e.ctx); err != nil {
		t.Fatal(err)
	}
	if tok2 := e.setupToken(); tok2 != tok {
		t.Fatal("token changed")
	}
}

func TestInitSetupWithAccountLogsNoToken(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	if err := e.svc.InitSetup(e.ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.log.String(), "setup_token") {
		t.Fatalf("token logged although an account exists: %s", e.log.String())
	}
	if required, _ := e.svc.SetupRequired(e.ctx); required {
		t.Fatal("setup required although account exists")
	}
}

func TestSetupCreatesAccountOnceThenIsClosed(t *testing.T) {
	e := newEnv(t, Config{})
	e.svc.InitSetup(e.ctx)
	tok := e.setupToken()

	req := SetupRequest{Token: tok, Username: testUser, Password: testPass, IP: "10.0.0.1"}
	if err := e.svc.Setup(e.ctx, req); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	u, err := e.db.GetUser(e.ctx)
	if err != nil || u.Username != testUser {
		t.Fatalf("user = %+v, %v", u, err)
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") || strings.Contains(u.PasswordHash, testPass) {
		t.Fatalf("password not stored as Argon2id hash: %s", u.PasswordHash)
	}
	// The same, still correct, token must not work a second time.
	req.Username, req.Password = "mallory", otherPW
	if err := e.svc.Setup(e.ctx, req); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("second Setup = %v, want ErrSetupClosed", err)
	}
	if u, _ := e.db.GetUser(e.ctx); u.Username != testUser {
		t.Fatalf("account was replaced: %+v", u)
	}
	if required, _ := e.svc.SetupRequired(e.ctx); required {
		t.Fatal("SetupRequired still true")
	}
	if _, err := e.login("10.0.0.1"); err != nil {
		t.Fatalf("login after setup: %v", err)
	}
}

func TestSetupRejectsWrongOrMissingToken(t *testing.T) {
	e := newEnv(t, Config{})
	e.svc.InitSetup(e.ctx)
	tok := e.setupToken()
	for _, bad := range []string{"", "nope", tok + "x", tok[:len(tok)-1], strings.ToUpper(tok)} {
		err := e.svc.Setup(e.ctx, SetupRequest{Token: bad, Username: testUser, Password: testPass, IP: "10.0.0.1"})
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("token %q: %v, want ErrInvalidToken", bad, err)
		}
	}
	if n, _ := e.db.UserCount(e.ctx); n != 0 {
		t.Fatal("account created with a wrong token")
	}
}

func TestSetupValidationDoesNotBurnToken(t *testing.T) {
	e := newEnv(t, Config{})
	e.svc.InitSetup(e.ctx)
	tok := e.setupToken()
	err := e.svc.Setup(e.ctx, SetupRequest{Token: tok, Username: testUser, Password: "short", IP: "10.0.0.1"})
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password: %v", err)
	}
	err = e.svc.Setup(e.ctx, SetupRequest{Token: tok, Username: " ", Password: testPass, IP: "10.0.0.1"})
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("blank username: %v", err)
	}
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: tok, Username: testUser, Password: testPass, IP: "10.0.0.1"}); err != nil {
		t.Fatalf("retry with the same token: %v", err)
	}
}

func TestSetupTokenGuessingIsRateLimited(t *testing.T) {
	e := newEnv(t, Config{})
	e.svc.InitSetup(e.ctx)
	tok := e.setupToken()
	for i := 0; i < 5; i++ {
		e.svc.Setup(e.ctx, SetupRequest{Token: "guess", Username: testUser, Password: testPass, IP: "6.6.6.6"})
	}
	err := e.svc.Setup(e.ctx, SetupRequest{Token: tok, Username: testUser, Password: testPass, IP: "6.6.6.6"})
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want RateLimitError", err)
	}
}

func TestResetTokenFlow(t *testing.T) {
	e := newEnv(t, Config{})
	if _, err := e.svc.ResetToken(e.ctx); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("ResetToken without account = %v, want ErrNoAccount", err)
	}
	e.withAccount()
	// Create TOTP, recovery codes and a session for the old account.
	login := e.mustLogin()
	enrollTOTP(t, e)
	if _, err := e.db.GetSession(e.ctx, sha256hex(login.Token)); err != nil {
		t.Fatal(err)
	}

	// The CLI is a separate process: a second Service on the same database.
	cli := New(e.db, Config{Now: e.clk.Now, Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{}, nil))})
	token, err := cli.ResetToken(e.ctx)
	if err != nil || len(token) < 24 {
		t.Fatalf("ResetToken = %q, %v", token, err)
	}
	var stored map[string]any
	if err := e.db.GetJSON(e.ctx, "auth.reset_token", &stored); err != nil {
		t.Fatalf("reset token not stored in setting: %v", err)
	}
	if raw, _ := json.Marshal(stored); strings.Contains(string(raw), token) {
		t.Fatal("reset token stored in plain text")
	}
	if required, _ := e.svc.SetupRequired(e.ctx); !required {
		t.Fatal("running server does not see the reset token")
	}

	bad := SetupRequest{Token: "wrong", Username: "ignored", Password: otherPW, IP: "10.0.0.1"}
	if err := e.svc.Setup(e.ctx, bad); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong reset token: %v", err)
	}
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: token, Username: "ignored", Password: "short", IP: "10.0.0.1"}); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password in reset: %v", err)
	}
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: token, Username: "ignored", Password: otherPW, IP: "10.0.0.1"}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	u, _ := e.db.GetUser(e.ctx)
	if u.Username != testUser || u.TOTPSecret != "" {
		t.Fatalf("user after reset = %+v", u)
	}
	if n, _ := e.db.UnusedRecoveryCodes(e.ctx); n != 0 {
		t.Fatal("recovery codes survived reset")
	}
	if _, err := e.db.GetSession(e.ctx, sha256hex(login.Token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old session survived reset")
	}
	if _, err := e.login("10.0.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, err := e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: otherPW, IP: "10.0.0.2"}); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: token, Username: "x", Password: testPass, IP: "10.0.0.1"}); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("reuse of reset token = %v, want ErrSetupClosed", err)
	}
	if required, _ := e.svc.SetupRequired(e.ctx); required {
		t.Fatal("setup still required after reset")
	}
}

func TestResetTokenExpires(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	token, err := e.svc.ResetToken(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(resetTokenTTL + time.Minute)
	if required, _ := e.svc.SetupRequired(e.ctx); required {
		t.Fatal("expired reset token still offered")
	}
	err = e.svc.Setup(e.ctx, SetupRequest{Token: token, Username: testUser, Password: otherPW, IP: "10.0.0.1"})
	if !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("expired reset token: %v, want ErrSetupClosed", err)
	}
}

func TestSecondResetTokenInvalidatesFirst(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	first, _ := e.svc.ResetToken(e.ctx)
	second, _ := e.svc.ResetToken(e.ctx)
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: first, Password: otherPW, IP: "10.0.0.1"}); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("first token: %v", err)
	}
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: second, Password: otherPW, IP: "10.0.0.1"}); err != nil {
		t.Fatalf("second token: %v", err)
	}
}

func TestLoginCreatesHashedThirtyDaySession(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	res := e.mustLogin()
	if len(res.Token) != 43 {
		t.Fatalf("token length %d, want 43 (256 bit base64url)", len(res.Token))
	}
	if want := e.clk.Now().Add(30 * 24 * time.Hour); !res.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
	sessions, err := e.db.ListSessions(e.ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, %v", sessions, err)
	}
	if sessions[0].TokenHash != sha256hex(res.Token) {
		t.Fatal("session is not stored under the SHA-256 of the token")
	}
	if sessions[0].UserAgent != "test-agent" {
		t.Fatalf("user agent %q", sessions[0].UserAgent)
	}
	if !sessions[0].ExpiresAt.Equal(res.ExpiresAt) {
		t.Fatal("stored expiry differs")
	}
	// The raw token must not be anywhere in the database file.
	var n int
	e.db.R.QueryRow("SELECT count(*) FROM session WHERE token_hash = ?", res.Token).Scan(&n)
	if n != 0 {
		t.Fatal("raw token stored")
	}
	res2 := e.mustLogin()
	if res2.Token == res.Token {
		t.Fatal("tokens repeat")
	}
}

func TestLoginTruncatesLongUserAgent(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	_, err := e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: testPass, IP: "1.1.1.1", UserAgent: strings.Repeat("ä", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := e.db.ListSessions(e.ctx)
	if len(s[0].UserAgent) > 200 {
		t.Fatalf("user agent length %d", len(s[0].UserAgent))
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	var checks atomic.Int32
	e.svc.verify = func(hash, pw string) bool { checks.Add(1); return VerifyPassword(hash, pw) }

	wrongPW := LoginRequest{Username: testUser, Password: "not the password", IP: "1.1.1.1"}
	unknown := LoginRequest{Username: "nobody", Password: testPass, IP: "1.1.1.2"}
	_, err1 := e.svc.Login(e.ctx, wrongPW)
	_, err2 := e.svc.Login(e.ctx, unknown)
	if !errors.Is(err1, ErrInvalidCredentials) || !errors.Is(err2, ErrInvalidCredentials) {
		t.Fatalf("errors: %v / %v", err1, err2)
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("messages differ: %q vs %q", err1, err2)
	}
	if got := checks.Load(); got != 2 {
		t.Fatalf("password checks = %d, want 2: unknown user must cost a dummy hash", got)
	}
}

func TestLoginWithoutAccountRunsDummyHash(t *testing.T) {
	e := newEnv(t, Config{})
	var checks atomic.Int32
	e.svc.verify = func(hash, pw string) bool { checks.Add(1); return VerifyPassword(hash, pw) }
	_, err := e.login("1.1.1.1")
	if !errors.Is(err, ErrInvalidCredentials) || checks.Load() != 1 {
		t.Fatalf("err = %v, checks = %d", err, checks.Load())
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	bad := LoginRequest{Username: testUser, Password: "wrong password!", IP: "9.9.9.9"}
	for i := 0; i < 5; i++ {
		if _, err := e.svc.Login(e.ctx, bad); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	// Now even the right password is refused, without a hash computation.
	var checks atomic.Int32
	e.svc.verify = func(hash, pw string) bool { checks.Add(1); return VerifyPassword(hash, pw) }
	_, err := e.login("9.9.9.9")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("err = %v, want RateLimitError", err)
	}
	if checks.Load() != 0 {
		t.Fatal("blocked request still hashed a password")
	}
	// Another address is fine.
	if _, err := e.login("8.8.8.8"); err != nil {
		t.Fatalf("other address: %v", err)
	}
	// After the wait it works again and the counter is cleared.
	e.clk.Advance(rl.RetryAfter)
	if _, err := e.login("9.9.9.9"); err != nil {
		t.Fatalf("after wait: %v", err)
	}
	for i := 0; i < 4; i++ {
		e.svc.Login(e.ctx, bad)
	}
	if _, err := e.login("9.9.9.9"); err != nil {
		t.Fatalf("a success must reset the counter: %v", err)
	}
}

func TestLoginGlobalRateLimit(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	for i := 0; i < globalThreshold; i++ {
		ip := "7.7.7." + string(rune('a'+i/4))
		e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: "wrong password!", IP: ip})
	}
	if _, err := e.login("1.2.3.4"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want global rate limit", err)
	}
}

// enrollTOTP walks through enrol and confirm and returns the secret and recovery codes.
func enrollTOTP(t *testing.T, e *env) (secret string, recovery []string) {
	t.Helper()
	enr, err := e.svc.EnrollTOTP(e.ctx)
	if err != nil {
		t.Fatalf("EnrollTOTP: %v", err)
	}
	raw, _ := decodeSecret(enr.Secret)
	recovery, err = e.svc.ConfirmTOTP(e.ctx, totpCode(raw, e.clk.Now()))
	if err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	return enr.Secret, recovery
}

func TestTOTPEnrollmentAndLogin(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	enr, err := e.svc.EnrollTOTP(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enr.URI, "otpauth://totp/") || !strings.Contains(enr.URI, enr.Secret) || !strings.HasPrefix(enr.QRSVG, "<svg") {
		t.Fatalf("enrollment = %+v", enr)
	}
	// Nothing is stored until the code is confirmed.
	if u, _ := e.db.GetUser(e.ctx); u.TOTPSecret != "" {
		t.Fatal("secret stored before confirmation")
	}
	if _, err := e.svc.ConfirmTOTP(e.ctx, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong confirm code: %v", err)
	}
	raw, _ := decodeSecret(enr.Secret)
	codes, err := e.svc.ConfirmTOTP(e.ctx, totpCode(raw, e.clk.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %d, want 10", len(codes))
	}
	if n, _ := e.db.UnusedRecoveryCodes(e.ctx); n != 10 {
		t.Fatalf("stored recovery codes = %d", n)
	}
	for _, c := range codes {
		var n int
		e.db.R.QueryRow("SELECT count(*) FROM recovery_code WHERE code_hash = ?", c).Scan(&n)
		if n != 0 {
			t.Fatal("recovery code stored in plain text")
		}
	}
	if acc, _ := e.svc.Account(e.ctx); !acc.TOTPEnabled || acc.Username != testUser {
		t.Fatalf("account = %+v", acc)
	}
	if _, err := e.svc.EnrollTOTP(e.ctx); !errors.Is(err, ErrTOTPEnabled) {
		t.Fatalf("enrol twice: %v", err)
	}

	// Login now needs a code. The confirm step consumed this time step, so move on.
	e.clk.Advance(30 * time.Second)
	if _, err := e.login("1.1.1.1"); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("login without code: %v, want ErrTOTPRequired", err)
	}
	req := LoginRequest{Username: testUser, Password: testPass, IP: "1.1.1.1", TOTP: "000000"}
	if _, err := e.svc.Login(e.ctx, req); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong code: %v", err)
	}
	req.TOTP = totpCode(raw, e.clk.Now())
	if _, err := e.svc.Login(e.ctx, req); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if _, err := e.svc.Login(e.ctx, req); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the same code twice: %v", err)
	}
	// A wrong password never reaches the code check, and the message stays the same.
	req.Password = "wrong password!"
	req.TOTP = ""
	if _, err := e.svc.Login(e.ctx, req); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password with TOTP on: %v", err)
	}
}

func TestMissingTOTPIsNotCountedAsFailure(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	enrollTOTP(t, e)
	for i := 0; i < 12; i++ {
		if _, err := e.login("1.1.1.1"); !errors.Is(err, ErrTOTPRequired) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
}

func TestWrongTOTPIsRateLimited(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	enrollTOTP(t, e)
	req := LoginRequest{Username: testUser, Password: testPass, IP: "1.1.1.1", TOTP: "111111"}
	for i := 0; i < 5; i++ {
		e.svc.Login(e.ctx, req)
	}
	if _, err := e.svc.Login(e.ctx, req); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limit", err)
	}
}

func TestRecoveryCodeWorksExactlyOnce(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	_, codes := enrollTOTP(t, e)
	use := func(code string) error {
		_, err := e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: testPass, IP: "1.1.1.1", TOTP: code})
		return err
	}
	// Dashes, case and spaces do not matter.
	if err := use(strings.ToUpper(codes[0])); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := use(codes[0]); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("second use: %v", err)
	}
	if err := use(strings.ReplaceAll(codes[1], "-", " ")); err != nil {
		t.Fatalf("other code: %v", err)
	}
	if n, _ := e.db.UnusedRecoveryCodes(e.ctx); n != 8 {
		t.Fatalf("unused = %d, want 8", n)
	}
}

func TestEnrollmentErrorsAndExpiry(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	if _, err := e.svc.ConfirmTOTP(e.ctx, "123456"); !errors.Is(err, ErrNoEnrollment) {
		t.Fatalf("confirm without enrol: %v", err)
	}
	enr, _ := e.svc.EnrollTOTP(e.ctx)
	raw, _ := decodeSecret(enr.Secret)
	e.clk.Advance(enrollmentTTL + time.Second)
	if _, err := e.svc.ConfirmTOTP(e.ctx, totpCode(raw, e.clk.Now())); !errors.Is(err, ErrNoEnrollment) {
		t.Fatalf("expired enrollment: %v", err)
	}
	// Enrolling again replaces the pending secret.
	enr1, _ := e.svc.EnrollTOTP(e.ctx)
	enr2, _ := e.svc.EnrollTOTP(e.ctx)
	if enr1.Secret == enr2.Secret {
		t.Fatal("secret reused")
	}
	raw1, _ := decodeSecret(enr1.Secret)
	if _, err := e.svc.ConfirmTOTP(e.ctx, totpCode(raw1, e.clk.Now())); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("code for the replaced secret: %v", err)
	}
}

func TestDisableTOTP(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	if err := e.svc.DisableTOTP(e.ctx, testPass); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("disable when off: %v", err)
	}
	enrollTOTP(t, e)
	if err := e.svc.DisableTOTP(e.ctx, "wrong password!"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := e.svc.DisableTOTP(e.ctx, testPass); err != nil {
		t.Fatal(err)
	}
	if u, _ := e.db.GetUser(e.ctx); u.TOTPSecret != "" {
		t.Fatal("secret still stored")
	}
	if n, _ := e.db.UnusedRecoveryCodes(e.ctx); n != 0 {
		t.Fatal("recovery codes still stored")
	}
	if _, err := e.login("1.1.1.1"); err != nil {
		t.Fatalf("login without TOTP: %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	cur := e.mustLogin()
	other := e.mustLogin()
	curHash := sha256hex(cur.Token)

	if err := e.svc.ChangePassword(e.ctx, "wrong password!", otherPW, curHash); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current: %v", err)
	}
	if err := e.svc.ChangePassword(e.ctx, testPass, "short", curHash); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short new: %v", err)
	}
	if err := e.svc.ChangePassword(e.ctx, testPass, otherPW, curHash); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.GetSession(e.ctx, curHash); err != nil {
		t.Fatalf("current session lost: %v", err)
	}
	if _, err := e.db.GetSession(e.ctx, sha256hex(other.Token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("other session survived a password change")
	}
	if _, err := e.login("1.1.1.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: otherPW, IP: "1.1.1.1"}); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestAccountPasswordGuessingIsRateLimited(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	for i := 0; i < 5; i++ {
		e.svc.ChangePassword(e.ctx, "wrong password!", otherPW, "x")
	}
	if err := e.svc.ChangePassword(e.ctx, testPass, otherPW, "x"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limit", err)
	}
}

func TestSessionListingAndSignOutOthers(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	a := e.mustLogin()
	e.clk.Advance(time.Minute)
	b := e.mustLogin()
	list, err := e.svc.ListSessions(e.ctx, sha256hex(a.Token))
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	current := 0
	for _, s := range list {
		if s.Current {
			current++
			if s.ID != sha256hex(a.Token)[:16] {
				t.Fatalf("current id %s", s.ID)
			}
		}
		if strings.Contains(s.ID, a.Token) || strings.Contains(s.ID, b.Token) || s.UserAgent != "test-agent" {
			t.Fatalf("session info %+v", s)
		}
	}
	if current != 1 {
		t.Fatalf("%d sessions marked current", current)
	}
	n, err := e.svc.SignOutOthers(e.ctx, sha256hex(a.Token))
	if err != nil || n != 1 {
		t.Fatalf("SignOutOthers = %d, %v", n, err)
	}
	list, _ = e.svc.ListSessions(e.ctx, sha256hex(a.Token))
	if len(list) != 1 || !list[0].Current {
		t.Fatalf("after sign-out: %v", list)
	}
}

func TestLogoutDeletesSession(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	res := e.mustLogin()
	if err := e.svc.Logout(e.ctx, sha256hex(res.Token)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.GetSession(e.ctx, sha256hex(res.Token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("session still there")
	}
}

func TestExpiredSessionsAreCleanedOnLogin(t *testing.T) {
	e := newEnv(t, Config{}).withAccount()
	old := e.mustLogin()
	e.clk.Advance(SessionTTL + time.Hour)
	e.mustLogin()
	if _, err := e.db.GetSession(e.ctx, sha256hex(old.Token)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("expired session not removed")
	}
}

// Secrets must never reach the log, except the one deliberate setup_token line.
func TestLogsNeverContainSecrets(t *testing.T) {
	e := newEnv(t, Config{})
	e.svc.InitSetup(e.ctx)
	tok := e.setupToken()
	if err := e.svc.Setup(e.ctx, SetupRequest{Token: tok, Username: testUser, Password: testPass, IP: "1.1.1.1"}); err != nil {
		t.Fatal(err)
	}
	res := e.mustLogin()
	e.svc.Login(e.ctx, LoginRequest{Username: testUser, Password: "wrong password!", IP: "1.1.1.1"})
	e.svc.Login(e.ctx, LoginRequest{Username: "nobody-" + otherPW, Password: otherPW, IP: "1.1.1.1"})
	secret, codes := enrollTOTP(t, e)
	e.svc.ChangePassword(e.ctx, "wrong password!", otherPW, "x")
	e.svc.ChangePassword(e.ctx, testPass, otherPW, sha256hex(res.Token))
	resetTok, _ := e.svc.ResetToken(e.ctx)

	logs := e.log.String()
	secrets := []string{testPass, otherPW, "wrong password!", res.Token, sha256hex(res.Token), secret, resetTok, codes[0], strings.ReplaceAll(codes[0], "-", "")}
	for _, s := range secrets {
		if strings.Contains(logs, s) {
			t.Errorf("log contains secret %q", s)
		}
	}
	if strings.Count(logs, tok) != 1 {
		t.Errorf("setup token occurs %d times in the log, want exactly 1", strings.Count(logs, tok))
	}
}

func TestTOTPUsesSHA1SixDigitsLikeAuthenticatorApps(t *testing.T) {
	// Guard against accidental parameter changes: authenticator apps assume these.
	raw := []byte("12345678901234567890")
	if hotp(raw, 1, 6, sha1.New) != "287082" {
		t.Fatal("HOTP counter 1 must give 287082 (RFC 4226)")
	}
}
