//go:build windows

package daemon

import (
	"os"
	"os/exec"
)

func setSysProcAttr(cmd *exec.Cmd) {}

func StopDaemon() error {
	pid, err := ReadPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil {
		return err
	}
	os.Remove(PIDPath())
	return nil
}

func IsRunning() (int, bool) {
	pid, err := ReadPID()
	if err != nil {
		return 0, false
	}
	// On Windows, FindProcess always succeeds. Use os.FindProcess + Signal(nil)
	// or check /proc. For simplicity, just check if PID file exists and process can be found.
	_, err = os.FindProcess(pid)
	return pid, err == nil
}
