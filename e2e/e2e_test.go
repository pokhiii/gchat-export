//go:build e2e

// Package e2e runs gchat-export against the real Chat API with the
// developer's own account. It never runs in CI.
//
// Prerequisites: `gchat-export auth login` done once, then
//
//	GCHAT_E2E_SPACE=spaces/AAAA GCHAT_E2E_SINCE=2026-09-01 go test -tags e2e ./e2e/ -v
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OWNER/gchat-export/internal/cli"
)

func TestExportAgainstRealAPI(t *testing.T) {
	space, since := os.Getenv("GCHAT_E2E_SPACE"), os.Getenv("GCHAT_E2E_SINCE")
	if space == "" || since == "" {
		t.Skip("set GCHAT_E2E_SPACE and GCHAT_E2E_SINCE")
	}
	sinceT, err := time.Parse("2006-01-02", since)
	if err != nil {
		t.Fatal(err)
	}
	until := sinceT.AddDate(0, 0, 7).Format("2006-01-02")
	ctx := context.Background()

	var out, errb bytes.Buffer
	if code := cli.Execute(ctx, "e2e", &out, &errb, []string{"spaces", "--json"}); code != 0 {
		t.Fatalf("spaces exit %d: %s", code, errb.String())
	}

	root := t.TempDir()
	out.Reset()
	errb.Reset()
	code := cli.Execute(ctx, "e2e", &out, &errb, []string{
		"export", "--space", space, "--since", since, "--until", until, "--tz", "UTC",
		"--attachments", "--out", root,
	})
	if code != 0 && code != 2 {
		t.Fatalf("export exit %d: %s", code, errb.String())
	}

	manifests, _ := filepath.Glob(filepath.Join(root, "*", "*", "manifest.json"))
	if len(manifests) != 1 {
		t.Fatalf("manifests: %v", manifests)
	}
	dir := filepath.Dir(manifests[0])
	var m struct {
		Messages    int `json:"messages"`
		Attachments []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"attachments"`
	}
	b, err := os.ReadFile(manifests[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Messages == 0 {
		t.Fatalf("no messages in %s – %s; pick a busier week", since, until)
	}
	for _, a := range m.Attachments {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(a.Path)))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			t.Errorf("hash mismatch for %s", a.Path)
		}
	}
	md, err := os.ReadFile(filepath.Join(dir, "transcript.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "<script") {
		t.Error("transcript contains raw <script")
	}
	t.Logf("exported %d messages, %d attachments", m.Messages, len(m.Attachments))
}
