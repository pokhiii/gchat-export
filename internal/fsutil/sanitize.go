package fsutil

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxNameBytes = 200
	maxExtBytes  = 16
)

var (
	dotRuns        = regexp.MustCompile(`\.{2,}`)
	underscoreRuns = regexp.MustCompile(`_{2,}`)
	reservedNames  = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`)
)

// SanitizeName turns an untrusted name into a single safe path component.
func SanitizeName(name string) string {
	s := norm.NFC.String(strings.ToValidUTF8(name, "_"))
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, s)
	s = dotRuns.ReplaceAllString(s, "_")
	s = underscoreRuns.ReplaceAllString(s, "_")
	s = strings.TrimLeft(s, ".-_ ")
	s = strings.TrimRight(s, ". ")

	base, _, _ := strings.Cut(s, ".")
	if reservedNames.MatchString(base) {
		s = "_" + s
	}
	s = truncate(s)
	if s == "" {
		return "file"
	}
	return s
}

// truncate shortens s to maxNameBytes on a rune boundary, keeping a short
// trailing extension.
func truncate(s string) string {
	if len(s) <= maxNameBytes {
		return s
	}
	stem, ext := s, ""
	if i := strings.LastIndexByte(s, '.'); i > 0 && len(s)-i <= maxExtBytes {
		stem, ext = s[:i], s[i:]
	}
	limit := maxNameBytes - len(ext)
	for len(stem) > limit {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return strings.TrimRight(stem, ". ") + ext
}
