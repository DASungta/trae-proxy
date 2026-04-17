//go:build darwin

package privilege

import (
	"fmt"
	"os"
	"os/exec"
)

// RunPrivileged executes shellCmd with macOS administrator privileges.
// In a GUI session it pops the native macOS password dialog via osascript.
// In an SSH / non-GUI session it falls back to sudo (terminal prompt).
func RunPrivileged(shellCmd string) error {
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "" {
		cmd := exec.Command("sudo", "sh", "-c", shellCmd)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
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
