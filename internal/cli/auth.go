package cli

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/OWNER/gchat-export/internal/auth"
	"github.com/OWNER/gchat-export/internal/fsutil"
	"github.com/spf13/cobra"
)

const loginTimeout = 2 * time.Minute

func newAuthCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Log in, check status, or log out"}
	cmd.AddCommand(
		&cobra.Command{Use: "login", Short: "Log in with your Google account (opens a browser)", Args: cobra.NoArgs, RunE: a.authLogin},
		&cobra.Command{Use: "status", Short: "Show the logged-in account and granted scopes", Args: cobra.NoArgs, RunE: a.authStatus},
		&cobra.Command{Use: "logout", Short: "Revoke the token at Google and delete it locally", Args: cobra.NoArgs, RunE: a.authLogout},
	)
	return cmd
}

func (a *app) authLogin(cmd *cobra.Command, _ []string) error {
	cfg, err := a.oauthConfig(nil)
	if err != nil {
		return err
	}
	secretPath, err := filepath.Abs(a.cfg.ClientSecret)
	if err != nil {
		return err
	}
	if err := fsutil.CheckPrivate(secretPath); errors.Is(err, fsutil.ErrInsecurePerms) {
		fmt.Fprintf(a.stderr, "warning: %s is readable by other users; run: chmod 600 %q\n", secretPath, secretPath)
	}
	store, err := a.store()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	fmt.Fprintln(a.stdout, "Opening your browser to log in…")
	tok, err := auth.Login(ctx, cfg, openBrowser, loginTimeout)
	if err != nil {
		return err
	}
	email, scopes, err := auth.FetchTokenInfo(ctx, http.DefaultClient, auth.DefaultEndpoints, tok.AccessToken)
	if err != nil {
		return err
	}
	cred := &auth.Credential{Email: email, Scopes: scopes, Token: tok, ClientSecretPath: secretPath}
	if err := store.Save(cred); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Logged in as %s\n", termSafe(email))
	for _, s := range auth.Scopes {
		if !slices.Contains(scopes, s) {
			fmt.Fprintf(a.stderr, "warning: scope not granted: %s (some commands will fail)\n", s)
		}
	}
	return nil
}

func (a *app) authStatus(cmd *cobra.Command, _ []string) error {
	store, err := a.store()
	if err != nil {
		return err
	}
	cred, err := store.Load()
	if err != nil {
		return err
	}
	where := "OS keychain"
	if a.cfg.InsecureFileStore {
		where = "file (insecure-file-store)"
	}
	fmt.Fprintf(a.stdout, "Logged in as %s\nToken stored in: %s\nScopes:\n", termSafe(cred.Email), where)
	for _, s := range cred.Scopes {
		fmt.Fprintf(a.stdout, "  %s\n", termSafe(s))
	}
	return nil
}

func (a *app) authLogout(cmd *cobra.Command, _ []string) error {
	store, err := a.store()
	if err != nil {
		return err
	}
	cred, err := store.Load()
	if errors.Is(err, auth.ErrNoCredential) {
		fmt.Fprintln(a.stdout, "Not logged in.")
		return nil
	}
	if err != nil {
		return err
	}
	if cred.Token != nil {
		tok := cred.Token.RefreshToken
		if tok == "" {
			tok = cred.Token.AccessToken
		}
		if err := auth.Revoke(cmd.Context(), http.DefaultClient, auth.DefaultEndpoints, tok); err != nil {
			fmt.Fprintf(a.stderr, "warning: could not revoke token at Google (%s); revoke it at https://myaccount.google.com/permissions\n", userMessage(err))
		}
	}
	if err := store.Delete(); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "Logged out.")
	return nil
}
