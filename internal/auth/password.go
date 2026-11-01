// Package auth implements the single-account login of JDWeb: Argon2id
// passwords, sessions, TOTP two-factor, recovery codes, the one-time setup
// token, rate limiting and the HTTP middleware that enforces them.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordLength and MaxPasswordLength count characters, not bytes.
	MinPasswordLength = 12
	MaxPasswordLength = 256
	maxUsernameLength = 64

	// Argon2id parameters follow the OWASP minimum (19 MiB, 2 passes) and keep
	// the memory budget of the container small.
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	// Upper bounds for parameters read from a stored hash, so a damaged or
	// hostile value cannot make verification allocate gigabytes.
	maxArgonMemory  = 256 * 1024
	maxArgonTime    = 10
	maxArgonThreads = 8
)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", MaxPasswordLength)
	ErrInvalidUsername  = errors.New("username must be 1 to 64 characters without control characters")
)

// hashSlots limits concurrent Argon2 runs. Each needs 19 MiB, and a burst of
// login attempts must not blow up the memory of the container.
var hashSlots = make(chan struct{}, 2)

func argonKey(pw string, salt []byte, t, m uint32, p uint8, n uint32) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey([]byte(pw), salt, t, m, p, n)
}

// HashPassword returns an Argon2id hash in the PHC string format.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argonKey(pw, salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword compares pw with a PHC hash in constant time. Malformed
// hashes never match.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	if m == 0 || m > maxArgonMemory || t == 0 || t > maxArgonTime || p == 0 || p > maxArgonThreads {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(salt) > 64 || len(want) < 16 || len(want) > 64 {
		return false
	}
	got := argonKey(pw, salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword enforces the length rules.
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < MinPasswordLength:
		return ErrPasswordTooShort
	case n > MaxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}

// ValidateUsername accepts any printable name of 1 to 64 characters.
func ValidateUsername(name string) error {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n == 0 || n > maxUsernameLength {
		return ErrInvalidUsername
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return ErrInvalidUsername
		}
	}
	return nil
}
