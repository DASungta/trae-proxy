//go:build linux

package privilege

import (
	"errors"
	"os"
)

// ErrNotSupported is returned on platforms where osascript is unavailable.
var ErrNotSupported = errors.New("osascript not supported on this platform; please use sudo")

func RunPrivileged(shellCmd string) error { return ErrNotSupported }
func IsPrivileged() bool                  { return os.Geteuid() == 0 }
