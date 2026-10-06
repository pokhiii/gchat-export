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
	"sync/atomic"
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
	data, err := os.ReadFile(path) // #nosec G304 -- user-chosen OAuth client file
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

const shutdownGrace = 2 * time.Second

const callbackPage = "Login complete. You can close this tab and return to the terminal.\n"

// Login runs the OAuth installed-app flow with PKCE on a loopback listener
// bound to 127.0.0.1. openBrowser is asked to open the consent URL; if it
// fails, the URL is printed to stderr.
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error, timeout time.Duration) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://%s/callback", ln.Addr().String())

	state, err := randomState()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	var handled atomic.Bool
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
		if !handled.CompareAndSwap(false, true) {
			http.Error(w, "already handled", http.StatusConflict)
			return
		}
		// Deliver the page before handing over the code, so shutting the
		// server down can never cut the browser's response short.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(callbackPage))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		results <- res
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	// Graceful: lets the in-flight callback response finish before the port
	// closes; otherwise the browser retries and shows "refused to connect".
	shutdown := func() {
		sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}
	defer shutdown()

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
		shutdown() // stop accepting callbacks before the exchange
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
