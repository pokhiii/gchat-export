package chat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pokhiii/gchat-export/internal/daterange"
	"github.com/pokhiii/gchat-export/internal/model"
)

const page1 = `{"messages":[
 {"name":"spaces/AAA/messages/m1","thread":{"name":"spaces/AAA/threads/t1"},
  "createTime":"2026-09-03T09:02:11.123Z","lastUpdateTime":"2026-09-03T09:05:00Z",
  "sender":{"name":"users/1","displayName":"Jane Doe"},"text":"hello",
  "emojiReactionSummaries":[{"emoji":{"unicode":"👍"},"reactionCount":3},{"emoji":{"customEmoji":{"uid":"abc","emojiName":":party:"}},"reactionCount":1}],
  "attachment":[
   {"contentName":"report.pdf","contentType":"application/pdf","source":"UPLOADED_CONTENT","attachmentDataRef":{"resourceName":"res-1"}},
   {"contentName":"Plan","contentType":"application/vnd.google-apps.document","source":"DRIVE_FILE","driveDataRef":{"driveFileId":"drv1"}}]},
 {"name":"spaces/AAA/messages/m2","thread":{"name":"spaces/AAA/threads/t1"},"threadReply":true,
  "createTime":"2026-09-03T10:00:00Z","lastUpdateTime":"2026-09-03T10:00:00Z",
  "sender":{"name":"users/2"},"text":"reply",
  "quotedMessageMetadata":{"name":"spaces/AAA/messages/m1"}}],
 "nextPageToken":"p2"}`

const page2 = `{"messages":[
 {"name":"spaces/AAA/messages/m3","thread":{"name":"spaces/AAA/threads/t3"},
  "createTime":"2026-09-04T10:00:00Z","sender":{"name":"users/3"},"text":"x"}]}`

