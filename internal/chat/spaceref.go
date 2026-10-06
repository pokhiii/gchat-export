package chat

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	spaceIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	// bareIDRE is the shape tried as a space ID only after no display name
	// matched, so a space really named like an ID still wins.
	bareIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{6,}$`)
)

// looksLikeURL reports whether ref should be parsed as a URL rather than a
// name.
func looksLikeURL(ref string) bool {
	return strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://")
}

// ParseSpaceRef returns the "spaces/ID" resource name for a "spaces/ID" string
// or a Google Chat URL (chat.google.com or mail.google.com/chat, https only).
// It returns ok=false for anything else, including display names.
func ParseSpaceRef(ref string) (name string, ok bool) {
	if id, found := strings.CutPrefix(ref, "spaces/"); found {
		return spaceName(id)
	}
	if !looksLikeURL(ref) {
		return "", false
	}
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "chat.google.com":
		if len(segs) >= 2 && segs[0] == "u" {
			segs = segs[2:]
		}
		switch {
		case len(segs) >= 3 && segs[0] == "app" && segs[1] == "chat":
			return spaceName(segs[2])
		case len(segs) >= 2 && (segs[0] == "room" || segs[0] == "dm"):
			return spaceName(segs[1])
		}
	case "mail.google.com":
		// Standalone Chat (/chat/…) and Chat inside Gmail (/mail/…).
		if segs[0] != "chat" && segs[0] != "mail" {
			return "", false
		}
		frag := strings.Split(u.EscapedFragment(), "/")
		if len(frag) >= 3 && frag[0] == "chat" && (frag[1] == "space" || frag[1] == "dm") {
			return spaceName(frag[2])
		}
	}
	return "", false
}

func spaceName(id string) (string, bool) {
	if !spaceIDRE.MatchString(id) {
		return "", false
	}
	return "spaces/" + id, true
}
