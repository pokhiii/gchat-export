//go:build !windows

package fsutil

import (
	"os"
	"syscall"
)

func openNoFollow(path string, flag int) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, 0o600)
}
