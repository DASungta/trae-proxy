//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func StopDaemon() error {
	pid, err := ReadPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	// Wait for the process to actually exit so callers can immediately reuse the port.
	// proc.Wait() only works for child processes; for daemons use polling.
	for i := 0; i < 50; i++ {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break // process gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	os.Remove(PIDPath())
	return nil
}

func IsRunning() (int, bool) {
	pid, err := ReadPID()
	if err != nil {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}
