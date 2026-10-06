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
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
