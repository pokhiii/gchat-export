package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// run executes the CLI with an isolated, empty config file so tests never
// read the developer's real configuration.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	cfg := filepath.Join(t.TempDir(), "config.json")
	code := Execute(context.Background(), "test", &out, &errb, append([]string{"--config", cfg}, args...))
	return code, out.String(), errb.String()
}

func TestExportFlagValidation(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"export"}, "--space"},
		{[]string{"export", "--space", "x"}, "--since"},
		{[]string{"export", "--space", "x", "--since", "yesterday"}, "--since"},
		{[]string{"export", "--space", "x", "--since", "2026-09-02", "--until", "2026-09-01"}, "before"},
		{[]string{"export", "--space", "x", "--since", "2026-09-01", "--format", "xml"}, "--format"},
		{[]string{"export", "--space", "x", "--since", "2026-09-01", "--tz", "Mars/Base"}, "--tz"},
		{[]string{"export", "--space", "x", "--since", "2026-09-01", "--max-attachment-size", "lots"}, "--max-attachment-size"},
		{[]string{"export", "--space", "x", "--since", "2026-09-01", "--resume", "--force"}, "--resume"},
		{[]string{"export", "--space", "https://evil.example/room/AAA", "--since", "2026-09-01"}, "Unrecognized Google Chat URL"},
	}
	for _, c := range cases {
		code, _, stderr := run(t, c.args...)
		if code != 1 || !strings.Contains(stderr, c.want) {
			t.Errorf("%v: code %d stderr %q, want substring %q", c.args, code, stderr, c.want)
		}
	}
}

func TestHelpListsCommands(t *testing.T) {
	code, out, _ := run(t, "--help")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, c := range []string{"auth", "spaces", "export"} {
		if !strings.Contains(out, c) {
			t.Errorf("help missing %q", c)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != 0 || !strings.Contains(out, "test") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestNoTokenFlagsExist(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		check := func(f *pflag.Flag) {
			n := strings.ToLower(f.Name)
			if (strings.Contains(n, "token") || strings.Contains(n, "secret")) && n != "client-secret" {
				t.Errorf("command %q has sensitive flag --%s", c.Name(), f.Name)
			}
		}
		c.Flags().VisitAll(check)
		c.PersistentFlags().VisitAll(check)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd(&app{}))
}

func TestParseSize(t *testing.T) {
	good := map[string]int64{"100MB": 100_000_000, "5MiB": 5_242_880, "123": 123, "1kb": 1000, "2GiB": 2 << 30}
	for in, want := range good {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"-1", "x", "", "0", "10XB", "99999999999999999999"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) succeeded", in)
		}
	}
}

func TestParseFormats(t *testing.T) {
	md, jl, err := parseFormats("md,jsonl")
	if err != nil || !md || !jl {
		t.Fatalf("%v %v %v", md, jl, err)
	}
	md, jl, err = parseFormats("jsonl")
	if err != nil || md || !jl {
		t.Fatalf("%v %v %v", md, jl, err)
	}
	if _, _, err := parseFormats(""); err == nil {
		t.Fatal("empty formats accepted")
	}
}

func TestOpenBrowserRejectsNonHTTPS(t *testing.T) {
	if err := openBrowser("file:///etc/passwd"); err == nil {
		t.Fatal("non-https URL accepted")
	}
}
