// Package auth handles OAuth login and credential storage.
package auth

import (
	"errors"

	"golang.org/x/oauth2"
)

// ErrNoCredential means no account is logged in.
var ErrNoCredential = errors.New("not logged in")

// Credential is the single signed-in account's token and granted scopes.
type Credential struct {
	Email  string        `json:"email"`
	Scopes []string      `json:"scopes"`
	Token  *oauth2.Token `json:"token"`
	// ClientSecretPath is where the OAuth client JSON was read at login, so
	// later commands can refresh tokens without repeating --client-secret.
	ClientSecretPath string `json:"client_secret_path,omitempty"`
}

// Store persists one Credential.
type Store interface {
	Load() (*Credential, error)
	Save(*Credential) error
	Delete() error
}
