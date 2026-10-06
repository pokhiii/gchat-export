package auth

import (
	"encoding/json"
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "gchat-export"
	keyringUser    = "default"
)

type keyringStore struct{}

// NewKeyringStore stores the credential in the OS keychain.
func NewKeyringStore() Store { return keyringStore{} }

func (keyringStore) Load() (*Credential, error) {
	s, err := keyring.Get(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoCredential
	}
	if err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, errors.New("stored credential is corrupt; run auth login")
	}
	return &c, nil
}

func (keyringStore) Save(c *Credential) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringUser, string(b))
}

func (keyringStore) Delete() error {
	err := keyring.Delete(keyringService, keyringUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
