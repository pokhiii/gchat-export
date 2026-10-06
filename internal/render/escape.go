package render

import (
	"net/url"
	"strings"
	"unicode"
)

// textEscaper neutralizes HTML and link syntax in untrusted text. Brackets
// become entities so that no "](" or "<scheme:" sequence can form a link.
var textEscaper = strings.NewReplacer(
	`\`, `\\`,
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"[", "&#91;",
	"]", "&#93;",
)

func escapeText(s string) string { return textEscaper.Replace(s) }

// safeLink allows http(s) URLs with a host and relative attachment paths;
// everything else must be rendered as plain text.
func safeLink(u string) (string, bool) {
	if strings.ContainsAny(u, "<>\\") || strings.ContainsFunc(u, unicode.IsControl) {
		return "", false
	}
	if strings.HasPrefix(u, "attachments/") {
		if strings.Contains(u, "..") {
			return "", false
		}
		return u, true
	}
	if strings.ContainsAny(u, " \t") {
		return "", false
	}
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return "", false
	}
	switch strings.ToLower(p.Scheme) {
	case "http", "https":
		return u, true
	}
	return "", false
}
