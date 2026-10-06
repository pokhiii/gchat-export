// Package config resolves settings from the config file and environment.
// Command-line flags are applied on top by the cli package.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds non-secret settings. Tokens are never read from here.
type Config struct {
	ClientSecret      string  `json:"client_secret"` // path to the OAuth client JSON
	OutDir            string  `json:"out_dir"`
	TZ                string  `json:"tz"`
	RPS               float64 `json:"rps"`
	InsecureFileStore bool    `json:"insecure_file_store"`
}

// Dir is the per-user config directory for the tool.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gchat-export"), nil
}

// DefaultPath is <user config dir>/gchat-export/config.json.
func DefaultPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads path (a missing file is fine) and applies GCHAT_EXPORT_*
// environment variables from getenv.
func Load(path string, getenv func(string) string) (Config, error) {
	c := Config{OutDir: "./export", RPS: 10}
	b, err := os.ReadFile(path) // #nosec G304 -- user-chosen config path
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return c, err
	default:
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("config %s: %w", path, err)
		}
	}
	if v := getenv("GCHAT_EXPORT_CLIENT_SECRET"); v != "" {
		c.ClientSecret = v
	}
	if v := getenv("GCHAT_EXPORT_OUT"); v != "" {
		c.OutDir = v
	}
	if v := getenv("GCHAT_EXPORT_TZ"); v != "" {
		c.TZ = v
	}
	if v := getenv("GCHAT_EXPORT_RPS"); v != "" {
		rps, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return c, fmt.Errorf("GCHAT_EXPORT_RPS: %w", err)
		}
		c.RPS = rps
	}
	if c.RPS <= 0 {
		return c, errors.New("rps must be greater than 0")
	}
	return c, nil
}
