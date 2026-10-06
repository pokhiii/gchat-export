//go:build windows

package fsutil

import (
	"fmt"
	"os"
)

// Windows has no O_NOFOLLOW; reject an existing symlink immediately before
// opening.
func openNoFollow(path string, flag int) (*os.File, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: refusing to follow symlink: %w", path, ErrNotRegular)
	}
	return os.OpenFile(path, flag, 0o600)
}
