package main

import (
	"os"
	"testing"
	"time"

	"github.com/zhangyc/trae-proxy/internal/daemon"
)

type stubFileInfo struct {
	mode os.FileMode
}

func (s stubFileInfo) Name() string       { return "" }
func (s stubFileInfo) Size() int64        { return 0 }
func (s stubFileInfo) Mode() os.FileMode  { return s.mode }
func (s stubFileInfo) ModTime() time.Time { return time.Time{} }
func (s stubFileInfo) IsDir() bool        { return false }
func (s stubFileInfo) Sys() any           { return nil }

func TestStartOptionsDaemonArgs(t *testing.T) {
	args := startOptions{
		configPath: "/tmp/config.toml",
		upstream:   "http://127.0.0.1:8080",
		listen:     ":8443",
	}.daemonArgs()

	want := []string{"start", "--config", "/tmp/config.toml", "--upstream", "http://127.0.0.1:8080", "--listen", ":8443"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestStartOptionsDaemonArgsReloadDefaultConfig(t *testing.T) {
	args := (startOptions{}).daemonArgs()
	want := []string{"start"}

	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestRestartDaemonStopsAndStartsRunningDaemon(t *testing.T) {
	origIsRunning := daemonIsRunning
	origStop := daemonStop
	origStart := daemonStart
	origRemoveHosts := removeHosts
	defer func() {
		daemonIsRunning = origIsRunning
		daemonStop = origStop
		daemonStart = origStart
		removeHosts = origRemoveHosts
	}()

	var stopped, hostsRemoved bool
	var startedArgs []string

	daemonIsRunning = func() (int, bool) { return 1234, true }
	daemonStop = func() error {
		stopped = true
		return nil
	}
	daemonStart = func(args []string) error {
		startedArgs = append([]string(nil), args...)
		return nil
	}
	removeHosts = func() error {
		hostsRemoved = true
		return nil
	}

	opts := startOptions{
		configPath: "/tmp/config.toml",
		upstream:   "http://127.0.0.1:8080",
		listen:     ":8443",
	}
	if err := restartDaemon(opts); err != nil {
		t.Fatalf("restartDaemon() error = %v", err)
	}

	if !stopped {
		t.Fatal("expected daemonStop to be called")
	}
	if !hostsRemoved {
		t.Fatal("expected removeHosts to be called")
	}

	want := opts.daemonArgs()
	if len(startedArgs) != len(want) {
		t.Fatalf("started args len = %d, want %d", len(startedArgs), len(want))
	}
	for i := range want {
		if startedArgs[i] != want[i] {
			t.Fatalf("startedArgs[%d] = %q, want %q", i, startedArgs[i], want[i])
		}
	}
}

func TestRestartDaemonStartsWhenNotRunning(t *testing.T) {
	origIsRunning := daemonIsRunning
	origStop := daemonStop
	origStart := daemonStart
	origRemoveHosts := removeHosts
	defer func() {
		daemonIsRunning = origIsRunning
		daemonStop = origStop
		daemonStart = origStart
		removeHosts = origRemoveHosts
	}()

	var stopCalled bool
	var startCalled bool

	daemonIsRunning = func() (int, bool) { return 0, false }
	daemonStop = func() error {
		stopCalled = true
		return nil
	}
	daemonStart = func(args []string) error {
		startCalled = true
		return nil
	}
	removeHosts = func() error { return nil }

	if err := restartDaemon(startOptions{}); err != nil {
		t.Fatalf("restartDaemon() error = %v", err)
	}
	if stopCalled {
		t.Fatal("did not expect daemonStop to be called")
	}
	if !startCalled {
		t.Fatal("expected daemonStart to be called")
	}
}

func TestRestartDaemonRemovesStalePID(t *testing.T) {
	origIsRunning := daemonIsRunning
	origStop := daemonStop
	origStart := daemonStart
	origRemoveHosts := removeHosts
	defer func() {
		daemonIsRunning = origIsRunning
		daemonStop = origStop
		daemonStart = origStart
		removeHosts = origRemoveHosts
	}()

	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer func() {
		_ = os.Setenv("HOME", oldHome)
	}()

	pidPath := daemon.PIDPath()
	if err := os.MkdirAll(filepathDir(pidPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("9999"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	daemonIsRunning = func() (int, bool) { return 9999, false }
	daemonStop = func() error {
		t.Fatal("did not expect daemonStop to be called for stale pid")
		return nil
	}
	daemonStart = func(args []string) error { return nil }
	removeHosts = func() error { return nil }

	if err := restartDaemon(startOptions{}); err != nil {
		t.Fatalf("restartDaemon() error = %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid file to be removed, stat err = %v", err)
	}
}

func TestShouldColorizeFileInfo(t *testing.T) {
	tests := []struct {
		name    string
		info    os.FileInfo
		term    string
		noColor string
		want    bool
	}{
		{
			name: "tty enabled",
			info: stubFileInfo{mode: os.ModeCharDevice},
			term: "xterm-256color",
			want: true,
		},
		{
			name: "non tty disabled",
			info: stubFileInfo{mode: 0},
			term: "xterm-256color",
			want: false,
		},
		{
			name:    "no color disabled",
			info:    stubFileInfo{mode: os.ModeCharDevice},
			term:    "xterm-256color",
			noColor: "1",
			want:    false,
		},
		{
			name: "dumb terminal disabled",
			info: stubFileInfo{mode: os.ModeCharDevice},
			term: "dumb",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldColorizeFileInfo(tt.info, tt.term, tt.noColor); got != tt.want {
				t.Fatalf("shouldColorizeFileInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestColorStatusLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		level   colorLevel
		enabled bool
		want    string
	}{
		{
			name:    "ok green",
			line:    "[daemon] ✓ running (pid 1234)",
			level:   levelOK,
			enabled: true,
			want:    colorGreen + "[daemon] ✓ running (pid 1234)" + colorReset,
		},
		{
			name:    "warn yellow",
			line:    "[daemon] ✗ dead (stale pid 1234)",
			level:   levelWarn,
			enabled: true,
			want:    colorYellow + "[daemon] ✗ dead (stale pid 1234)" + colorReset,
		},
		{
			name:    "error red",
			line:    "[daemon] ✗ not running",
			level:   levelError,
			enabled: true,
			want:    colorRed + "[daemon] ✗ not running" + colorReset,
		},
		{
			name:    "disabled returns plain",
			line:    "[daemon] ✗ dead (stale pid 1234)",
			level:   levelWarn,
			enabled: false,
			want:    "[daemon] ✗ dead (stale pid 1234)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorStatusLine(tt.line, tt.level, tt.enabled)
			if got != tt.want {
				t.Fatalf("colorStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == os.PathSeparator {
			return path[:i]
		}
	}
	return "."
}
