// Package render writes exported messages as JSONL and Markdown.
package render

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/OWNER/gchat-export/internal/model"
)

type record struct {
	ID              string           `json:"id"`
	ThreadID        *string          `json:"thread_id"`
	IsThreadReply   bool             `json:"is_thread_reply"`
	Time            string           `json:"time"`
	EditedTime      *string          `json:"edited_time"`
	SenderID        string           `json:"sender_id"`
	SenderName      string           `json:"sender_name"`
	Text            string           `json:"text"`
	Reactions       []model.Reaction `json:"reactions"`
	Attachments     []attachmentRec  `json:"attachments"`
	QuotedMessageID *string          `json:"quoted_message_id"`
}

type attachmentRec struct {
	Name        string  `json:"name"`
	ContentType string  `json:"content_type"`
	Source      string  `json:"source"`
	Path        *string `json:"path"`
	SHA256      *string `json:"sha256"`
	Size        *int64  `json:"size"`
	DriveURL    *string `json:"drive_url"`
}

// WriteJSONL writes m as one JSON line. Attachment resource names are
// internal API handles and are not written.
func WriteJSONL(w io.Writer, m model.Message) error {
	rec := record{
		ID:              m.ID,
		ThreadID:        strPtr(m.ThreadID),
		IsThreadReply:   m.IsThreadReply,
		Time:            m.Time.UTC().Format(time.RFC3339Nano),
		SenderID:        m.Sender.ID,
		SenderName:      m.Sender.DisplayName,
		Text:            m.Text,
		Reactions:       m.Reactions,
		Attachments:     []attachmentRec{},
		QuotedMessageID: strPtr(m.QuotedMessageID),
	}
	if rec.Reactions == nil {
		rec.Reactions = []model.Reaction{}
	}
	if m.EditedTime != nil {
		s := m.EditedTime.UTC().Format(time.RFC3339Nano)
		rec.EditedTime = &s
	}
	for _, a := range m.Attachments {
		ar := attachmentRec{
			Name: a.Name, ContentType: a.ContentType, Source: string(a.Source),
			Path: strPtr(a.Path), SHA256: strPtr(a.SHA256), DriveURL: strPtr(a.DriveURL),
		}
		if a.Size != 0 {
			size := a.Size
			ar.Size = &size
		}
		rec.Attachments = append(rec.Attachments, ar)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(rec)
}

// ReadJSONL parses messages written by WriteJSONL.
func ReadJSONL(r io.Reader) ([]model.Message, error) {
	var out []model.Message
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for line := 1; sc.Scan(); line++ {
		var rec record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("messages.jsonl line %d: %w", line, err)
		}
		m, err := rec.toMessage()
		if err != nil {
			return nil, fmt.Errorf("messages.jsonl line %d: %w", line, err)
		}
		out = append(out, m)
	}
	return out, sc.Err()
}

func (rec record) toMessage() (model.Message, error) {
	t, err := time.Parse(time.RFC3339Nano, rec.Time)
	if err != nil {
		return model.Message{}, err
	}
	m := model.Message{
		ID:              rec.ID,
		ThreadID:        deref(rec.ThreadID),
		IsThreadReply:   rec.IsThreadReply,
		Time:            t,
		Sender:          model.User{ID: rec.SenderID, DisplayName: rec.SenderName},
		Text:            rec.Text,
		QuotedMessageID: deref(rec.QuotedMessageID),
	}
	if rec.EditedTime != nil {
		et, err := time.Parse(time.RFC3339Nano, *rec.EditedTime)
		if err != nil {
			return model.Message{}, err
		}
		m.EditedTime = &et
	}
	if len(rec.Reactions) > 0 {
		m.Reactions = rec.Reactions
	}
	for _, a := range rec.Attachments {
		att := model.Attachment{
			Name: a.Name, ContentType: a.ContentType, Source: model.AttachmentSource(a.Source),
			Path: deref(a.Path), SHA256: deref(a.SHA256), DriveURL: deref(a.DriveURL),
		}
		if a.Size != nil {
			att.Size = *a.Size
		}
		m.Attachments = append(m.Attachments, att)
	}
	return m, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
