package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scopes are the only OAuth scopes the tool ever requests.
var Scopes = []string{
	"https://www.googleapis.com/auth/chat.spaces.readonly",
	"https://www.googleapis.com/auth/chat.messages.readonly",
	"https://www.googleapis.com/auth/chat.memberships.readonly",
	"openid",
	"email",
}

// LoadClientConfig reads a Desktop ("installed") OAuth client JSON file.
func LoadClientConfig(path string) (*oauth2.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading OAuth client file: %w", err)
	}
	var kind map[string]json.RawMessage
	if err := json.Unmarshal(data, &kind); err != nil {
		return nil, errors.New("OAuth client file is not valid JSON")
	}
	if _, ok := kind["installed"]; !ok {
		return nil, errors.New(`OAuth client must be of type "Desktop app" (see docs/setup.md)`)
	}
	return google.ConfigFromJSON(data, Scopes...)
}

const callbackPage = "Login complete. You can close this tab and return to the terminal.\n"

// Login runs the OAuth installed-app flow with PKCE on a loopback listener
// bound to 127.0.0.1. openBrowser is asked to open the consent URL; if it
// fails, the URL is printed to stderr.
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error, timeout time.Duration) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://%s/callback", ln.Addr().String())

	state, err := randomState()
	if err != nil {
		ln.Close()
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}
		res := result{code: q.Get("code")}
		if e := q.Get("error"); e != "" {
			res = result{err: fmt.Errorf("authorization failed: %s", sanitizeParam(e))}
		} else if res.code == "" {
			res = result{err: errors.New("authorization failed: no code returned")}
		}
		select {
		case results <- res:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(callbackPage))
		default:
			http.Error(w, "already handled", http.StatusConflict)
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	authURL := c.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.S256ChallengeOption(verifier))
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Open this URL in your browser to log in:\n\n  %s\n\n", authURL)
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("login timed out waiting for browser authorization: %w", ctx.Err())
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		srv.Close()
		return c.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	}
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sanitizeParam keeps provider error codes printable and short.
func sanitizeParam(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if len(out) == 64 {
			break
		}
		if r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		}
	}
	return string(out)
}