type fakeAPI struct {
	*httptest.Server
	queries []url.Values
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/spaces/AAA/messages", func(w http.ResponseWriter, r *http.Request) {
		f.queries = append(f.queries, r.URL.Query())
		if r.URL.Query().Get("pageToken") == "p2" {
			io.WriteString(w, page2)
			return
		}
		io.WriteString(w, page1)
	})
	mux.HandleFunc("/v1/spaces/AAA/members", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"memberships":[{"name":"spaces/AAA/members/2","member":{"name":"users/2","displayName":"Bob"}}]}`)
	})
	mux.HandleFunc("/v1/spaces/AAA", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name":"spaces/AAA","displayName":"Team","spaceType":"SPACE"}`)
	})
	mux.HandleFunc("/v1/spaces/AAAAUKFhXM0", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name":"spaces/AAAAUKFhXM0","displayName":"From URL","spaceType":"SPACE"}`)
	})
	mux.HandleFunc("/v1/spaces/MISSING", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"code":404,"message":"not found","status":"NOT_FOUND"}}`)
	})
	mux.HandleFunc("/v1/spaces", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			io.WriteString(w, `{"spaces":[{"name":"spaces/AAA","displayName":"Team","spaceType":"SPACE"},{"name":"spaces/BBB","displayName":"Dup","spaceType":"SPACE"}],"nextPageToken":"s2"}`)
			return
		}
		io.WriteString(w, `{"spaces":[{"name":"spaces/CCC","displayName":"Dup","spaceType":"SPACE"},{"name":"spaces/DDD","spaceType":"DIRECT_MESSAGE"},{"name":"spaces/EEE","displayName":"standup2026","spaceType":"SPACE"}]}`)
	})
	mux.HandleFunc("/v1/media/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/media/res-1" || r.URL.Query().Get("alt") != "media" {
			t.Errorf("media request %s", r.URL)
		}
		io.WriteString(w, "FILEDATA")
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newClient(t *testing.T, f *fakeAPI) *Client {
	t.Helper()
	c, err := New(context.Background(), f.Client(), f.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testRange() daterange.Range {
	return daterange.Range{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Loc:  time.UTC,
	}
}

func TestFilterString(t *testing.T) {
	// Only `create_time` with > and < is documented; > (From - 1µs) is an
	// inclusive lower bound at the API's microsecond precision.
	want := `create_time > "2026-08-31T23:59:59.999999Z" AND create_time < "2026-10-01T00:00:00Z"`
	if got := Filter(testRange()); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func collect(t *testing.T, c *Client, token string, members map[string]string) ([][]model.Message, []string) {
	t.Helper()
	var pages [][]model.Message
	var tokens []string
	err := c.ListMessages(context.Background(), "spaces/AAA", testRange(), token, members, func(p []model.Message, next string) error {
		pages = append(pages, p)
		tokens = append(tokens, next)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return pages, tokens
}

func TestListMessagesPagesAndParams(t *testing.T) {
	f := newFakeAPI(t)
	pages, tokens := collect(t, newClient(t, f), "", nil)
	if len(pages) != 2 || tokens[0] != "p2" || tokens[1] != "" {
		t.Fatalf("pages %d tokens %q", len(pages), tokens)
	}
	q := f.queries[0]
	if q.Get("filter") != Filter(testRange()) || q.Get("orderBy") != "createTime ASC" || q.Get("pageSize") != "1000" || q.Get("showDeleted") != "false" {
		t.Fatalf("query %v", q)
	}
}

func TestListMessagesResumesFromToken(t *testing.T) {
	f := newFakeAPI(t)
	pages, _ := collect(t, newClient(t, f), "p2", nil)
	if len(pages) != 1 || f.queries[0].Get("pageToken") != "p2" {
		t.Fatalf("pages %d first query %v", len(pages), f.queries[0])
	}
}

func TestListMessagesCallbackErrorStops(t *testing.T) {
	f := newFakeAPI(t)
	stop := errors.New("stop")
	err := newClient(t, f).ListMessages(context.Background(), "spaces/AAA", testRange(), "", nil, func([]model.Message, string) error { return stop })
	if !errors.Is(err, stop) || len(f.queries) != 1 {
		t.Fatalf("err %v, %d requests", err, len(f.queries))
	}
}

func TestConvertMessage(t *testing.T) {
	pages, _ := collect(t, newClient(t, newFakeAPI(t)), "", nil)
	m1, m2 := pages[0][0], pages[0][1]
	if m1.ID != "spaces/AAA/messages/m1" || m1.ThreadID != "spaces/AAA/threads/t1" || m1.IsThreadReply {
		t.Fatalf("m1 ids %+v", m1)
	}
	if !m1.Time.Equal(time.Date(2026, 9, 3, 9, 2, 11, 123e6, time.UTC)) {
		t.Fatalf("m1 time %v", m1.Time)
	}
	if m1.EditedTime == nil || !m1.EditedTime.Equal(time.Date(2026, 9, 3, 9, 5, 0, 0, time.UTC)) {
		t.Fatalf("m1 edited %v", m1.EditedTime)
	}
	if m1.Sender != (model.User{ID: "users/1", DisplayName: "Jane Doe"}) || m1.Text != "hello" {
		t.Fatalf("m1 sender/text %+v", m1)
	}
	wantR := []model.Reaction{{Emoji: "👍", Count: 3}, {Emoji: ":party:", Count: 1}}
	if len(m1.Reactions) != 2 || m1.Reactions[0] != wantR[0] || m1.Reactions[1] != wantR[1] {
		t.Fatalf("reactions %+v", m1.Reactions)
	}
	up, drv := m1.Attachments[0], m1.Attachments[1]
	if up.Source != model.SourceUploaded || up.ResourceName != "res-1" || up.Name != "report.pdf" || up.ContentType != "application/pdf" {
		t.Fatalf("uploaded %+v", up)
	}
	if drv.Source != model.SourceDrive || drv.DriveURL != "https://drive.google.com/open?id=drv1" || drv.ResourceName != "" {
		t.Fatalf("drive %+v", drv)
	}
	if !m2.IsThreadReply || m2.EditedTime != nil || m2.QuotedMessageID != "spaces/AAA/messages/m1" {
		t.Fatalf("m2 %+v", m2)
	}
}

func TestSenderNameFallback(t *testing.T) {
	pages, _ := collect(t, newClient(t, newFakeAPI(t)), "", map[string]string{"users/2": "Bob"})
	if got := pages[0][1].Sender.DisplayName; got != "Bob" {
		t.Fatalf("member fallback = %q", got)
	}
	if got := pages[1][0].Sender.DisplayName; got != "users/3" {
		t.Fatalf("raw id fallback = %q", got)
	}
}

func TestListMembers(t *testing.T) {
	m, err := newClient(t, newFakeAPI(t)).ListMembers(context.Background(), "spaces/AAA")
	if err != nil || m["users/2"] != "Bob" {
		t.Fatalf("members %v err %v", m, err)
	}
}

func TestListSpaces(t *testing.T) {
	s, err := newClient(t, newFakeAPI(t)).ListSpaces(context.Background())
	if err != nil || len(s) != 5 || s[3] != (model.Space{Name: "spaces/DDD", Type: model.SpaceTypeDM}) {
		t.Fatalf("spaces %+v err %v", s, err)
	}
}

func TestResolveSpace(t *testing.T) {
	c := newClient(t, newFakeAPI(t))
	ctx := context.Background()
	if s, err := c.ResolveSpace(ctx, "spaces/AAA"); err != nil || s.DisplayName != "Team" {
		t.Fatalf("by name: %+v %v", s, err)
	}
	if s, err := c.ResolveSpace(ctx, "Team"); err != nil || s.Name != "spaces/AAA" {
		t.Fatalf("by display name: %+v %v", s, err)
	}
	if _, err := c.ResolveSpace(ctx, "Nope"); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := c.ResolveSpace(ctx, "spaces/MISSING"); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("missing by name: %v", err)
	}
	var amb *AmbiguousSpaceError
	if _, err := c.ResolveSpace(ctx, "Dup"); !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("ambiguous: %v", err)
	}
}

func TestDownloadMediaRequest(t *testing.T) {
	rc, err := newClient(t, newFakeAPI(t)).DownloadMedia(context.Background(), "res-1")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if b, _ := io.ReadAll(rc); string(b) != "FILEDATA" {
		t.Fatalf("body %q", b)
	}
}
