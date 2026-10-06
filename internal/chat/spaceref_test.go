package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSpaceRef(t *testing.T) {
	good := map[string]string{
		"spaces/AAAA": "spaces/AAAA",
		"https://chat.google.com/app/chat/AAAAUKFhXM0":      "spaces/AAAAUKFhXM0",
		"https://chat.google.com/u/1/app/chat/AAAA":         "spaces/AAAA",
		"https://chat.google.com/room/AAAA":                 "spaces/AAAA",
		"https://chat.google.com/room/AAAA/Xyz123?cls=7":    "spaces/AAAA",
		"https://chat.google.com/app/chat/AAAA#x":           "spaces/AAAA",
		"https://chat.google.com/dm/BBBB":                   "spaces/BBBB",
		"https://Chat.Google.com/room/AAAA":                 "spaces/AAAA",
		"https://mail.google.com/chat/u/0/#chat/space/AAAA": "spaces/AAAA",
		"https://mail.google.com/chat/u/0/#chat/dm/BBBB":    "spaces/BBBB",
		"https://mail.google.com/mail/u/0/#chat/space/AAAA": "spaces/AAAA",
	}
	for in, want := range good {
		if got, ok := ParseSpaceRef(in); !ok || got != want {
			t.Errorf("ParseSpaceRef(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	bad := []string{
		"http://chat.google.com/room/AAAA",
		"https://chat.google.com.evil.example/room/AAAA",
		"https://evilchat.google.com/room/AAAA",
		"https://chat.google.com/settings",
		"https://chat.google.com/room/AA%2FAA",
		"https://mail.google.com/mail/u/0/#inbox",
		"https://mail.google.com/chat/u/0/#chat/space/AA$AA",
		"spaces/AA/BB",
		"Team Room", "AAAA", "",
	}
	for _, in := range bad {
		if got, ok := ParseSpaceRef(in); ok {
			t.Errorf("ParseSpaceRef(%q) = %q, want not ok", in, got)
		}
	}
}

func TestResolveSpaceURL(t *testing.T) {
	c := newClient(t, newFakeAPI(t))
	s, err := c.ResolveSpace(context.Background(), "https://chat.google.com/app/chat/AAAAUKFhXM0")
	if err != nil || s.Name != "spaces/AAAAUKFhXM0" || s.DisplayName != "From URL" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestResolveSpaceBadURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
	}))
	defer srv.Close()
	c, err := New(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResolveSpace(context.Background(), "https://evil.example/room/AAA"); !errors.Is(err, ErrBadChatURL) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveSpaceBareID(t *testing.T) {
	c := newClient(t, newFakeAPI(t))
	ctx := context.Background()
	if s, err := c.ResolveSpace(ctx, "AAAAUKFhXM0"); err != nil || s.Name != "spaces/AAAAUKFhXM0" {
		t.Fatalf("bare id: %+v, %v", s, err)
	}
	// A display name that looks like an ID still wins.
	if s, err := c.ResolveSpace(ctx, "standup2026"); err != nil || s.Name != "spaces/EEE" {
		t.Fatalf("id-like display name: %+v, %v", s, err)
	}
	if _, err := c.ResolveSpace(ctx, "MISSING"); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("missing bare id: %v", err)
	}
}

func TestResolveSpaceBareIDNonNotFoundErrors(t *testing.T) {
	// Spaces the user can't see may answer 403 or 400 rather than 404; a
	// mistyped display name must still read as "space not found".
	for _, code := range []int{400, 403} {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/spaces", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"spaces":[{"name":"spaces/AAA","displayName":"Team","spaceType":"SPACE"}]}`))
		})
		mux.HandleFunc("/v1/spaces/engineering", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			w.Write([]byte(`{"error":{"code":` + fmt.Sprint(code) + `,"message":"nope"}}`))
		})
		srv := httptest.NewServer(mux)
		c, err := New(context.Background(), srv.Client(), srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ResolveSpace(context.Background(), "engineering"); !errors.Is(err, ErrSpaceNotFound) {
			t.Errorf("HTTP %d: err = %v, want ErrSpaceNotFound", code, err)
		}
		srv.Close()
	}
}
