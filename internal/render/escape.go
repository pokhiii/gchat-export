package render

import (
	"net/url"
	"strings"
	"unicode"
)

// textEscaper neutralizes HTML, link and inline Markdown syntax in untrusted
// text. Brackets become entities so that no "](" or "<scheme:" sequence can
// form a link; emphasis, code, heading, strikethrough and table characters are
// backslash-escaped so message text cannot imitate the transcript's own
// formatting.
var textEscaper = strings.NewReplacer(
	`\`, `\\`,
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"[", "&#91;",
	"]", "&#93;",
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"#", `\#`,
	"~", `\~`,
	"|", `\|`,
)

// escapeText makes one line of untrusted text safe to embed in Markdown: line
// breaks become spaces, control characters (including terminal escape
// sequences) are removed, and Markdown syntax is escaped. Callers split
// multi-line text into lines first.
func escapeText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n':
			return ' '
		case r == '\t':
			return r
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	return escapeLineStart(textEscaper.Replace(s))
}

// escapeLineStart escapes block markers that only matter at the start of a
// line: list bullets, setext underlines and ordered-list numbers.
func escapeLineStart(s string) string {
	body := strings.TrimLeft(s, " \t")
	indent := s[:len(s)-len(body)]
	if body == "" {
		return s
	}
	switch body[0] {
	case '-', '+', '=':
		return indent + `\` + body
	}
	i := 0
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
	}
	if i > 0 && i < len(body) && (body[i] == '.' || body[i] == ')') {
		return indent + body[:i] + `\` + body[i:]
	}
	return s
}

// splitLines normalizes CRLF and lone CR to LF and splits on LF, so a bare CR
// can never start a new Markdown line inside a quoted reply.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

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
