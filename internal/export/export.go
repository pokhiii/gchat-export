// Package export orchestrates an export: output directory rules, page-by-page
// JSONL writing with resumable state, attachments, Markdown and manifest.
package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/OWNER/gchat-export/internal/attach"
	"github.com/OWNER/gchat-export/internal/daterange"
	"github.com/OWNER/gchat-export/internal/fsutil"
	"github.com/OWNER/gchat-export/internal/model"
	"github.com/OWNER/gchat-export/internal/render"
)

const (
	jsonlFile      = "messages.jsonl"
	transcriptFile = "transcript.md"
)

var (
	ErrOutputExists  = errors.New("output directory already exists")
	ErrStateMismatch = errors.New("resume state is for a different space or range")
	ErrNoState       = errors.New("nothing to resume in output directory")
)

// Source is the subset of the Chat client an export needs.
type Source interface {
	ListMembers(ctx context.Context, space string) (map[string]string, error)
	ListMessages(ctx context.Context, space string, r daterange.Range, pageToken string, members map[string]string,
		fn func([]model.Message, string) error) error
	DownloadMedia(ctx context.Context, resourceName string) (io.ReadCloser, error)
}

type Options struct {
	Space             model.Space
	Range             daterange.Range
	Markdown, JSONL   bool
	Attachments       bool
	MaxAttachmentSize int64
	OutRoot           string
	Resume, Force     bool
	Scopes            []string
	Version           string
	Now               func() time.Time
}

// ItemError records a non-fatal failure for one message or attachment.
type ItemError struct {
	MessageID  string `json:"message_id"`
	Attachment string `json:"attachment,omitempty"`
	Error      string `json:"error"`
}

type Result struct {
	Dir         string
	Messages    int
	Attachments int
	Errors      []ItemError
}

// Partial reports whether some items failed.
func (r Result) Partial() bool { return len(r.Errors) > 0 }

// Run performs the export described by opt.
func Run(ctx context.Context, src Source, opt Options, log *slog.Logger) (Result, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	members, err := src.ListMembers(ctx, opt.Space.Name)
	if err != nil {
		return Result{}, fmt.Errorf("listing members: %w", err)
	}

	dir := filepath.Join(opt.OutRoot, Slug(opt.Space), opt.Range.DirName())
	if err := fsutil.Contained(opt.OutRoot, dir); err != nil {
		return Result{}, err
	}
	st, err := prepareDir(dir, opt)
	if err != nil {
		return Result{}, err
	}
	res := Result{Dir: dir}

	if !st.Complete {
		if err := fetch(ctx, src, opt, dir, members, st, log); err != nil {
			return res, err
		}
	}
	if err := finish(dir, opt, members, st); err != nil {
		return res, err
	}
	log.Debug("export finished", "messages", st.Messages, "attachments", len(st.Attachments), "errors", len(st.Errors))
	res.Messages, res.Attachments, res.Errors = st.Messages, len(st.Attachments), st.Errors
	return res, nil
}

// prepareDir applies the existing-output rules and returns the state to
// continue from.
func prepareDir(dir string, opt Options) (*state, error) {
	entries, err := os.ReadDir(dir)
	exists := err == nil && len(entries) > 0
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	fresh := &state{Space: opt.Space.Name, From: opt.Range.From, To: opt.Range.To}

	switch {
	case exists && opt.Resume:
		st, err := loadState(dir)
		if err != nil {
			return nil, err
		}
		if st.Space != opt.Space.Name || !st.From.Equal(opt.Range.From) || !st.To.Equal(opt.Range.To) {
			return nil, ErrStateMismatch
		}
		return st, nil
	case exists && opt.Force:
		if err := os.RemoveAll(dir); err != nil {
			return nil, err
		}
	case exists:
		return nil, fmt.Errorf("%s: %w", dir, ErrOutputExists)
	case opt.Resume:
		return nil, fmt.Errorf("%s: %w", dir, ErrNoState)
	}
	if err := fsutil.MkdirPrivate(dir); err != nil {
		return nil, err
	}
	return fresh, nil
}

