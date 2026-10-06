// Package fsutil provides file-system helpers that create private files and
// directories, refuse to write through symlinks, and keep paths inside a root.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	// ErrInsecurePerms is returned when a file is readable or writable by
	// group or others.
	ErrInsecurePerms = errors.New("file permissions are too open")
	// ErrEscapesRoot is returned when a path resolves outside its root.
	ErrEscapesRoot = errors.New("path escapes output directory")
	// ErrNotRegular is returned when a path is not a regular file.
	ErrNotRegular = errors.New("not a regular file")
)

// MkdirPrivate creates path (and missing parents) with mode 0700 and
// tightens path itself to 0700 if it already exists.
func MkdirPrivate(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700) // #nosec G302 -- directories need the execute bit; 0700 is owner-only
}

// CheckPrivate reports ErrInsecurePerms if path is accessible by group or
// others. It is a no-op on Windows.
func CheckPrivate(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s has mode %o: %w", path, fi.Mode().Perm(), ErrInsecurePerms)
	}
	return nil
}

// Contained returns ErrEscapesRoot unless path is root or lies inside it.
func Contained(root, path string) error {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrEscapesRoot
	}
	return nil
}

func resolve(root, rel string) (string, error) {
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", ErrEscapesRoot
	}
	full := filepath.Join(root, rel)
	if err := Contained(root, full); err != nil {
		return "", err
	}
	return full, nil
}

// CreateNew creates root/rel exclusively with mode 0600, refusing symlinks
// and paths outside root.
func CreateNew(root, rel string) (*os.File, error) {
	full, err := resolve(root, rel)
	if err != nil {
		return nil, err
	}
	return openNoFollow(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}

// OpenAppend opens root/rel for appending, creating it with mode 0600 if
// needed, refusing symlinks and paths outside root.
func OpenAppend(root, rel string) (*os.File, error) {
	full, err := resolve(root, rel)
	if err != nil {
		return nil, err
	}
	return openNoFollow(full, os.O_WRONLY|os.O_APPEND|os.O_CREATE)
}

// WriteFileAtomic replaces root/rel with data via a 0600 temp file in the same
// directory, fsync, and rename.
func WriteFileAtomic(root, rel string, data []byte) error {
	full, err := resolve(root, rel)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), "."+filepath.Base(full)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func(e error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return e
	}
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		return cleanup(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// RemoveRegular removes root/rel only if it is a regular file (not a
// directory or symlink).
func RemoveRegular(root, rel string) error {
	full, err := resolve(root, rel)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", rel, ErrNotRegular)
	}
	return os.Remove(full)
}
