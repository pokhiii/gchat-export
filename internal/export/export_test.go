package export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pokhiii/gchat-export/internal/daterange"
	"github.com/pokhiii/gchat-export/internal/model"
	"github.com/pokhiii/gchat-export/internal/render"
)

const secretText = "TOP-SECRET-MESSAGE-TEXT"

type fakeSource struct {
	pages     [][]model.Message
	media     map[string]string
	failAt    int // page index whose request fails; -1 = never
	requests  []string
	afterLast func() // called once the last page has been delivered
}

var errBoom = errors.New("network down")

func (f *fakeSource) ListMembers(ctx context.Context, space string) (map[string]string, error) {
	return map[string]string{"users/1": "Alice", "users/2": "Bob"}, nil
}

func (f *fakeSource) ListMessages(ctx context.Context, space string, r daterange.Range, token string, members map[string]string,
	fn func([]model.Message, string) error) error {
	i := 0
	if token != "" {
		i = int(token[1] - '0')
	}
	for ; i < len(f.pages); i++ {
		f.requests = append(f.requests, token)
		if i == f.failAt {
			return errBoom
		}
		next := ""
		if i+1 < len(f.pages) {
			next = "p" + string(rune('0'+i+1))
		}
		if err := fn(f.pages[i], next); err != nil {
			return err
		}
		token = next
	}
	if f.afterLast != nil {
		f.afterLast()
	}
	return nil
}

