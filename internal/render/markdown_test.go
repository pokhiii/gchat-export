package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/OWNER/gchat-export/internal/daterange"
	"github.com/OWNER/gchat-export/internal/model"
)

func testHeader(t *testing.T) Header {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	return Header{
		Space: model.Space{Name: "spaces/AAA", DisplayName: "Team", Type: model.SpaceTypeSpace},
		Title: "Team",
		Range: daterange.Range{
			From: ts("2026-08-31T18:30:00Z"), To: ts("2026-09-30T18:30:00Z"), Loc: loc, DateOnly: true,
		},
		GeneratedAt: ts("2026-10-06T06:30:00Z"),
	}
}

func render(t *testing.T, msgs []model.Message) string {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, testHeader(t), msgs); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRenderGolden(t *testing.T) {
	golden(t, "transcript.golden.md", []byte(render(t, fixtureMessages())))
}

func TestRenderGroupsRepliesUnderRoot(t *testing.T) {
	msgs := fixtureMessages()
	// A later top-level message arrives between root and reply in time order.
	other := model.Message{ID: "spaces/AAA/messages/x", ThreadID: "spaces/AAA/threads/tx",
		Time: ts("2026-09-03T03:50:00Z"), Sender: model.User{DisplayName: "Eve"}, Text: "interleaved"}
	out := render(t, []model.Message{msgs[0], other, msgs[1]})
	if !(strings.Index(out, "Kickoff") < strings.Index(out, "Thanks!") && strings.Index(out, "Thanks!") < strings.Index(out, "interleaved")) {
		t.Fatalf("reply not grouped under its root:\n%s", out)
	}
	if !strings.Contains(out, "> Thanks!") {
		t.Fatalf("reply not blockquoted:\n%s", out)
	}
}

func TestOrphanReply(t *testing.T) {
	out := render(t, []model.Message{fixtureMessages()[1]})
	if !strings.Contains(out, "↳ reply in thread") || !strings.Contains(out, "Thanks!") {
		t.Fatalf("orphan reply not rendered:\n%s", out)
	}
}

func TestRenderTimesInZone(t *testing.T) {
	out := render(t, fixtureMessages())
	// 03:32Z is 09:02 in Asia/Kolkata.
	if !strings.Contains(out, "**Jane Doe** · 09:02") {
		t.Fatalf("time not localized:\n%s", out)
	}
	if !strings.Contains(out, "## 2026-09-05") {
		t.Fatalf("19:00Z on 09-04 should be 09-05 local:\n%s", out)
	}
}

func TestMaliciousAttachmentName(t *testing.T) {
	m := model.Message{ID: "spaces/A/messages/m", ThreadID: "t", Time: ts("2026-09-03T03:32:11Z"),
		Sender:      model.User{DisplayName: "x"},
		Attachments: []model.Attachment{{Name: "](javascript:alert(1))", Source: model.SourceUploaded, Path: "attachments/m_0_x"}}}
	out := render(t, []model.Message{m})
	if strings.Contains(out, "](javascript") {
		t.Fatalf("attachment name not escaped:\n%s", out)
	}
}

func TestUnsafeLinkRenderedAsText(t *testing.T) {
	m := model.Message{ID: "spaces/A/messages/m", ThreadID: "t", Time: ts("2026-09-03T03:32:11Z"),
		Sender:      model.User{DisplayName: "x"},
		Attachments: []model.Attachment{{Name: "evil", Source: model.SourceDrive, DriveURL: "javascript:alert(1)"}}}
	out := render(t, []model.Message{m})
	if strings.Contains(out, "(javascript:") || strings.Contains(out, "<javascript:") {
		t.Fatalf("unsafe link emitted:\n%s", out)
	}
}

func TestMaliciousSenderAndText(t *testing.T) {
	m := model.Message{ID: "spaces/A/messages/m", ThreadID: "t", Time: ts("2026-09-03T03:32:11Z"),
		Sender: model.User{DisplayName: "<img src=x onerror=alert(1)>"},
		Text:   "<script>alert(1)</script>\n[click](javascript:alert(1))\n<javascript:alert(1)>"}
	out := render(t, []model.Message{m})
	for _, bad := range []string{"<script", "<img", "](javascript", "<javascript"} {
		if strings.Contains(out, bad) {
			t.Fatalf("output contains %q:\n%s", bad, out)
		}
	}
}
