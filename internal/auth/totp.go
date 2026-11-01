package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

const (
	totpPeriod      = 30
	totpDigits      = 6
	totpIssuer      = "JDWeb"
	recoveryCodeNum = 10
)

var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// hotp is RFC 4226 with a selectable hash, which RFC 6238 allows.
func hotp(key []byte, counter uint64, digits int, h func() hash.Hash) string {
	mac := hmac.New(h, key)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode returns the six-digit SHA-1 code authenticator apps show at t.
func totpCode(secret []byte, t time.Time) string {
	return hotp(secret, uint64(totpStep(t)), totpDigits, sha1.New)
}

// verifyTOTP accepts the codes of the previous, current and next 30-second
// step to tolerate clock drift. A step at or below lastStep counts as a
// replay and is refused. It returns the step that matched.
func verifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	var matched int64
	found := false
	cur := totpStep(now)
	for step := cur - 1; step <= cur+1; step++ {
		want := hotp(secret, uint64(step), totpDigits, sha1.New)
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && step > lastStep && !found {
			matched, found = step, true
		}
	}
	return matched, found
}

// newTOTPSecret returns 160 random bits as unpadded Base32.
func newTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return secretEncoding.EncodeToString(b), nil
}

func decodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	return secretEncoding.DecodeString(s)
}

// otpauthURI is the Key URI format that authenticator apps import.
func otpauthURI(secret, account string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(totpIssuer+":"+account) + "?" + q.Encode()
}

// qrSVG renders text as a QR code in a self-contained SVG without scripts,
// links or external references. The image has a four-module quiet zone.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var path strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			start := x
			for x < code.Size && code.Black(x, y) {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start+quiet, y+quiet, x-start, x-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		size, size, size, size, path.String()), nil
}

var recoveryEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// newRecoveryCodes returns ten codes of the form xxxx-xxxx-xxxx-xxxx. Each
// carries 80 random bits: they are stored as a plain SHA-256, so they need
// enough entropy to make offline guessing hopeless.
func newRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeNum)
	seen := map[string]bool{}
	for len(codes) < recoveryCodeNum {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		raw := recoveryEncoding.EncodeToString(b) // 16 characters
		c := raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
		if !seen[c] {
			seen[c] = true
			codes = append(codes, c)
		}
	}
	return codes, nil
}

// hashRecoveryCode ignores case, dashes and spaces and returns the hex SHA-256.
func hashRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
