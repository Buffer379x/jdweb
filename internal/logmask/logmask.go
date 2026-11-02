// Package logmask keeps secrets out of the log (Concept 9.4). It is a safety
// net for the day somebody logs a value they should not: the code does not
// log secrets, and this makes sure a slip does not write them down.
package logmask

import (
	"log/slog"
	"strings"
)

// Masked replaces the value of an attribute whose name looks like a secret.
const Masked = "[masked]"

// names are parts of attribute names (compared lower case, `_` and `-` removed).
var names = []string{"password", "passwd", "passphrase", "secret", "token", "apikey", "authorization", "cookie", "credential", "totp", "recoverycode"}

// allowed are attributes that look like a secret and are meant to be logged:
// the setup token that the operator reads in the log (Concept 9.2).
var allowed = map[string]bool{"setup_token": true}

// Sensitive reports whether an attribute name looks like it holds a secret.
func Sensitive(key string) bool {
	if allowed[key] {
		return false
	}
	k := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	for _, n := range names {
		if strings.Contains(k, n) {
			return true
		}
	}
	return false
}

// ReplaceAttr is for slog.HandlerOptions.ReplaceAttr. It masks the value of a
// sensitive attribute at any depth of a group.
func ReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		return a // its members pass through ReplaceAttr on their own
	}
	if Sensitive(a.Key) {
		return slog.String(a.Key, Masked)
	}
	return a
}
