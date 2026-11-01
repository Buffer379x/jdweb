package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordIsArgon2id(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected format: %s", h)
	}
	if strings.Contains(h, "correct horse") {
		t.Fatal("hash contains the password")
	}
	if !VerifyPassword(h, "correct horse battery") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(h, "correct horse batterY") || VerifyPassword(h, "") {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("two hashes of the same password are identical: salt missing")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$argon2i$v=19$m=19456,t=2,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=x,t=2,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$AAAA",
		// Parameters that would make verification a denial-of-service lever.
		"$argon2id$v=19$m=4194304,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		if VerifyPassword(h, "x") {
			t.Errorf("accepted %q", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw   string
		want error
	}{
		{"elevenchars", ErrPasswordTooShort},
		{"twelve chars", nil},
		{strings.Repeat("ä", 11), ErrPasswordTooShort}, // counts characters, not bytes
		{strings.Repeat("ä", 12), nil},
		{strings.Repeat("a", 256), nil},
		{strings.Repeat("a", 257), ErrPasswordTooLong},
		{"", ErrPasswordTooShort},
	}
	for _, c := range cases {
		if err := ValidatePassword(c.pw); !errors.Is(err, c.want) {
			t.Errorf("ValidatePassword(%d runes) = %v, want %v", len([]rune(c.pw)), err, c.want)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	for _, u := range []string{"admin", "Max M.", "ü"} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v", u, err)
		}
	}
	for _, u := range []string{"", "   ", "a\nb", strings.Repeat("x", 65), "a\x00"} {
		if err := ValidateUsername(u); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("ValidateUsername(%q) = %v, want ErrInvalidUsername", u, err)
		}
	}
}
