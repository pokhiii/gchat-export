package chat

import (
	"net/url"
	"time"

	"github.com/pokhiii/gchat-export/internal/model"
	chatapi "google.golang.org/api/chat/v1"
)

func convertSpace(s *chatapi.Space) model.Space {
	return model.Space{Name: s.Name, DisplayName: s.DisplayName, Type: model.SpaceType(s.SpaceType)}
}

func convertMessage(m *chatapi.Message, members map[string]string) model.Message {
	out := model.Message{
		ID:            m.Name,
		IsThreadReply: m.ThreadReply,
		Time:          parseTime(m.CreateTime),
		Text:          m.Text,
	}
	if m.Thread != nil {
		out.ThreadID = m.Thread.Name
	}
	if m.LastUpdateTime != "" && m.LastUpdateTime != m.CreateTime {
		if t := parseTime(m.LastUpdateTime); !t.IsZero() && !t.Equal(out.Time) {
			out.EditedTime = &t
		}
	}
	if m.Sender != nil {
		out.Sender = model.User{ID: m.Sender.Name, DisplayName: senderName(m.Sender, members)}
	}
	for _, r := range m.EmojiReactionSummaries {
		if r.Emoji == nil {
			continue
		}
		out.Reactions = append(out.Reactions, model.Reaction{Emoji: emojiText(r.Emoji), Count: r.ReactionCount})
	}
	for _, a := range m.Attachment {
		out.Attachments = append(out.Attachments, convertAttachment(a))
	}
	if m.QuotedMessageMetadata != nil {
		out.QuotedMessageID = m.QuotedMessageMetadata.Name
	}
	return out
}

func senderName(u *chatapi.User, members map[string]string) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if n := members[u.Name]; n != "" {
		return n
	}
	return u.Name
}

func emojiText(e *chatapi.Emoji) string {
	if e.Unicode != "" {
		return e.Unicode
	}
	if c := e.CustomEmoji; c != nil {
		if c.EmojiName != "" {
			return c.EmojiName
		}
		return ":" + c.Uid + ":"
	}
	return "?"
}

func convertAttachment(a *chatapi.Attachment) model.Attachment {
	out := model.Attachment{Name: a.ContentName, ContentType: a.ContentType}
	if a.Source == "DRIVE_FILE" && a.DriveDataRef != nil {
		out.Source = model.SourceDrive
		out.DriveURL = "https://drive.google.com/open?id=" + url.QueryEscape(a.DriveDataRef.DriveFileId)
		return out
	}
	out.Source = model.SourceUploaded
	if a.AttachmentDataRef != nil {
		out.ResourceName = a.AttachmentDataRef.ResourceName
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
