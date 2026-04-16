//go:build windows

package privilege

import "errors"

var ErrNotSupported = errors.New("osascript not supported on Windows; please run as administrator")

func RunPrivileged(shellCmd string) error { return ErrNotSupported }
func IsPrivileged() bool                  { return false }
