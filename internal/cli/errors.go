package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/OWNER/gchat-export/internal/auth"
	"github.com/OWNER/gchat-export/internal/chat"
	"github.com/OWNER/gchat-export/internal/export"
)

// userMessage turns an error into an actionable, redacted message.
func userMessage(err error) string {
	var scope *chat.ScopeError
	var amb *chat.AmbiguousSpaceError
	switch {
	case errors.Is(err, auth.ErrNoCredential), errors.Is(err, chat.ErrUnauthenticated):
		return "Not logged in or token revoked. Run: gchat-export auth login"
	case errors.As(err, &scope):
		return fmt.Sprintf("Missing permission (%s). Run: gchat-export auth login", termSafe(redact(scope.Detail)))
	case errors.Is(err, chat.ErrAccessNotConfigured):
		return "Chat API not enabled or app not allowed by your Workspace admin. See docs/setup.md#enable-the-api"
	case errors.Is(err, chat.ErrBadChatURL):
		return "Unrecognized Google Chat URL. Copy the link from chat.google.com, or use: gchat-export spaces --filter <text>"
	case errors.Is(err, chat.ErrSpaceNotFound):
		return "Space not found. Try: gchat-export spaces --filter <text>"
	case errors.As(err, &amb):
		var b strings.Builder
		b.WriteString("Display name matches several spaces; pass one of these to --space:")
		for _, s := range amb.Candidates {
			fmt.Fprintf(&b, "\n  %s  %s", s.Name, termSafe(s.DisplayName))
		}
		return b.String()
	case errors.Is(err, export.ErrOutputExists):
		return termSafe(redact(err.Error())) + ". Use --resume or --force."
	case errors.Is(err, export.ErrNoState):
		return termSafe(redact(err.Error())) + ". Run without --resume to start a new export (or --force to replace it)."
	case errors.Is(err, export.ErrStateMismatch):
		return termSafe(redact(err.Error())) + ". Use --force to start over."
	}
	return termSafe(redact(err.Error()))
}

// termSafe removes control characters (terminal escape sequences, CR/LF) from
// untrusted text before printing it.
func termSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
