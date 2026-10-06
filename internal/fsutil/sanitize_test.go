package fsutil

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":       "report.pdf",
		"../../etc/passwd": "etc_passwd",
		"a/b\\c":           "a_b_c",
		"CON":              "_CON",
		"nul.txt":          "_nul.txt",
		".bashrc":          "bashrc",
		"-rf":              "rf",
		"":                 "file",
		"...":              "file",
		"a\x00b\x1fc":      "abc",
		"é.txt":      "é.txt",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeNameLongKeepsExtension(t *testing.T) {
	in := strings.Repeat("a", 300) + ".pdf"
	got := SanitizeName(in)
	if len(got) > 200 {
		t.Fatalf("len = %d", len(got))
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("lost extension: %q", got)
	}
}

func checkSanitized(t *testing.T, in, out string) {
	t.Helper()
	if out == "" {
		t.Fatalf("empty output for %q", in)
	}
	if strings.ContainsAny(out, `/\`) || strings.Contains(out, "..") {
		t.Fatalf("unsafe output %q for %q", out, in)
	}
	if len(out) > 200 {
		t.Fatalf("too long (%d) for %q", len(out), in)
	}
	root := filepath.FromSlash("/root")
	if err := Contained(root, filepath.Join(root, out)); err != nil {
		t.Fatalf("not contained: %q for %q", out, in)
	}
}

func FuzzSanitizeName(f *testing.F) {
	for _, s := range []string{"a.txt", "../x", "CON", "", "...", "\x00", "a/../../b", strings.Repeat("é", 200)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkSanitized(t, in, SanitizeName(in))
	})
}
