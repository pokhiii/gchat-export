package auth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pokhiii/gchat-export/internal/fsutil"
)

const credentialFile = "credential.json" // #nosec G101 -- a file name, not a credential

type fileStore struct{ dir string }

// NewFileStore stores the credential in dir/credential.json (0600 in a 0700
// directory). It is the opt-in fallback for systems without a keychain.
func NewFileStore(dir string) Store { return fileStore{dir: dir} }

func (s fileStore) Load() (*Credential, error) {
	path := filepath.Join(s.dir, credentialFile)
	if err := fsutil.CheckPrivate(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoCredential
		}
		return nil, err
	}
	b, err := os.ReadFile(path) // #nosec G304 -- path is the tool's own config dir
	if err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, errors.New("stored credential is corrupt; run auth login")
	}
	return &c, nil
}

func (s fileStore) Save(c *Credential) error {
	if err := fsutil.MkdirPrivate(s.dir); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.dir, credentialFile, b)
}

func (s fileStore) Delete() error {
	err := fsutil.RemoveRegular(s.dir, credentialFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
