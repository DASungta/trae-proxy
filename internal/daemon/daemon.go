package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/zhangyc/trae-proxy/internal/config"
)

func PIDPath() string {
	dir, _ := config.ConfigDir()
	return filepath.Join(dir, "trae-proxy.pid")
}

func LogPath() string {
	dir, _ := config.ConfigDir()
	return filepath.Join(dir, "trae-proxy.log")
}

func Daemonize() error {
	logFile, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	args := filterDaemonFlag(os.Args[1:])
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	if err := os.WriteFile(PIDPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		return fmt.Errorf("write PID: %w", err)
	}

	fmt.Printf("[trae-proxy] daemon started (pid %d)\n", cmd.Process.Pid)
	fmt.Printf("[trae-proxy] log: %s\n", LogPath())
	os.Exit(0)
	return nil
}

func ReadPID() (int, error) {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func filterDaemonFlag(args []string) []string {
	var result []string
	for _, a := range args {
		if a != "-d" && a != "--daemon" {
			result = append(result, a)
		}
	}
	return result
}
