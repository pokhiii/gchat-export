package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// formServer asserts the request is a POST whose body (not URL) carries
// field=want, then replies with status and body.
func formServer(t *testing.T, field, want string, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if strings.Contains(r.URL.String(), want) {
			t.Errorf("token leaked into URL: %s", r.URL)
		}
		r.ParseForm()
		if r.PostForm.Get(field) != want {
			t.Errorf("form %s = %q", field, r.PostForm.Get(field))
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchTokenInfoUsesPOSTBody(t *testing.T) {
	srv := formServer(t, "access_token", "secret-at", 200, `{"email":"me@example.com","scope":"openid email https://x/y"}`)
	email, scopes, err := FetchTokenInfo(context.Background(), srv.Client(), Endpoints{TokenInfo: srv.URL}, "secret-at")
	if err != nil {
		t.Fatal(err)
	}
	if email != "me@example.com" || strings.Join(scopes, ",") != "openid,email,https://x/y" {
		t.Fatalf("got %q %v", email, scopes)
	}
}

func TestFetchTokenInfoError(t *testing.T) {
	srv := formServer(t, "access_token", "bad", 400, `{"error":"invalid_token"}`)
	if _, _, err := FetchTokenInfo(context.Background(), srv.Client(), Endpoints{TokenInfo: srv.URL}, "bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRevokePOST(t *testing.T) {
	srv := formServer(t, "token", "secret-rt", 200, "")
	if err := Revoke(context.Background(), srv.Client(), Endpoints{Revoke: srv.URL}, "secret-rt"); err != nil {
		t.Fatal(err)
	}
	bad := formServer(t, "token", "secret-rt", 400, `{"error":"invalid_token"}`)
	if err := Revoke(context.Background(), bad.Client(), Endpoints{Revoke: bad.URL}, "secret-rt"); err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestDefaultEndpoints(t *testing.T) {
	if DefaultEndpoints.TokenInfo != "https://oauth2.googleapis.com/tokeninfo" || DefaultEndpoints.Revoke != "https://oauth2.googleapis.com/revoke" {
		t.Fatalf("%+v", DefaultEndpoints)
	}
}
