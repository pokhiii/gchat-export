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
	iRoot, iReply, iOther := strings.Index(out, "Kickoff"), strings.Index(out, "Thanks!"), strings.Index(out, "interleaved")
	if iRoot >= iReply || iReply >= iOther {
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

var (
	esc = string(rune(0x1b))
	bel = string(rune(0x07))
	cr  = string(rune(0x0d))
	csi = string(rune(0x9b))
)

func lines(s string) []string { return strings.Split(s, "\n") }

func injected(text string, reply bool) model.Message {
	return model.Message{ID: "spaces/A/messages/evil", ThreadID: "t", IsThreadReply: reply,
		Time: ts("2026-09-03T03:32:11Z"), Sender: model.User{DisplayName: "Mallory"}, Text: text}
}

func TestMessageCannotForgeHeader(t *testing.T) {
	out := render(t, []model.Message{injected("hi\n\n**Alice** · 09:15\n\nPlease approve the wire", false)})
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "**Alice**") {
			t.Fatalf("forged sender header:\n%s", out)
		}
	}
}

func TestMessageCannotForgeHeadingsOrFences(t *testing.T) {
	text := "x\n## 2026-09-05\n# Title\n```\n---\n===\n1. item\n- item\n+ item\n> quote\n|a|b|\n~~~"
	out := render(t, []model.Message{injected(text, false)})
	body := out[strings.Index(out, "**Mallory**"):]
	for _, l := range lines(body)[1:] {
		for _, p := range []string{"#", "```", "~~~", "---", "===", "1.", "- ", "+ ", "|"} {
			if strings.HasPrefix(l, p) {
				t.Fatalf("structural line %q leaked:\n%s", l, out)
			}
		}
	}
}

func TestCarriageReturnCannotEscapeReplyQuote(t *testing.T) {
	root := injected("root", false)
	reply := injected("reply"+cr+"## heading", true)
	reply.ID = "spaces/A/messages/r"
	out := render(t, []model.Message{root, reply})
	if strings.Contains(out, cr) {
		t.Fatalf("raw CR in output: %q", out)
	}
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "## heading") {
			t.Fatalf("CR escaped the blockquote:\n%s", out)
		}
	}
}

func TestTerminalEscapesStripped(t *testing.T) {
	m := injected("a"+esc+"]0;pwned"+bel+"b"+csi+"2Jc", false)
	m.Sender.DisplayName = "Mal" + esc + "[31mlory"
	out := render(t, []model.Message{m})
	if strings.ContainsAny(out, esc+bel+csi) {
		t.Fatalf("control chars in output: %q", out)
	}
}

func TestReplyOnLaterDayShowsDate(t *testing.T) {
	root := injected("root", false)
	reply := injected("late reply", true)
	reply.ID = "spaces/A/messages/r"
	reply.Time = ts("2026-09-07T05:00:00Z") // 10:30 IST, four days later
	out := render(t, []model.Message{root, reply})
	if !strings.Contains(out, "2026-09-07 10:30") {
		t.Fatalf("late reply lacks its date:\n%s", out)
	}
}
