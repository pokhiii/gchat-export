package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type fakeTokenServer struct {
	*httptest.Server
	mu       sync.Mutex
	verifier string
	grants   []string
}

func newFakeTokenServer(t *testing.T) *fakeTokenServer {
	t.Helper()
	f := &fakeTokenServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		f.verifier = r.PostForm.Get("code_verifier")
		f.grants = append(f.grants, r.PostForm.Get("grant_type"))
		n := len(f.grants)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + string(rune('0'+n)), "refresh_token": "refresh",
			"token_type": "Bearer", "expires_in": 3600,
		})
	}))
	t.Cleanup(f.Close)
	return f
}

func testConfig(tokenURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: "cid", ClientSecret: "csecret", Scopes: Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.example/auth", TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

func callback(t *testing.T, authURL, state string, extra url.Values) *http.Response {
	t.Helper()
	u, _ := url.Parse(authURL)
	q := url.Values{"state": {state}, "code": {"the-code"}}
	for k, v := range extra {
		q[k] = v
	}
	resp, err := http.Get(u.Query().Get("redirect_uri") + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func stateOf(authURL string) string {
	u, _ := url.Parse(authURL)
	return u.Query().Get("state")
}

func TestLoginHappyPath(t *testing.T) {
	ts := newFakeTokenServer(t)
	var authURL string
	tok, err := Login(context.Background(), testConfig(ts.URL), func(u string) error {
		authURL = u
		if r := callback(t, u, stateOf(u), nil); r.StatusCode != 200 {
			t.Errorf("callback status %d", r.StatusCode)
		}
		return nil
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "refresh" {
		t.Fatalf("token %+v", tok)
	}
	sum := sha256.Sum256([]byte(ts.verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	u, _ := url.Parse(authURL)
	if ts.verifier == "" || u.Query().Get("code_challenge") != challenge {
		t.Fatal("PKCE verifier does not match challenge")
	}
}

func TestLoginBindsLoopbackOnly(t *testing.T) {
	ts := newFakeTokenServer(t)
	_, err := Login(context.Background(), testConfig(ts.URL), func(u string) error {
		pu, _ := url.Parse(u)
		ru, _ := url.Parse(pu.Query().Get("redirect_uri"))
		if ru.Hostname() != "127.0.0.1" || ru.Path != "/callback" {
			t.Errorf("redirect_uri = %s", ru)
		}
		callback(t, u, stateOf(u), nil)
		return nil
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoginRejectsWrongState(t *testing.T) {
	ts := newFakeTokenServer(t)
	_, err := Login(context.Background(), testConfig(ts.URL), func(u string) error {
		if r := callback(t, u, "wrong", nil); r.StatusCode != http.StatusBadRequest {
			t.Errorf("wrong state status %d", r.StatusCode)
		}
		callback(t, u, stateOf(u), nil)
		return nil
	}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoginProviderError(t *testing.T) {
	ts := newFakeTokenServer(t)
	_, err := Login(context.Background(), testConfig(ts.URL), func(u string) error {
		callback(t, u, stateOf(u), url.Values{"error": {"access_denied"}, "code": {""}})
		return nil
	}, 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	ts := newFakeTokenServer(t)
	var redirect string
	_, err := Login(context.Background(), testConfig(ts.URL), func(u string) error {
		pu, _ := url.Parse(u)
		redirect = pu.Query().Get("redirect_uri")
		return nil
	}, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	ru, _ := url.Parse(redirect)
	if c, err := net.DialTimeout("tcp", ru.Host, 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("listener still open after timeout")
	}
}

func TestLoginAuthURLParams(t *testing.T) {
	ts := newFakeTokenServer(t)
	Login(context.Background(), testConfig(ts.URL), func(u string) error {
		q, _ := url.Parse(u)
		v := q.Query()
		if v.Get("access_type") != "offline" || v.Get("prompt") != "consent" || v.Get("code_challenge_method") != "S256" {
			t.Errorf("params %v", v)
		}
		for _, s := range Scopes {
			if !strings.Contains(" "+v.Get("scope")+" ", " "+s+" ") {
				t.Errorf("scope %q missing", s)
			}
		}
		if len(v.Get("state")) < 32 {
			t.Errorf("state too short: %q", v.Get("state"))
		}
		callback(t, u, stateOf(u), nil)
		return nil
	}, 5*time.Second)
}

func TestScopesAreReadOnly(t *testing.T) {
	want := []string{
		"https://www.googleapis.com/auth/chat.spaces.readonly",
		"https://www.googleapis.com/auth/chat.messages.readonly",
		"https://www.googleapis.com/auth/chat.memberships.readonly",
		"openid", "email",
	}
	if strings.Join(Scopes, " ") != strings.Join(want, " ") {
		t.Fatalf("Scopes = %v", Scopes)
	}
}

func TestLoadClientConfig(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "installed.json")
	os.WriteFile(installed, []byte(`{"installed":{"client_id":"cid","client_secret":"cs","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost"]}}`), 0o600)
	cfg, err := LoadClientConfig(installed)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "cid" || len(cfg.Scopes) != len(Scopes) {
		t.Fatalf("cfg %+v", cfg)
	}
	web := filepath.Join(dir, "web.json")
	os.WriteFile(web, []byte(`{"web":{"client_id":"cid","client_secret":"cs","auth_uri":"a","token_uri":"b"}}`), 0o600)
	if _, err := LoadClientConfig(web); err == nil {
		t.Fatal("web client accepted")
	}
}
