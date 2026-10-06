package auth

import (
	"context"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

func TestPersistingTokenSourceSavesOnRefresh(t *testing.T) {
	keyring.MockInit()
	ts := newFakeTokenServer(t)
	store := NewKeyringStore()
	cred := &Credential{Email: "me@example.com", Token: &oauth2.Token{
		AccessToken: "old", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour),
	}}
	src := PersistingTokenSource(context.Background(), testConfig(ts.URL), cred, store)
	tok, err := src.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken == "old" {
		t.Fatal("token not refreshed")
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Token.AccessToken != tok.AccessToken || saved.Email != "me@example.com" {
		t.Fatalf("saved %+v", saved)
	}
	// A second call with a valid token must not hit the token endpoint again.
	src.Token()
	if len(ts.grants) != 1 {
		t.Fatalf("token endpoint called %d times", len(ts.grants))
	}
}
