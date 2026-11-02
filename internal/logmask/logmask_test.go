package logmask

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSensitiveNames(t *testing.T) {
	for _, k := range []string{"password", "Password", "new_password", "currentPassword", "api_key", "apiKey", "API-KEY", "token",
		"session_token", "secret", "client.secret", "Authorization", "cookie", "totp", "recovery_code", "passwd"} {
		if !Sensitive(k) {
			t.Errorf("%q is not masked", k)
		}
	}
	for _, k := range []string{"setup_token", "path", "ip", "method", "url", "user_agent", "count", "interface", "key"} {
		if Sensitive(k) {
			t.Errorf("%q is masked", k)
		}
	}
}

func TestLoggerMasksValuesAtAnyDepth(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: ReplaceAttr}))
	log.Info("x", "password", "hunter2", "ip", "1.2.3.4", "setup_token", "visible-token",
		slog.Group("req", slog.String("api_key", "k-123"), slog.String("path", "/x")),
		slog.Any("map", map[string]string{"ok": "fine"}))
	log.With("token", "t-456").Warn("y", slog.Int("password", 12345))
	out := buf.String()
	for _, leak := range []string{"hunter2", "k-123", "t-456", "12345"} {
		if strings.Contains(out, leak) {
			t.Errorf("the log holds %q: %s", leak, out)
		}
	}
	for _, want := range []string{"1.2.3.4", "visible-token", "/x", "fine", Masked} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lost %q: %s", want, out)
		}
	}
}