func (f *fakeSource) DownloadMedia(ctx context.Context, res string) (io.ReadCloser, error) {
	body, ok := f.media[res]
	if !ok {
		return nil, errors.New("media gone")
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func at(h int) time.Time { return time.Date(2026, 9, 3, h, 0, 0, 0, time.UTC) }

func m(id string, h int, atts ...model.Attachment) model.Message {
	return model.Message{ID: "spaces/AAA/messages/" + id, ThreadID: "spaces/AAA/threads/" + id, Time: at(h),
		Sender: model.User{ID: "users/1", DisplayName: "Alice"}, Text: secretText + " " + id, Attachments: atts}
}

func upl(name, res string) model.Attachment {
	return model.Attachment{Name: name, Source: model.SourceUploaded, ResourceName: res}
}

func newSource() *fakeSource {
	return &fakeSource{
		pages: [][]model.Message{
			{m("m1", 1, upl("a.txt", "r1")), m("m2", 2)},
			{m("m3", 3, upl("b.txt", "r3"))},
		},
		media:  map[string]string{"r1": "one", "r3": "three"},
		failAt: -1,
	}
}

func opts(t *testing.T) Options {
	t.Helper()
	return Options{
		Space:    model.Space{Name: "spaces/AAA", DisplayName: "Team Room", Type: model.SpaceTypeSpace},
		Range:    daterange.Range{From: at(0), To: at(23), Loc: time.UTC, DateOnly: true},
		Markdown: true, JSONL: true, Attachments: true, MaxAttachmentSize: 1 << 20,
		OutRoot: filepath.Join(t.TempDir(), "export"),
		Scopes:  []string{"s1"}, Version: "test",
		Now: func() time.Time { return time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) },
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func readMessages(t *testing.T, dir string) []model.Message {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "messages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	msgs, err := render.ReadJSONL(f)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

type manifestView struct {
	Messages    int `json:"messages"`
	Attachments []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"attachments"`
	Errors []ItemError `json:"errors"`
	Space  struct {
		Name string `json:"name"`
	} `json:"space"`
	Range struct {
		TZ string `json:"tz"`
	} `json:"range"`
	Scopes      []string `json:"scopes"`
	ToolVersion string   `json:"tool_version"`
}

func readManifest(t *testing.T, dir string) manifestView {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mv manifestView
	if err := json.Unmarshal(b, &mv); err != nil {
		t.Fatal(err)
	}
	return mv
}

func TestRunHappyPath(t *testing.T) {
	o := opts(t)
	res, err := Run(context.Background(), newSource(), o, quiet())
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(o.OutRoot, "team-room-AAA", "2026-09-03_2026-09-03")
	if res.Dir != wantDir || res.Messages != 3 || res.Attachments != 2 || res.Partial() {
		t.Fatalf("result %+v", res)
	}
	for _, name := range []string{"messages.jsonl", "transcript.md", "manifest.json", "attachments/m1_0_a.txt", "attachments/m3_0_b.txt"} {
		fi, err := os.Stat(filepath.Join(res.Dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %o", name, fi.Mode().Perm())
		}
	}
	if runtime.GOOS != "windows" {
		for _, d := range []string{res.Dir, filepath.Join(res.Dir, "attachments")} {
			if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o700 {
				t.Errorf("%s mode %o", d, fi.Mode().Perm())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(res.Dir, ".state.json")); !os.IsNotExist(err) {
		t.Fatal(".state.json left behind")
	}
	mv := readManifest(t, res.Dir)
	if mv.Messages != 3 || len(mv.Attachments) != 2 || mv.Space.Name != "spaces/AAA" || mv.Range.TZ != "UTC" ||
		len(mv.Scopes) != 1 || mv.ToolVersion != "test" || mv.Errors == nil {
		t.Fatalf("manifest %+v", mv)
	}
	if md, _ := os.ReadFile(filepath.Join(res.Dir, "transcript.md")); !strings.Contains(string(md), "# Team Room") {
		t.Fatalf("transcript:\n%s", md)
	}
}

func TestRunRefusesExisting(t *testing.T) {
	o := opts(t)
	if _, err := Run(context.Background(), newSource(), o, quiet()); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), newSource(), o, quiet()); !errors.Is(err, ErrOutputExists) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunForceReplaces(t *testing.T) {
	o := opts(t)
	res, _ := Run(context.Background(), newSource(), o, quiet())
	os.WriteFile(filepath.Join(res.Dir, "stray.txt"), []byte("x"), 0o600)
	o.Force = true
	if _, err := Run(context.Background(), newSource(), o, quiet()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "stray.txt")); !os.IsNotExist(err) {
		t.Fatal("stray file survived --force")
	}
}

func TestRunResume(t *testing.T) {
	o := opts(t)
	src := newSource()
	src.failAt = 1
	if _, err := Run(context.Background(), src, o, quiet()); !errors.Is(err, errBoom) {
		t.Fatalf("first run err = %v", err)
	}
	dir := filepath.Join(o.OutRoot, "team-room-AAA", "2026-09-03_2026-09-03")
	// Simulate a crash mid-page-2: partial JSONL bytes and a partial attachment.
	f, _ := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"id":"spaces/AAA/messages/m3","thr`)
	f.Close()
	os.WriteFile(filepath.Join(dir, "attachments", "m3_0_b.txt"), []byte("par"), 0o600)

	src.failAt = -1
	src.requests = nil
	o.Resume = true
	res, err := Run(context.Background(), src, o, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if len(src.requests) != 1 || src.requests[0] != "p1" {
		t.Fatalf("resume requests %v, want [p1]", src.requests)
	}
	msgs := readMessages(t, dir)
	seen := map[string]bool{}
	for _, mm := range msgs {
		if seen[mm.ID] {
			t.Fatalf("duplicate %s", mm.ID)
		}
		seen[mm.ID] = true
	}
	if len(msgs) != 3 || res.Messages != 3 || res.Attachments != 2 {
		t.Fatalf("msgs %d result %+v", len(msgs), res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "attachments", "m3_0_b.txt")); string(b) != "three" {
		t.Fatalf("attachment not replaced: %q", b)
	}
	if mv := readManifest(t, dir); mv.Messages != 3 || len(mv.Attachments) != 2 {
		t.Fatalf("manifest %+v", mv)
	}
}

func TestRunResumeAfterLastPageBeforeFinish(t *testing.T) {
	o := opts(t)
	src := newSource()
	dir := filepath.Join(o.OutRoot, "team-room-AAA", "2026-09-03_2026-09-03")
	// Make the finish step fail: a non-empty directory where transcript.md goes.
	src.afterLast = func() { os.MkdirAll(filepath.Join(dir, "transcript.md", "x"), 0o700) }
	if _, err := Run(context.Background(), src, o, quiet()); err == nil {
		t.Fatal("expected finish failure")
	}
	os.RemoveAll(filepath.Join(dir, "transcript.md"))
	src.afterLast = nil
	src.requests = nil
	o.Resume = true
	if _, err := Run(context.Background(), src, o, quiet()); err != nil {
		t.Fatal(err)
	}
	if len(src.requests) != 0 {
		t.Fatalf("re-fetched pages after completed listing: %v", src.requests)
	}
	if n := len(readMessages(t, dir)); n != 3 {
		t.Fatalf("%d messages", n)
	}
}

func TestRunResumeWithoutState(t *testing.T) {
	o := opts(t)
	Run(context.Background(), newSource(), o, quiet())
	o.Resume = true
	if _, err := Run(context.Background(), newSource(), o, quiet()); !errors.Is(err, ErrNoState) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunResumeMismatch(t *testing.T) {
	o := opts(t)
	src := newSource()
	src.failAt = 1
	Run(context.Background(), src, o, quiet())
	// Same directory name, different exact range.
	o.Range.To = o.Range.To.Add(time.Minute)
	o.Resume = true
	if _, err := Run(context.Background(), newSource(), o, quiet()); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunPartialAttachmentFailure(t *testing.T) {
	src := newSource()
	delete(src.media, "r3")
	res, err := Run(context.Background(), src, opts(t), quiet())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Partial() || len(res.Errors) != 1 || res.Errors[0].MessageID != "spaces/AAA/messages/m3" || res.Errors[0].Attachment != "b.txt" {
		t.Fatalf("result %+v", res)
	}
	if mv := readManifest(t, res.Dir); len(mv.Errors) != 1 {
		t.Fatalf("manifest errors %+v", mv.Errors)
	}
}

func TestRunJSONLOnlyAndMDOnly(t *testing.T) {
	o := opts(t)
	o.Markdown = false
	res, err := Run(context.Background(), newSource(), o, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "transcript.md")); !os.IsNotExist(err) {
		t.Fatal("transcript.md written without md format")
	}
	o = opts(t)
	o.JSONL = false
	res, err = Run(context.Background(), newSource(), o, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "messages.jsonl")); !os.IsNotExist(err) {
		t.Fatal("messages.jsonl kept without jsonl format")
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "transcript.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRunWithoutAttachments(t *testing.T) {
	o := opts(t)
	o.Attachments = false
	res, err := Run(context.Background(), newSource(), o, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if res.Attachments != 0 {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "attachments")); !os.IsNotExist(err) {
		t.Fatal("attachments dir created")
	}
}

func TestSlugAndTitleForDM(t *testing.T) {
	dm := model.Space{Name: "spaces/AAAA", Type: model.SpaceTypeDM}
	if got := Slug(dm); got != "dm-AAAA" {
		t.Fatalf("Slug = %q", got)
	}
	if got := Slug(model.Space{Name: "spaces/G1", Type: model.SpaceTypeGroup}); got != "group-G1" {
		t.Fatalf("group Slug = %q", got)
	}
	if got := Slug(model.Space{Name: "spaces/S", DisplayName: "../Team Room/x", Type: model.SpaceTypeSpace}); strings.ContainsAny(got, `/\ `) || got == "" {
		t.Fatalf("named Slug = %q", got)
	}
	if got := Title(dm, map[string]string{"users/2": "Bob", "users/1": "Alice"}); got != "DM with Alice, Bob" {
		t.Fatalf("Title = %q", got)
	}
	if got := Title(model.Space{Name: "spaces/S", DisplayName: "Team"}, nil); got != "Team" {
		t.Fatalf("named Title = %q", got)
	}
}

func TestLogsContainNoMessageText(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := Run(context.Background(), newSource(), opts(t), log); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected some debug logging")
	}
	if strings.Contains(buf.String(), secretText) {
		t.Fatalf("message text logged:\n%s", buf.String())
	}
}

func TestSlugDistinctForSameDisplayName(t *testing.T) {
	a := Slug(model.Space{Name: "spaces/AAA", DisplayName: "Standup", Type: model.SpaceTypeSpace})
	b := Slug(model.Space{Name: "spaces/BBB", DisplayName: "Standup", Type: model.SpaceTypeSpace})
	if a == b {
		t.Fatalf("same slug %q for different spaces", a)
	}
	if a != "standup-AAA" {
		t.Fatalf("slug = %q, want standup-AAA", a)
	}
}

func TestSlugKeepsIDForLongNames(t *testing.T) {
	got := Slug(model.Space{Name: "spaces/AAA", DisplayName: strings.Repeat("é", 300)})
	if !strings.HasSuffix(got, "-AAA") || len(got) > 200 {
		t.Fatalf("slug %q (len %d)", got, len(got))
	}
}

func TestRunResumeWithoutDirIsError(t *testing.T) {
	o := opts(t)
	o.Resume = true
	if _, err := Run(context.Background(), newSource(), o, quiet()); !errors.Is(err, ErrNoState) {
		t.Fatalf("err = %v", err)
	}
}

// An export started without --until has To = start time; resuming must find
// that exact range again rather than computing a new "now".
func TestFindResumeTo(t *testing.T) {
	o := opts(t)
	o.Range.DateOnly = false
	o.Range.To = time.Date(2026, 10, 6, 10, 15, 3, 0, time.UTC)
	src := newSource()
	src.failAt = 1
	if _, err := Run(context.Background(), src, o, quiet()); !errors.Is(err, errBoom) {
		t.Fatalf("first run err = %v", err)
	}
	to, err := FindResumeTo(o.OutRoot, o.Space, o.Range.From)
	if err != nil || !to.Equal(o.Range.To) {
		t.Fatalf("FindResumeTo = %v, %v; want %v", to, err, o.Range.To)
	}
	if _, err := FindResumeTo(o.OutRoot, o.Space, o.Range.From.Add(time.Hour)); !errors.Is(err, ErrNoState) {
		t.Fatalf("different since: err = %v", err)
	}
	other := model.Space{Name: "spaces/ZZZ", DisplayName: "Team Room"}
	if _, err := FindResumeTo(o.OutRoot, other, o.Range.From); !errors.Is(err, ErrNoState) {
		t.Fatalf("other space: err = %v", err)
	}
}
