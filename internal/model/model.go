// Package model holds the tool's own representation of Chat data, decoupled
// from the Google API types.
package model

import (
	"strings"
	"time"
)

type SpaceType string

const (
	SpaceTypeSpace SpaceType = "SPACE"
	SpaceTypeGroup SpaceType = "GROUP_CHAT"
	SpaceTypeDM    SpaceType = "DIRECT_MESSAGE"
)

// Space is a Chat space; Name is the resource name, e.g. "spaces/AAAA".
type Space struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Type        SpaceType `json:"type"`
}

// ID returns the space ID without the "spaces/" prefix.
func (s Space) ID() string { return lastSegment(s.Name) }

// User identifies a message sender; ID is "users/123".
type User struct {
	ID          string
	DisplayName string
}

type Reaction struct {
	Emoji string `json:"emoji"`
	Count int64  `json:"count"`
}

type AttachmentSource string

const (
	SourceUploaded AttachmentSource = "uploaded"
	SourceDrive    AttachmentSource = "drive"
)

type Attachment struct {
	Name         string
	ContentType  string
	Source       AttachmentSource
	ResourceName string // uploaded: attachmentDataRef.resourceName
	DriveURL     string // drive: https://drive.google.com/open?id=<driveFileId>
	Path         string // set after download; relative to the export dir
	SHA256       string
	Size         int64
}

type Message struct {
	ID              string
	ThreadID        string
	IsThreadReply   bool
	Time            time.Time
	EditedTime      *time.Time
	Sender          User
	Text            string
	Reactions       []Reaction
	Attachments     []Attachment
	QuotedMessageID string
}

// ShortID returns the last path segment of the message resource name.
func (m Message) ShortID() string { return lastSegment(m.ID) }

func lastSegment(s string) string {
	return s[strings.LastIndexByte(s, '/')+1:]
}
