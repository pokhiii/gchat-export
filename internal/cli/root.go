// Package cli wires the gchat-export commands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/OWNER/gchat-export/internal/auth"
	"github.com/OWNER/gchat-export/internal/chat"
	"github.com/OWNER/gchat-export/internal/config"
	"github.com/OWNER/gchat-export/internal/ratelimit"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// errPartial signals exit code 2 after a completed export with failures.
var errPartial = errors.New("export completed with errors")

type app struct {
	stdout, stderr    io.Writer
	version           string
	configPath        string
	clientSecret      string
	insecureFileStore bool
	verbose           bool
	cfg               config.Config
	log               *slog.Logger
}

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, version string, stdout, stderr io.Writer, args []string) int {
	a := &app{stdout: stdout, stderr: stderr, version: version}
	root := newRootCmd(a)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errPartial):
		return 2
	}
	fmt.Fprintln(stderr, "error:", userMessage(err))
	return 1
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "gchat-export",
		Short:         "Export Google Chat conversations to Markdown and JSONL",
		Version:       a.version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.init(cmd)
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&a.configPath, "config", "", "config file (default <user config dir>/gchat-export/config.json)")
	pf.StringVar(&a.clientSecret, "client-secret", "", "path to your Desktop OAuth client JSON")
	pf.BoolVar(&a.insecureFileStore, "insecure-file-store", false, "store the token in a 0600 file instead of the OS keychain")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "log API calls, counts and timings (never tokens or message text)")
	root.AddCommand(newAuthCmd(a), newSpacesCmd(a), newExportCmd(a))
	return root
}

func (a *app) init(cmd *cobra.Command) error {
	path := a.configPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}
	cfg, err := config.Load(path, os.Getenv)
	if err != nil {
		return err
	}
	flags := cmd.Flags()
	if flags.Changed("client-secret") {
		cfg.ClientSecret = a.clientSecret
	}
	if flags.Changed("insecure-file-store") {
		cfg.InsecureFileStore = a.insecureFileStore
	}
	a.cfg = cfg
	level := slog.LevelWarn
	if a.verbose {
		level = slog.LevelDebug
	}
	a.log = slog.New(NewRedactingHandler(slog.NewTextHandler(a.stderr, &slog.HandlerOptions{Level: level})))
	return nil
}

func (a *app) store() (auth.Store, error) {
	if !a.cfg.InsecureFileStore {
		return auth.NewKeyringStore(), nil
	}
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return auth.NewFileStore(dir), nil
}

func (a *app) oauthConfig(cred *auth.Credential) (*oauth2.Config, error) {
	path := a.cfg.ClientSecret
	if path == "" && cred != nil {
		path = cred.ClientSecretPath
	}
	if path == "" {
		return nil, errors.New("--client-secret is required (path to your Desktop OAuth client JSON; see docs/setup.md)")
	}
	return auth.LoadClientConfig(path)
}

// chatClient builds an authenticated, rate-limited Chat client.
func (a *app) chatClient(ctx context.Context) (*chat.Client, *auth.Credential, error) {
	store, err := a.store()
	if err != nil {
		return nil, nil, err
	}
	cred, err := store.Load()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := a.oauthConfig(cred)
	if err != nil {
		return nil, nil, err
	}
	base := &http.Client{Transport: ratelimit.NewTransport(http.DefaultTransport, a.cfg.RPS)}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	hc := oauth2.NewClient(ctx, auth.PersistingTokenSource(ctx, cfg, cred, store))
	c, err := chat.New(ctx, hc, "")
	return c, cred, err
}
