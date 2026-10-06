package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OWNER/gchat-export/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func tp(s string) *time.Time { t := ts(s); return &t }

// fixtureMessages covers threads, replies, edits, reactions, both attachment
// kinds, quotes, unicode and two days.
func fixtureMessages() []model.Message {
	return []model.Message{
		{
			ID: "spaces/AAA/messages/m1", ThreadID: "spaces/AAA/threads/t1",
			Time: ts("2026-09-03T03:32:11Z"), EditedTime: tp("2026-09-03T03:40:00Z"),
			Sender:    model.User{ID: "users/1", DisplayName: "Jane Doe"},
			Text:      "Kickoff notes — see attached 📎",
			Reactions: []model.Reaction{{Emoji: "👍", Count: 3}, {Emoji: "🎉", Count: 1}},
			Attachments: []model.Attachment{
				{Name: "report.pdf", ContentType: "application/pdf", Source: model.SourceUploaded,
					ResourceName: "res-1", Path: "attachments/m1_0_report.pdf", SHA256: "abc123", Size: 42},
				{Name: "Plan", ContentType: "application/vnd.google-apps.document", Source: model.SourceDrive,
					DriveURL: "https://drive.google.com/open?id=drv1"},
			},
		},
		{
			ID: "spaces/AAA/messages/m2", ThreadID: "spaces/AAA/threads/t1", IsThreadReply: true,
			Time:   ts("2026-09-03T04:00:00Z"),
			Sender: model.User{ID: "users/2", DisplayName: "Bob"},
			Text:   "Thanks!", QuotedMessageID: "spaces/AAA/messages/m1",
		},
		{
			ID: "spaces/AAA/messages/m3", ThreadID: "spaces/AAA/threads/t3",
			Time:   ts("2026-09-04T19:00:00Z"),
			Sender: model.User{ID: "users/3", DisplayName: "users/3"},
			Text:   "Next day, <b>bold</b> & more",
		},
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
