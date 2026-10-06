package cli

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactingHandler(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	secrets := []string{
		"ya29.a0AfH6SMBx-secret_value",
		"1//0gLongRefreshToken-abc_123",
		"Bearer abc.def.ghi",
		`"access_token": "zzz-secret"`,
		`"refresh_token":"rrr-secret"`,
		`"id_token": "iii-secret"`,
	}
	for _, s := range secrets {
		log.Info("msg "+s, "attr", s, "err", errors.New("wrapped "+s))
		log.With("pre", s).WithGroup("g").Info("x", "k", s)
	}
	out := buf.String()
	for _, leak := range []string{"secret_value", "LongRefreshToken", "abc.def.ghi", "zzz-secret", "rrr-secret", "iii-secret"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatal("no redaction marker")
	}
}

func TestRedactString(t *testing.T) {
	if got := redact("token ya29.abc ok"); got != "token [REDACTED] ok" {
		t.Fatalf("got %q", got)
	}
	if got := redact("nothing here"); got != "nothing here" {
		t.Fatalf("got %q", got)
	}
}
