package attach

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pokhiii/gchat-export/internal/model"
)

type fakeFetch struct {
	bodies map[string]string
	errs   map[string]error
	calls  []string
}

func (f *fakeFetch) fetch(ctx context.Context, res string) (io.ReadCloser, error) {
	f.calls = append(f.calls, res)
	if err := f.errs[res]; err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(f.bodies[res])), nil
}

func msg(atts ...model.Attachment) model.Message {
	return model.Message{ID: "spaces/AAA/messages/m1", Attachments: atts}
}

func up(name, res string) model.Attachment {
	return model.Attachment{Name: name, Source: model.SourceUploaded, ResourceName: res}
}

func newDL(t *testing.T, f *fakeFetch, max int64) *Downloader {
	t.Helper()
	return &Downloader{Fetch: f.fetch, Root: t.TempDir(), MaxSize: max}
}

func TestDownloadWritesFileAndHash(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r1": "hello"}}
	d := newDL(t, f, 100)
	out, errs := d.DownloadAll(context.Background(), msg(up("report.pdf", "r1")))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	a := out.Attachments[0]
	sum := sha256.Sum256([]byte("hello"))
	if a.Path != "attachments/m1_0_report.pdf" || a.SHA256 != hex.EncodeToString(sum[:]) || a.Size != 5 {
		t.Fatalf("attachment %+v", a)
	}
	full := filepath.Join(d.Root, filepath.FromSlash(a.Path))
	if b, _ := os.ReadFile(full); string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(full)
		di, _ := os.Stat(filepath.Dir(full))
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
			t.Fatalf("file %o dir %o", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
}

func TestDownloadTooLarge(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r1": "12345678901"}}
	d := newDL(t, f, 10)
	out, errs := d.DownloadAll(context.Background(), msg(up("big.bin", "r1")))
	if len(errs) != 1 || !errors.Is(errs[0], ErrTooLarge) {
		t.Fatalf("errs %v", errs)
	}
	if out.Attachments[0].Path != "" {
		t.Fatal("path set for oversized file")
	}
	entries, _ := os.ReadDir(filepath.Join(d.Root, "attachments"))
	if len(entries) != 0 {
		t.Fatalf("leftover files %v", entries)
	}
}

func TestDownloadExactlyMaxSize(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r1": "1234567890"}}
	if _, errs := newDL(t, f, 10).DownloadAll(context.Background(), msg(up("ok.bin", "r1"))); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestDownloadCollisionsAndEmptyNames(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"}}
	d := newDL(t, f, 100)
	out, errs := d.DownloadAll(context.Background(), msg(up("image.png", "a"), up("image.png", "b"), up("", "c"), up("...", "d")))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	seen := map[string]bool{}
	for _, a := range out.Attachments {
		if a.Path == "" || seen[a.Path] {
			t.Fatalf("bad/duplicate path %q", a.Path)
		}
		seen[a.Path] = true
		if _, err := os.Stat(filepath.Join(d.Root, filepath.FromSlash(a.Path))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadTraversalName(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r": "x"}}
	d := newDL(t, f, 100)
	out, errs := d.DownloadAll(context.Background(), msg(up("../../x", "r")))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !strings.HasPrefix(out.Attachments[0].Path, "attachments/") || strings.Contains(out.Attachments[0].Path, "..") {
		t.Fatalf("path %q", out.Attachments[0].Path)
	}
}

func TestDownloadReplacesExistingRegular(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r": "new"}}
	d := newDL(t, f, 100)
	os.MkdirAll(filepath.Join(d.Root, "attachments"), 0o700)
	os.WriteFile(filepath.Join(d.Root, "attachments", "m1_0_a.txt"), []byte("old partial"), 0o600)
	if _, errs := d.DownloadAll(context.Background(), msg(up("a.txt", "r"))); len(errs) != 0 {
		t.Fatal(errs)
	}
	if b, _ := os.ReadFile(filepath.Join(d.Root, "attachments", "m1_0_a.txt")); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
}

func TestDownloadRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	f := &fakeFetch{bodies: map[string]string{"r": "evil"}}
	d := newDL(t, f, 100)
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte("orig"), 0o600)
	os.MkdirAll(filepath.Join(d.Root, "attachments"), 0o700)
	os.Symlink(target, filepath.Join(d.Root, "attachments", "m1_0_a.txt"))
	if _, errs := d.DownloadAll(context.Background(), msg(up("a.txt", "r"))); len(errs) != 1 {
		t.Fatalf("errs %v", errs)
	}
	if b, _ := os.ReadFile(target); string(b) != "orig" {
		t.Fatalf("symlink target modified: %q", b)
	}
}

func TestDriveAttachmentSkipped(t *testing.T) {
	f := &fakeFetch{}
	drive := model.Attachment{Name: "Doc", Source: model.SourceDrive, DriveURL: "https://drive.google.com/open?id=x"}
	out, errs := newDL(t, f, 100).DownloadAll(context.Background(), msg(drive))
	if len(errs) != 0 || len(f.calls) != 0 || out.Attachments[0] != drive {
		t.Fatalf("errs %v calls %v out %+v", errs, f.calls, out.Attachments[0])
	}
}

func TestPartialFailureContinues(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"ok": "x"}, errs: map[string]error{"bad": errors.New("boom")}}
	out, errs := newDL(t, f, 100).DownloadAll(context.Background(), msg(up("a", "bad"), up("b", "ok")))
	if len(errs) != 1 || out.Attachments[0].Path != "" || out.Attachments[1].Path == "" {
		t.Fatalf("errs %v out %+v", errs, out.Attachments)
	}
}

func TestDownloadDoesNotMutateInput(t *testing.T) {
	f := &fakeFetch{bodies: map[string]string{"r": "x"}}
	in := msg(up("a", "r"))
	newDL(t, f, 100).DownloadAll(context.Background(), in)
	if in.Attachments[0].Path != "" {
		t.Fatal("input message mutated")
	}
}