func fetch(ctx context.Context, src Source, opt Options, dir string, members map[string]string, st *state, log *slog.Logger) error {
	f, err := fsutil.OpenWrite(dir, jsonlFile)
	if err != nil {
		return err
	}
	defer f.Close()
	// Drop anything written after the last checkpoint (a half-written page).
	if err := f.Truncate(st.JSONLBytes); err != nil {
		return err
	}
	if _, err := f.Seek(st.JSONLBytes, io.SeekStart); err != nil {
		return err
	}

	dl := &attach.Downloader{Fetch: src.DownloadMedia, Root: dir, MaxSize: opt.MaxAttachmentSize}
	page := 0
	return src.ListMessages(ctx, opt.Space.Name, opt.Range, st.PageToken, members, func(msgs []model.Message, next string) error {
		page++
		var buf bytes.Buffer
		var newAtts []manifestAttachment
		var newErrs []ItemError
		for _, m := range msgs {
			if opt.Attachments {
				var errs []error
				m, errs = dl.DownloadAll(ctx, m)
				newErrs = append(newErrs, attachmentErrors(m, errs)...)
				for _, a := range m.Attachments {
					if a.Path != "" {
						newAtts = append(newAtts, manifestAttachment{Path: a.Path, SHA256: a.SHA256, Size: a.Size})
					}
				}
			}
			if err := render.WriteJSONL(&buf, m); err != nil {
				return err
			}
		}
		if _, err := f.Write(buf.Bytes()); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		offset, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		st.PageToken, st.Complete, st.JSONLBytes = next, next == "", offset
		st.Messages += len(msgs)
		st.Attachments = append(st.Attachments, newAtts...)
		st.Errors = append(st.Errors, newErrs...)
		if err := st.save(dir); err != nil {
			return err
		}
		log.Debug("page written", "page", page, "messages", len(msgs), "total", st.Messages)
		return nil
	})
}

// attachmentErrors pairs DownloadAll's errors, which are in attachment order,
// with the uploaded attachments that were left without a path.
func attachmentErrors(m model.Message, errs []error) []ItemError {
	var out []ItemError
	i := 0
	for _, a := range m.Attachments {
		if i == len(errs) {
			break
		}
		if a.Source == model.SourceUploaded && a.Path == "" {
			out = append(out, ItemError{MessageID: m.ID, Attachment: a.Name, Error: errs[i].Error()})
			i++
		}
	}
	return out
}

func finish(dir string, opt Options, members map[string]string, st *state) error {
	if opt.Markdown {
		f, err := os.Open(filepath.Join(dir, jsonlFile)) // #nosec G304 -- inside the export dir
		if err != nil {
			return err
		}
		msgs, err := render.ReadJSONL(f)
		_ = f.Close() // read-only
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		h := render.Header{Space: opt.Space, Title: Title(opt.Space, members), Range: opt.Range, GeneratedAt: opt.Now()}
		if err := render.RenderMarkdown(&buf, h, msgs); err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(dir, transcriptFile, buf.Bytes()); err != nil {
			return err
		}
	}
	if !opt.JSONL {
		if err := fsutil.RemoveRegular(dir, jsonlFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := writeManifest(dir, opt, st, opt.Now()); err != nil {
		return err
	}
	return fsutil.RemoveRegular(dir, stateFile)
}

// FindResumeTo returns the end of the range of an interrupted export of space
// s that started at from. It lets --resume work when --until was omitted and
// the original end was "now" at the time of the first run.
func FindResumeTo(outRoot string, s model.Space, from time.Time) (time.Time, error) {
	matches, err := filepath.Glob(filepath.Join(outRoot, Slug(s), "*", stateFile))
	if err != nil {
		return time.Time{}, err
	}
	var found []time.Time
	for _, m := range matches {
		st, err := loadState(filepath.Dir(m))
		if err != nil {
			continue
		}
		if st.Space == s.Name && st.From.Equal(from) {
			found = append(found, st.To)
		}
	}
	switch len(found) {
	case 0:
		return time.Time{}, ErrNoState
	case 1:
		return found[0], nil
	}
	return time.Time{}, fmt.Errorf("%d interrupted exports start at %s; pass --until to choose one", len(found), from.Format(time.RFC3339))
}

// Slug is the directory name for a space. It always ends in the space ID so
// two spaces with the same display name never share a directory.
func Slug(s model.Space) string {
	id := fsutil.SanitizeName(s.ID())
	if s.DisplayName != "" {
		slug := strings.ToLower(strings.ReplaceAll(fsutil.SanitizeName(s.DisplayName), " ", "-"))
		if slug != "" && slug != "file" {
			if len(slug) > 150 {
				slug = strings.ToValidUTF8(slug[:150], "")
			}
			return slug + "-" + id
		}
	}
	prefix := "space"
	switch s.Type {
	case model.SpaceTypeDM:
		prefix = "dm"
	case model.SpaceTypeGroup:
		prefix = "group"
	}
	return prefix + "-" + id
}

// Title is a human heading for a space; unnamed spaces list member names.
func Title(s model.Space, members map[string]string) string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	names := make([]string, 0, len(members))
	for _, n := range members {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], "…")
	}
	label := "Space"
	switch s.Type {
	case model.SpaceTypeDM:
		label = "DM"
	case model.SpaceTypeGroup:
		label = "Group chat"
	}
	if len(names) == 0 {
		return label + " " + s.ID()
	}
	return label + " with " + strings.Join(names, ", ")
}
