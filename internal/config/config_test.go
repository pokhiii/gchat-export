package config

import (
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.json"), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.OutDir != "./export" || c.RPS != 10 || c.ClientSecret != "" || c.InsecureFileStore {
		t.Fatalf("defaults %+v", c)
	}
}

func TestConfigPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"client_secret":"/file/cs.json","out_dir":"/file/out","tz":"UTC","rps":5,"insecure_file_store":true}`), 0o600)
	c, err := Load(path, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientSecret != "/file/cs.json" || c.OutDir != "/file/out" || c.TZ != "UTC" || c.RPS != 5 || !c.InsecureFileStore {
		t.Fatalf("file %+v", c)
	}
	c, err = Load(path, env(map[string]string{
		"GCHAT_EXPORT_CLIENT_SECRET": "/env/cs.json", "GCHAT_EXPORT_OUT": "/env/out",
		"GCHAT_EXPORT_TZ": "Asia/Kolkata", "GCHAT_EXPORT_RPS": "2.5",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientSecret != "/env/cs.json" || c.OutDir != "/env/out" || c.TZ != "Asia/Kolkata" || c.RPS != 2.5 {
		t.Fatalf("env %+v", c)
	}
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"refresh_token":"x"}`), 0o600)
	if _, err := Load(path, env(nil)); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestConfigRejectsBadRPS(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "x.json"), env(map[string]string{"GCHAT_EXPORT_RPS": "fast"})); err == nil {
		t.Fatal("bad rps accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "x.json"), env(map[string]string{"GCHAT_EXPORT_RPS": "0"})); err == nil {
		t.Fatal("zero rps accepted")
	}
}
