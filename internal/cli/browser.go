package cli

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// openBrowser opens an https URL with the OS handler, without a shell.
func openBrowser(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("refusing to open non-https URL")
	}
	// #nosec G204 -- fixed binaries; the URL is validated as https and passed as
	// a single argument, never through a shell.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204
	default:
		cmd = exec.Command("xdg-open", url) // #nosec G204
	}
	return cmd.Start()
}
