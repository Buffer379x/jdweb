package auth

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"hash"
	"net/url"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test vectors (8 digits, 30 s step).
func TestTOTPRFC6238Vectors(t *testing.T) {
	seeds := map[string][]byte{
		"sha1":   []byte("12345678901234567890"),
		"sha256": []byte("12345678901234567890123456789012"),
		"sha512": []byte("1234567890123456789012345678901234567890123456789012345678901234"),
	}
	hashes := map[string]func() hash.Hash{"sha1": sha1.New, "sha256": sha256.New, "sha512": sha512.New}
	vectors := []struct {
		unix                 int64
		sha1, sha256, sha512 string
	}{
		{59, "94287082", "46119246", "90693936"},
		{1111111109, "07081804", "68084774", "25091201"},
		{1111111111, "14050471", "67062674", "99943326"},
		{1234567890, "89005924", "91819424", "93441116"},
		{2000000000, "69279037", "90698825", "38618901"},
		{20000000000, "65353130", "77737706", "47863826"},
	}
	for _, v := range vectors {
		step := uint64(v.unix / 30)
		for name, want := range map[string]string{"sha1": v.sha1, "sha256": v.sha256, "sha512": v.sha512} {
			if got := hotp(seeds[name], step, 8, hashes[name]); got != want {
				t.Errorf("T=%d %s: got %s, want %s", v.unix, name, got, want)
			}
		}
	}
}

func TestTOTPCodeIsSixDigitsSHA1(t *testing.T) {
	secret := []byte("12345678901234567890")
	// 6-digit code is the last six digits of the 8-digit vector at T=59.
	if got := totpCode(secret, time.Unix(59, 0)); got != "287082" {
		t.Fatalf("got %s, want 287082", got)
	}
}

func TestTOTPVerifyWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	cur := totpStep(now)
	code := func(step int64) string { return hotp(secret, uint64(step), 6, sha1.New) }

	if _, ok := verifyTOTP(secret, code(cur), now, 0); !ok {
		t.Fatal("current code rejected")
	}
	if _, ok := verifyTOTP(secret, code(cur-1), now, 0); !ok {
		t.Fatal("previous step rejected (clock drift)")
	}
	if _, ok := verifyTOTP(secret, code(cur+1), now, 0); !ok {
		t.Fatal("next step rejected (clock drift)")
	}
	if _, ok := verifyTOTP(secret, code(cur-2), now, 0); ok {
		t.Fatal("two steps back accepted")
	}
	if _, ok := verifyTOTP(secret, code(cur+2), now, 0); ok {
		t.Fatal("two steps ahead accepted")
	}
	// A step that was already used (or an older one) is a replay.
	if _, ok := verifyTOTP(secret, code(cur), now, cur); ok {
		t.Fatal("replay of the used step accepted")
	}
	if _, ok := verifyTOTP(secret, code(cur-1), now, cur); ok {
		t.Fatal("older step accepted after a newer one was used")
	}
	if step, ok := verifyTOTP(secret, code(cur+1), now, cur); !ok || step != cur+1 {
		t.Fatalf("newer step rejected: %d %v", step, ok)
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if _, ok := verifyTOTP(secret, bad, now, 0); ok {
			t.Errorf("malformed code %q accepted", bad)
		}
	}
}

func TestNewTOTPSecret(t *testing.T) {
	s1, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := newTOTPSecret()
	if s1 == s2 || len(s1) != 32 || strings.ToUpper(s1) != s1 {
		t.Fatalf("secrets %q %q", s1, s2)
	}
	if raw, err := decodeSecret(s1); err != nil || len(raw) != 20 {
		t.Fatalf("decode: %d bytes, %v", len(raw), err)
	}
}

func TestOTPAuthURI(t *testing.T) {
	uri := otpauthURI("JBSWY3DPEHPK3PXP", "Ann & Bob")
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "otpauth" || u.Host != "totp" || q.Get("secret") != "JBSWY3DPEHPK3PXP" ||
		q.Get("issuer") != "JDWeb" || q.Get("algorithm") != "SHA1" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Fatalf("bad URI %s", uri)
	}
	if !strings.HasPrefix(u.Path, "/JDWeb:") || !strings.Contains(u.Path, "Ann & Bob") {
		t.Fatalf("label %q", u.Path)
	}
}

func TestQRSVG(t *testing.T) {
	svg, err := qrSVG("otpauth://totp/JDWeb:admin?secret=JBSWY3DPEHPK3PXP&issuer=JDWeb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, "<svg ") || !strings.Contains(svg, "viewBox=") || !strings.Contains(svg, "<path") ||
		!strings.HasSuffix(svg, "</svg>") {
		t.Fatalf("not an SVG: %.120s", svg)
	}
	for _, bad := range []string{"<script", "javascript:", "onload", "href"} {
		if strings.Contains(svg, bad) {
			t.Errorf("SVG contains %q", bad)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("format %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate %q", c)
		}
		seen[c] = true
		if hashRecoveryCode(strings.ToUpper(c)) != hashRecoveryCode(c) {
			t.Errorf("case matters for %q", c)
		}
		if hashRecoveryCode(strings.ReplaceAll(c, "-", " ")) != hashRecoveryCode(c) {
			t.Errorf("separator matters for %q", c)
		}
		if strings.Contains(hashRecoveryCode(c), strings.ReplaceAll(c, "-", "")) {
			t.Errorf("hash contains the code")
		}
	}
}
