package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions/symlinks not applicable on windows")
	}
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestCreateNewPerms(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	f, err := CreateNew(root, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if m := mode(t, filepath.Join(root, "a.txt")); m != 0o600 {
		t.Fatalf("mode = %o, want 600", m)
	}
}

func TestCreateNewFailsIfExists(t *testing.T) {
	root := t.TempDir()
	f, err := CreateNew(root, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := CreateNew(root, "a.txt"); err == nil {
		t.Fatal("expected error for existing file")
	}
}

func TestMkdirPrivateTightensExisting(t *testing.T) {
	skipOnWindows(t)
	dir := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Fatalf("mode = %o, want 700", m)
	}
	nested := filepath.Join(dir, "x", "y")
	if err := MkdirPrivate(nested); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, nested); m != 0o700 {
		t.Fatalf("nested mode = %o, want 700", m)
	}
}

func TestCreateNewRefusesSymlink(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	if f, err := CreateNew(root, "a"); err == nil {
		f.Close()
		t.Fatal("expected error writing through symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "orig" {
		t.Fatalf("symlink target modified: %q", b)
	}
}

func TestCreateNewRefusesEscape(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"../x", "a/../../x", filepath.Join(string(filepath.Separator), "etc", "x")} {
		f, err := CreateNew(root, rel)
		if f != nil {
			f.Close()
		}
		if !errors.Is(err, ErrEscapesRoot) {
			t.Errorf("CreateNew(%q) err = %v, want ErrEscapesRoot", rel, err)
		}
	}
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	root := t.TempDir()
	if err := WriteFileAtomic(root, "f.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(root, "f.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "f.json")); string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, filepath.Join(root, "f.json")); m != 0o600 {
			t.Fatalf("mode = %o", m)
		}
	}
}

func TestWriteFileAtomicRefusesEscape(t *testing.T) {
	if err := WriteFileAtomic(t.TempDir(), "../x", nil); !errors.Is(err, ErrEscapesRoot) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveRegular(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f"), nil, 0o600)
	if err := RemoveRegular(root, "f"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "f")); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
	os.Mkdir(filepath.Join(root, "d"), 0o700)
	if err := RemoveRegular(root, "d"); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("dir: err = %v", err)
	}
}

func TestRemoveRegularRefusesSymlink(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "t")
	os.WriteFile(target, nil, 0o600)
	os.Symlink(target, filepath.Join(root, "l"))
	if err := RemoveRegular(root, "l"); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckPrivate(t *testing.T) {
	skipOnWindows(t)
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, nil, 0o644)
	os.Chmod(p, 0o644)
	if err := CheckPrivate(p); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("0644: err = %v", err)
	}
	os.Chmod(p, 0o600)
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("0600: err = %v", err)
	}
}

func TestContained(t *testing.T) {
	root := t.TempDir()
	if err := Contained(root, filepath.Join(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := Contained(root, root); err != nil {
		t.Fatal(err)
	}
	if err := Contained(root, filepath.Join(root, "..", "x")); !errors.Is(err, ErrEscapesRoot) {
		t.Fatalf("err = %v", err)
	}
	if err := Contained(root, root+"-sibling"); !errors.Is(err, ErrEscapesRoot) {
		t.Fatalf("sibling prefix: err = %v", err)
	}
}

// OpenWrite must not use O_APPEND: on Windows O_APPEND drops the write access
// that Truncate needs, and positioned writes must land where we Seek.
func TestOpenWriteIsPositional(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f"), []byte("abcdef"), 0o600)
	f, err := OpenWrite(root, "f")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(4); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	f.WriteString("X")
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(root, "f")); string(b) != "abXd" {
		t.Fatalf("got %q, want %q", b, "abXd")
	}
}

func TestOpenWriteRefusesSymlink(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "t")
	os.WriteFile(target, []byte("orig"), 0o600)
	os.Symlink(target, filepath.Join(root, "l"))
	if f, err := OpenWrite(root, "l"); err == nil {
		f.Close()
		t.Fatal("followed symlink")
	}
}
