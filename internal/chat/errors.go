package chat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pokhiii/gchat-export/internal/model"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

var (
	ErrUnauthenticated     = errors.New("not authenticated")
	ErrAccessNotConfigured = errors.New("chat API not enabled or app not allowed")
	ErrSpaceNotFound       = errors.New("space not found")
	ErrBadChatURL          = errors.New("unrecognized Google Chat URL")
)

// ScopeError means the token lacks a required OAuth scope.
type ScopeError struct{ Detail string }

func (e *ScopeError) Error() string { return "insufficient OAuth scope: " + e.Detail }

// AmbiguousSpaceError means a display name matched more than one space.
type AmbiguousSpaceError struct{ Candidates []model.Space }

func (e *AmbiguousSpaceError) Error() string {
	return fmt.Sprintf("display name matches %d spaces", len(e.Candidates))
}

// Classify maps API and OAuth errors to the package's typed errors; other
// errors are returned unchanged.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		return ErrUnauthenticated
	}
	var ge *googleapi.Error
	if !errors.As(err, &ge) {
		return err
	}
	switch ge.Code {
	case 401:
		return ErrUnauthenticated
	case 404:
		return ErrSpaceNotFound
	case 403:
		reasons := reasonsOf(ge)
		switch {
		case strings.Contains(reasons, "ACCESS_TOKEN_SCOPE_INSUFFICIENT"), strings.Contains(reasons, "insufficientPermissions"):
			return &ScopeError{Detail: ge.Message}
		case strings.Contains(reasons, "SERVICE_DISABLED"), strings.Contains(reasons, "accessNotConfigured"):
			return ErrAccessNotConfigured
		}
	}
	return err
}

func reasonsOf(ge *googleapi.Error) string {
	var b strings.Builder
	for _, item := range ge.Errors {
		b.WriteString(item.Reason + " ")
	}
	for _, d := range ge.Details {
		if m, ok := d.(map[string]any); ok {
			if r, ok := m["reason"].(string); ok {
				b.WriteString(r + " ")
			}
		}
	}
	return b.String()
}
