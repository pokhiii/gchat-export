package render

import (
	"strings"
	"testing"
)

func TestEscapeText(t *testing.T) {
	if got := escapeText("<script>alert(1)</script>"); strings.Contains(got, "<script") {
		t.Fatalf("script survived: %q", got)
	}
	if got := escapeText("[x](javascript:alert(1))"); strings.Contains(got, "](") {
		t.Fatalf("link survived: %q", got)
	}
	if got := escapeText(`\[x](javascript:alert(1))`); strings.Contains(got, `\](`) || !strings.Contains(got, `\\`) {
		t.Fatalf("backslash bypass: %q", got)
	}
	if got := escapeText("a & b"); got != "a &amp; b" {
		t.Fatalf("ampersand: %q", got)
	}
	if got := escapeText("plain words, 42 items"); got != "plain words, 42 items" {
		t.Fatalf("plain text altered: %q", got)
	}
	// Emphasis is escaped on purpose: it is how a forged "**Name** · 09:15"
	// header would be built.
	if got := escapeText("*bold*"); got != `\*bold\*` {
		t.Fatalf("emphasis not escaped: %q", got)
	}
}

func TestSafeLink(t *testing.T) {
	bad := []string{"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,x", "file:///etc/passwd",
		"attachments/../x", "../x", "/etc/passwd", "vbscript:x", "https://exa mple.com/<x>"}
	for _, u := range bad {
		if _, ok := safeLink(u); ok {
			t.Errorf("safeLink(%q) = ok", u)
		}
	}
	good := []string{"https://drive.google.com/open?id=abc", "http://example.com/x", "attachments/m_0_a.png", "attachments/m_0_a b (1).png"}
	for _, u := range good {
		if _, ok := safeLink(u); !ok {
			t.Errorf("safeLink(%q) rejected", u)
		}
	}
}
