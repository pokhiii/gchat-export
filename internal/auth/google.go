package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Endpoints are Google's token-info and revocation URLs, overridable in tests.
type Endpoints struct {
	TokenInfo string
	Revoke    string
}

var DefaultEndpoints = Endpoints{
	TokenInfo: "https://oauth2.googleapis.com/tokeninfo",
	Revoke:    "https://oauth2.googleapis.com/revoke",
}

// FetchTokenInfo returns the account email and granted scopes for an access
// token. The token is sent in the POST body, never the URL.
func FetchTokenInfo(ctx context.Context, hc *http.Client, ep Endpoints, accessToken string) (string, []string, error) {
	body, err := postForm(ctx, hc, ep.TokenInfo, url.Values{"access_token": {accessToken}})
	if err != nil {
		return "", nil, fmt.Errorf("token info: %w", err)
	}
	var info struct {
		Email string `json:"email"`
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", nil, fmt.Errorf("token info: unexpected response")
	}
	return info.Email, strings.Fields(info.Scope), nil
}

// Revoke revokes a token at Google. Revoking a refresh token also revokes
// its access tokens.
func Revoke(ctx context.Context, hc *http.Client, ep Endpoints, token string) error {
	if _, err := postForm(ctx, hc, ep.Revoke, url.Values{"token": {token}}); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	return nil
}

func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// The body is a Google error object; it never contains the token.
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &e)
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, sanitizeParam(e.Error))
	}
	return body, nil
}
