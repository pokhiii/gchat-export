package auth

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/OWNER/gchat-export/internal/fsutil"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

func sampleCred() *Credential {
	return &Credential{
		Email:  "me@example.com",
		Scopes: []string{"a", "b"},
		Token: &oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
			Expiry:       time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		},
	}
}

func roundTrip(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.Load(); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("empty Load err = %v", err)
	}
	want := sampleCred()
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != want.Email || !reflect.DeepEqual(got.Scopes, want.Scopes) ||
		got.Token.AccessToken != "access" || got.Token.RefreshToken != "refresh" || !got.Token.Expiry.Equal(want.Token.Expiry) {
		t.Fatalf("got %+v", got)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("after Delete err = %v", err)
	}
}

func TestKeyringRoundTrip(t *testing.T) {
	keyring.MockInit()
	roundTrip(t, NewKeyringStore())
}

func TestFileStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	roundTrip(t, NewFileStore(dir))
}

func TestFileStorePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := NewFileStore(dir).Save(sampleCred()); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "credential.json"))
	di, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("file %o dir %o", fi.Mode().Perm(), di.Mode().Perm())
	}
}

func TestFileStoreRefusesLoosePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := filepath.Join(t.TempDir(), "cfg")
	s := NewFileStore(dir)
	if err := s.Save(sampleCred()); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(dir, "credential.json"), 0o644)
	if _, err := s.Load(); !errors.Is(err, fsutil.ErrInsecurePerms) {
		t.Fatalf("err = %v", err)
	}
}

func TestFileStoreDeleteMissingIsNoop(t *testing.T) {
	if err := NewFileStore(t.TempDir()).Delete(); err != nil {
		t.Fatal(err)
	}
}
