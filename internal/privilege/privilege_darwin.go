//go:build darwin

package privilege

import (
	"fmt"
	"os"
	"os/exec"
)

// RunPrivileged executes shellCmd with macOS administrator privileges via osascript.
// It pops the native macOS password dialog. Returns error if user cancels.
func RunPrivileged(shellCmd string) error {
	script := fmt.Sprintf(`do shell script %q with administrator privileges`, shellCmd)
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IsPrivileged returns true if the current process is running as root.
func IsPrivileged() bool {
	return os.Geteuid() == 0
}
