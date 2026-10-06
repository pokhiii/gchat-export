package render

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/OWNER/gchat-export/internal/daterange"
	"github.com/OWNER/gchat-export/internal/model"
)

// Header describes the transcript. Title is precomputed by the caller so
// unnamed spaces (DMs) still get a meaningful heading.
type Header struct {
	Space       model.Space
	Title       string
	Range       daterange.Range
	GeneratedAt time.Time
}

type thread struct{ msgs []model.Message }

// RenderMarkdown writes a readable transcript: messages grouped by thread,
// threads ordered by their first message, day headings in the range's zone.
// All untrusted text is escaped.
func RenderMarkdown(w io.Writer, h Header, msgs []model.Message) error {
	loc := h.Range.Loc
	if loc == nil {
		loc = time.UTC
	}
	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "# %s\n\n", escapeText(h.Title))
	fmt.Fprintf(bw, "- Type: %s\n", escapeText(string(h.Space.Type)))
	fmt.Fprintf(bw, "- Range: %s – %s (%s)\n", fmtBound(h.Range.From, h.Range, loc), fmtBound(h.Range.To, h.Range, loc), loc)
	fmt.Fprintf(bw, "- Generated: %s\n", h.GeneratedAt.In(loc).Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(bw, "- Messages: %d\n", len(msgs))

	lastDay := ""
	for _, th := range groupThreads(msgs) {
		day := th.msgs[0].Time.In(loc).Format("2006-01-02")
		if day != lastDay {
			fmt.Fprintf(bw, "\n## %s\n", day)
			lastDay = day
		}
		for i, m := range th.msgs {
			prefix := ""
			if i > 0 {
				prefix = "> "
			}
			_, _ = bw.WriteString("\n") // errors surface at Flush
			writeMessage(bw, m, prefix, i == 0 && m.IsThreadReply, day, loc)
		}
	}
	return bw.Flush()
}

func fmtBound(t time.Time, r daterange.Range, loc *time.Location) string {
	if r.DateOnly {
		return t.In(loc).Format("2006-01-02")
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func groupThreads(msgs []model.Message) []*thread {
	var order []*thread
	byID := map[string]*thread{}
	for _, m := range msgs {
		key := m.ThreadID
		if key == "" {
			key = "msg:" + m.ID
		}
		th, ok := byID[key]
		if !ok {
			th = &thread{}
			byID[key] = th
			order = append(order, th)
		}
		th.msgs = append(th.msgs, m)
	}
	return order
}

// writeMessage renders one message. day is the heading it appears under; a
// reply posted on a later day shows its full date.
func writeMessage(w *bufio.Writer, m model.Message, prefix string, orphan bool, day string, loc *time.Location) {
	var lines []string
	head := ""
	if orphan {
		head = "↳ reply in thread · "
	}
	stamp := m.Time.In(loc).Format("15:04")
	if d := m.Time.In(loc).Format("2006-01-02"); d != day {
		stamp = d + " " + stamp
	}
	head += fmt.Sprintf("**%s** · %s", escapeText(m.Sender.DisplayName), stamp)
	if m.EditedTime != nil {
		head += " (edited)"
	}
	lines = append(lines, head, "")
	if m.QuotedMessageID != "" {
		lines = append(lines, "> _quoting "+escapeText(m.QuotedMessageID)+"_", "")
	}
	if m.Text != "" {
		for _, l := range splitLines(m.Text) {
			lines = append(lines, escapeText(l)+"  ")
		}
		lines = append(lines, "")
	}
	if len(m.Reactions) > 0 {
		parts := make([]string, len(m.Reactions))
		for i, r := range m.Reactions {
			parts[i] = fmt.Sprintf("%s %d", escapeText(r.Emoji), r.Count)
		}
		lines = append(lines, strings.Join(parts, " · "), "")
	}
	for _, a := range m.Attachments {
		lines = append(lines, "- "+attachmentLink(a))
	}
	if len(m.Attachments) > 0 {
		lines = append(lines, "")
	}
	// Drop the trailing blank line; the caller separates messages.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for _, l := range lines {
		_, _ = w.WriteString(strings.TrimRight(prefix+l, " ") + trailingBreak(l) + "\n") // errors surface at Flush
	}
}

// trailingBreak keeps Markdown hard line breaks on message text lines.
func trailingBreak(l string) string {
	if strings.HasSuffix(l, "  ") && strings.TrimSpace(l) != "" {
		return "  "
	}
	return ""
}

func attachmentLink(a model.Attachment) string {
	name := escapeText(a.Name)
	if name == "" {
		name = "attachment"
	}
	target := a.Path
	if a.Source == model.SourceDrive {
		target = a.DriveURL
	}
	if target == "" {
		return name + " (not downloaded)"
	}
	if u, ok := safeLink(target); ok {
		return fmt.Sprintf("[%s](<%s>)", name, u)
	}
	return name + " (link removed)"
}
