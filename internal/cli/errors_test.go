package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/OWNER/gchat-export/internal/auth"
	"github.com/OWNER/gchat-export/internal/chat"
	"github.com/OWNER/gchat-export/internal/export"
	"github.com/OWNER/gchat-export/internal/model"
)

func TestUserMessage(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{auth.ErrNoCredential, "Run: gchat-export auth login"},
		{fmt.Errorf("x: %w", chat.ErrUnauthenticated), "Run: gchat-export auth login"},
		{&chat.ScopeError{Detail: "insufficient scopes"}, "Missing permission (insufficient scopes)"},
		{chat.ErrAccessNotConfigured, "docs/setup.md#enable-the-api"},
		{chat.ErrSpaceNotFound, "gchat-export spaces --filter"},
		{fmt.Errorf("x: %w", export.ErrOutputExists), "--resume or --force"},
		{export.ErrStateMismatch, "--force"},
		{export.ErrNoState, "--force"},
		{chat.ErrBadChatURL, "Unrecognized Google Chat URL"},
		{errors.New("raw ya29.secret"), "raw [REDACTED]"},
	}
	for _, c := range cases {
		if got := userMessage(c.err); !strings.Contains(got, c.want) {
			t.Errorf("userMessage(%v) = %q, want substring %q", c.err, got, c.want)
		}
	}
	amb := &chat.AmbiguousSpaceError{Candidates: []model.Space{{Name: "spaces/A", DisplayName: "Dup"}, {Name: "spaces/B", DisplayName: "Dup"}}}
	got := userMessage(amb)
	if !strings.Contains(got, "spaces/A") || !strings.Contains(got, "spaces/B") {
		t.Fatalf("ambiguous: %q", got)
	}
}

func TestTermSafe(t *testing.T) {
	if got := termSafe("evil\x1b]0;title\x07name\r\n"); strings.ContainsAny(got, "\x1b\x07\r\n") {
		t.Fatalf("control chars survived: %q", got)
	}
	if got := termSafe("Team Room 🎉"); got != "Team Room 🎉" {
		t.Fatalf("plain altered: %q", got)
	}
}
