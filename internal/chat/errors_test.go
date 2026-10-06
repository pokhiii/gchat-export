package chat

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

func TestClassify(t *testing.T) {
	other := errors.New("boom")
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"401", &googleapi.Error{Code: 401}, ErrUnauthenticated},
		{"invalid_grant", fmt.Errorf("wrapped: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), ErrUnauthenticated},
		{"service disabled", &googleapi.Error{Code: 403, Details: []any{map[string]any{"reason": "SERVICE_DISABLED"}}}, ErrAccessNotConfigured},
		{"access not configured", &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "accessNotConfigured"}}}, ErrAccessNotConfigured},
		{"404", &googleapi.Error{Code: 404}, ErrSpaceNotFound},
		{"other", other, other},
	}
	for _, c := range cases {
		if got := Classify(c.in); !errors.Is(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	var se *ScopeError
	in := &googleapi.Error{Code: 403, Message: "Request had insufficient authentication scopes.", Details: []any{map[string]any{"reason": "ACCESS_TOKEN_SCOPE_INSUFFICIENT"}}}
	if got := Classify(in); !errors.As(got, &se) || se.Detail == "" {
		t.Fatalf("scope: %v", got)
	}
	if Classify(nil) != nil {
		t.Fatal("nil not preserved")
	}
}
