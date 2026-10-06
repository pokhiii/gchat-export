// Package attach downloads uploaded Chat attachments into the export
// directory, treating names and contents as untrusted.
package attach

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"

	"github.com/OWNER/gchat-export/internal/fsutil"
	"github.com/OWNER/gchat-export/internal/model"
)

const dirName = "attachments"

// ErrTooLarge is returned when an attachment exceeds Downloader.MaxSize.
var ErrTooLarge = errors.New("attachment exceeds size limit")

// FetchFunc streams the bytes of an uploaded attachment.
type FetchFunc func(ctx context.Context, resourceName string) (io.ReadCloser, error)

// Downloader saves uploaded attachments under Root/attachments.
type Downloader struct {
	Fetch   FetchFunc
	Root    string // export directory
	MaxSize int64
}

// DownloadAll downloads m's uploaded attachments and returns a copy of m with
// Path, SHA256 and Size filled in. Drive attachments are left as links. Each
// failure is reported separately; the others still download.
func (d *Downloader) DownloadAll(ctx context.Context, m model.Message) (model.Message, []error) {
	out := m
	out.Attachments = append([]model.Attachment(nil), m.Attachments...)
	var errs []error
	for i, a := range out.Attachments {
		if a.Source != model.SourceUploaded {
			continue
		}
		rel := path.Join(dirName, fmt.Sprintf("%s_%d_%s", fsutil.SanitizeName(m.ShortID()), i, fsutil.SanitizeName(a.Name)))
		sum, size, err := d.download(ctx, a.ResourceName, rel)
		if err != nil {
			errs = append(errs, fmt.Errorf("attachment %d (%s): %w", i, rel, err))
			continue
		}
		out.Attachments[i].Path, out.Attachments[i].SHA256, out.Attachments[i].Size = rel, sum, size
	}
	return out, errs
}

func (d *Downloader) download(ctx context.Context, resource, rel string) (string, int64, error) {
	if resource == "" {
		return "", 0, errors.New("missing resource name")
	}
	if err := fsutil.MkdirPrivate(filepath.Join(d.Root, dirName)); err != nil {
		return "", 0, err
	}
	osRel := filepath.FromSlash(rel)
	// A leftover file from an interrupted run is replaced; anything that is
	// not a regular file (e.g. a symlink) is refused.
	if err := fsutil.RemoveRegular(d.Root, osRel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", 0, err
	}

	body, err := d.Fetch(ctx, resource)
	if err != nil {
		return "", 0, err
	}
	defer body.Close()

	f, err := fsutil.CreateNew(d.Root, osRel)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, d.MaxSize+1))
	if copyErr == nil && n > d.MaxSize {
		copyErr = ErrTooLarge
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	if closeErr := f.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = fsutil.RemoveRegular(d.Root, osRel) // best-effort cleanup of a partial file
		return "", 0, copyErr
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
