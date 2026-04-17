package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/daemon"
	"github.com/zhangyc/trae-proxy/internal/hosts"
	"github.com/zhangyc/trae-proxy/internal/logging"
	"github.com/zhangyc/trae-proxy/internal/privilege"
	"github.com/zhangyc/trae-proxy/internal/proxy"
	tlsutil "github.com/zhangyc/trae-proxy/internal/tls"
	"github.com/zhangyc/trae-proxy/internal/updater"
)

var version = "dev"

var (
	daemonIsRunning = daemon.IsRunning
	daemonStop      = daemon.StopDaemon
	daemonStart     = daemon.DaemonizeArgs
	removeHosts     = hosts.Remove
)

type startOptions struct {
	daemonMode bool
	upstream   string
	configPath string
	listen     string
	logLevel   string
	logBody    bool
}

type colorLevel string

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"

	levelOK    colorLevel = "ok"
	levelWarn  colorLevel = "warn"
	levelError colorLevel = "error"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "trae-proxy",
		Short:   "HTTPS proxy for Anthropic API translation",
		Version: version,
	}

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(restartCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(uninstallCmd())
	rootCmd.AddCommand(updateCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate CA, install trust, create default config",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Clean up any stale /etc/hosts entries left by a previously crashed/killed
			// start process (signal handler only fires on SIGINT/SIGTERM, not SIGKILL).
			if _, running := daemon.IsRunning(); !running {
				if data, err := os.ReadFile(hosts.HostsPath()); err == nil {
					if strings.Contains(string(data), "# trae-proxy") {
						fmt.Println("[init] detected stale hosts entry from prior run, cleaning up...")
						if err := hosts.Remove(); err != nil {
							fmt.Printf("[init] WARNING: could not clean stale hosts entries: %v\n", err)
						}
					}
				}
			}

			dir, err := config.ConfigDir()
			if err != nil {
				return err
			}
			caDir := filepath.Join(dir, "ca")
			os.MkdirAll(caDir, 0755)

			configPath := filepath.Join(dir, "config.toml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				interactive := !yes && isTerminal(os.Stdin)
				if interactive {
					if err := runWizard(configPath, os.Stdin, os.Stdout); err != nil {
						return fmt.Errorf("wizard: %w", err)
					}
				} else {
					src, err := os.ReadFile("config.example.toml")
					if err != nil {
						cfg := config.DefaultConfig()
						fmt.Printf("[init] generating default config at %s\n", configPath)
						_ = writeDefaultConfig(configPath, cfg)
					} else {
						os.WriteFile(configPath, src, 0644)
						fmt.Printf("[init] config copied to %s\n", configPath)
					}
				}
			} else {
				fmt.Printf("[init] config already exists at %s\n", configPath)
			}

			caFile := filepath.Join(caDir, "root-ca.pem")
			if _, err := os.Stat(caFile); os.IsNotExist(err) {
				fmt.Println("[init] generating Root CA...")
				if err := tlsutil.GenerateCA(caDir); err != nil {
					return fmt.Errorf("generate CA: %w", err)
				}
			} else {
				fmt.Println("[init] CA already exists")
			}

			cfg, _ := config.Load(configPath, nil)
			if tlsutil.NeedsRegeneration(caDir, cfg.Hijack) {
				fmt.Printf("[init] generating server cert for %s...\n", cfg.Hijack)
				caCert, caKey, err := tlsutil.LoadCA(caDir)
				if err != nil {
					return fmt.Errorf("load CA: %w", err)
				}
				if err := tlsutil.GenerateServerCert(caDir, caCert, caKey, cfg.Hijack); err != nil {
					return fmt.Errorf("generate server cert: %w", err)
				}
			}

			fmt.Println("[init] installing CA to system trust store (may prompt for password)...")
			if runtime.GOOS == "darwin" {
				fmt.Println("[init] 需要系统权限安装 CA 证书，即将弹出系统授权对话框...")
			}
			if err := tlsutil.InstallCA(filepath.Join(caDir, "root-ca.pem")); err != nil {
				fmt.Printf("[init] WARNING: failed to install CA: %v\n", err)
				fmt.Println("[init] you may need to manually trust the CA")
			} else {
				fmt.Println("[init] CA installed successfully")
			}

			fmt.Println("\n[init] done! Run 'trae-proxy start' to begin.")
			fmt.Println("[init] 提示：请打开 Trae，在模型设置中选择对应的模型即可使用。")
			fmt.Println("[init] 如需调整更多配置，可编辑", configPath)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive wizard and use default config")
	return cmd
}

func startCmd() *cobra.Command {
	var opts startOptions

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the proxy (adds hosts entry, starts HTTPS server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.daemonMode {
				return daemonStart(opts.daemonArgs())
			}

			return runProxy(opts)
		},
	}

	bindStartFlags(cmd, &opts, true)

	return cmd
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon and remove hosts entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if pid, running := daemonIsRunning(); running {
				fmt.Printf("[stop] stopping daemon (pid %d)...\n", pid)
				if err := daemonStop(); err != nil {
					return err
				}
				fmt.Println("[stop] daemon stopped")
			} else {
				fmt.Println("[stop] daemon not running")
			}

			fmt.Println("[stop] removing hosts entry...")
			removeHosts()
			fmt.Println("[stop] done")
			return nil
		},
	}
}

func restartCmd() *cobra.Command {
	var opts startOptions

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon and reload config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := restartDaemon(opts); err != nil {
				return err
			}
			fmt.Println("[restart] done")
			return nil
		},
	}

	bindStartFlags(cmd, &opts, false)
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show proxy status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("=== trae-proxy status ===")
			fmt.Println()

			dir, _ := config.ConfigDir()
			configPath := filepath.Join(dir, "config.toml")
			cfg, _ := config.Load(configPath, nil)
			colorize := shouldColorize(os.Stdout)

			has, _ := hosts.HasEntry(cfg.Hijack)
			if has {
				fmt.Println(colorStatusLine(fmt.Sprintf("[hosts] ✓ %s → 127.0.0.1", cfg.Hijack), levelOK, colorize))
			} else {
				fmt.Println(colorStatusLine(fmt.Sprintf("[hosts] ✗ %s not redirected", cfg.Hijack), levelError, colorize))
			}

			if pid, running := daemonIsRunning(); running {
				fmt.Println(colorStatusLine(fmt.Sprintf("[daemon] ✓ running (pid %d)", pid), levelOK, colorize))
			} else if pid > 0 {
				fmt.Println(colorStatusLine(fmt.Sprintf("[daemon] ✗ dead (stale pid %d)", pid), levelWarn, colorize))
			} else {
				fmt.Println(colorStatusLine("[daemon] ✗ not running", levelError, colorize))
			}

			fmt.Println()
			fmt.Printf("Upstream: %s\n", cfg.Upstream)
			fmt.Printf("Protocol: %s\n", cfg.UpstreamProtocol)
			fmt.Printf("Listen:   %s\n", cfg.Listen)
			fmt.Printf("Hijack:   %s\n", cfg.Hijack)
			fmt.Printf("Models:   %d mappings\n", len(cfg.Models))
			return nil
		},
	}
}

func uninstallCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove CA from trust store, clean up hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Stop daemon if running.
			if pid, running := daemon.IsRunning(); running {
				fmt.Printf("[uninstall] stopping daemon (pid %d)...\n", pid)
				if err := daemon.StopDaemon(); err != nil {
					fmt.Printf("[uninstall] WARNING: could not stop daemon: %v\n", err)
				} else {
					fmt.Println("[uninstall] daemon stopped")
				}
			}

			if pid443 := findPort443Process(); pid443 > 0 {
				fmt.Printf("[uninstall] 检测到 PID %d 仍占用 443 端口，尝试终止...\n", pid443)
				killProcess(pid443)
			}

			dir, _ := config.ConfigDir()
			caDir := filepath.Join(dir, "ca")

			caCertPath := filepath.Join(caDir, "root-ca.pem")

			if runtime.GOOS == "darwin" {
				// Combine CA removal + hosts cleanup into a single privileged call.
				if err := darwinUninstallPrivileged(caCertPath); err != nil {
					fmt.Printf("[uninstall] WARNING: %v\n", err)
				}
			} else {
				if _, err := os.Stat(caCertPath); err == nil {
					fmt.Println("[uninstall] removing CA from system trust store...")
					if err := tlsutil.UninstallCA(caCertPath); err != nil {
						fmt.Printf("[uninstall] WARNING: %v\n", err)
					}
				}

				fmt.Println("[uninstall] removing hosts entry...")
				hosts.Remove()
			}

			// Remove the binary itself.
			exePath, err := os.Executable()
			if err == nil {
				exePath, err = filepath.EvalSymlinks(exePath)
			}
			if err == nil {
				fmt.Printf("[uninstall] removing binary %s...\n", exePath)
				if err := os.Remove(exePath); err != nil {
					if os.IsPermission(err) {
						if err2 := removePrivileged(exePath); err2 != nil {
							fmt.Printf("[uninstall] WARNING: could not remove binary: %v\n", err2)
						}
					} else {
						fmt.Printf("[uninstall] WARNING: could not remove binary: %v\n", err)
					}
				}
			}

			// Ask whether to remove the config directory.
			removeCfg := yes
			if !yes {
				fmt.Printf("[uninstall] remove config directory %s? [y/N] ", dir)
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
					removeCfg = answer == "y" || answer == "yes"
				}
			}
			if removeCfg {
				fmt.Printf("[uninstall] removing config directory %s...\n", dir)
				if err := os.RemoveAll(dir); err != nil {
					fmt.Printf("[uninstall] WARNING: could not remove config directory: %v\n", err)
				}
			} else {
				fmt.Printf("[uninstall] keeping config directory %s\n", dir)
			}

			fmt.Println("[uninstall] done")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Automatically confirm config directory removal")
	return cmd
}

func updateCmd() *cobra.Command {
	var (
		targetVersion string
		force         bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update trae-proxy to the latest release (macOS/Linux only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			oldVersion := version
			assetName, err := updater.AssetName()
			if err != nil {
				return err
			}

			u := updater.New()

			tag := targetVersion
			if tag == "" {
				fmt.Println("[update] fetching latest release...")
				tag, err = u.LatestTag()
				if err != nil {
					return err
				}
			}

			if !force && tag == version {
				fmt.Printf("[update] already up to date (%s)\n", version)
				return nil
			}

			exePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("find executable: %w", err)
			}
			exePath, err = filepath.EvalSymlinks(exePath)
			if err != nil {
				return fmt.Errorf("resolve symlinks: %w", err)
			}

			fmt.Printf("[update] fetching checksum for %s %s...\n", assetName, tag)
			expectedSHA, err := u.FetchChecksum(tag, assetName)
			if err != nil {
				return err
			}

			fmt.Printf("[update] downloading %s...\n", assetName)
			tmpPath, err := u.Download(tag, assetName, exePath)
			if err != nil {
				return err
			}
			defer os.Remove(tmpPath) // cleanup on verify/replace failure

			fmt.Println("[update] verifying checksum...")
			if err := updater.Verify(tmpPath, expectedSHA); err != nil {
				return err
			}

			fmt.Printf("[update] installing to %s...\n", exePath)
			if err := updater.Replace(exePath, tmpPath); err != nil {
				return err
			}

			fmt.Printf("[update] updated from %s to %s\n", oldVersion, tag)
			PrintMigrationGuide(oldVersion, tag)
			if pid, running := daemon.IsRunning(); running {
				fmt.Printf("[update] daemon is running (pid %d) — restart it to use the new version\n", pid)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&targetVersion, "version", "", "Target version (default: latest)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-install even if version matches")
	return cmd
}

func writeDefaultConfig(path string, cfg *config.Config) error {
	content := fmt.Sprintf(`# trae-proxy configuration

# Upstream API address
# 上游服务地址，路径不要包含/v1/messages或/v1/chat/completions
# 示例：- 移动云：OpenAI：https://ai.bayesdl.com/api/maas/
# 示例：- 京东云：OpenAI：https://modelservice.jdcloud.com/coding/openai
# 示例：- 京东云：Anthropic：https://modelservice.jdcloud.com/coding/anthropic
# 示例：- sub2api：直接填端点地址
upstream = "%s"

# Upstream protocol: "anthropic" (default) performs OpenAI → Anthropic Messages
# conversion. "openai" directly forwards OpenAI Chat Completions — use this when
# upstream is OpenAI-compatible (LM Studio, Ollama, most relays).
# 上游服务是Anthropic协议填anthropic，如果是openai兼容填openai
upstream_protocol = "anthropic"

# HTTPS listen address
listen = "%s"

# Domain to hijack via /etc/hosts
hijack = "%s"

# Log level: error | warn | info (default) | debug | trace
# trace adds four tap points per request: client body, proxy internal form,
# upstream payload, upstream response. Authorization/x-api-key are always redacted.
log_level = "info"

# When true, the trace level prints full request/response bodies.
# Leave false unless debugging payloads — bodies may contain API keys / secrets.
log_body = false

# When true, GET /v1/models forwards to the real hijack domain (bypassing /etc/hosts)
# instead of returning the fake list from [models] below.
# real_models = false

# Model mapping: request model name → upstream model name
# 3-tier fallback: exact match → strip "anthropic/" prefix → passthrough
# 如果劫持openrouter，模型名称是有"anthropic/"、"openai/"等前缀
# 以下是当前Trae中OpenRouter列出的模型，任选一个将请求模型映射到上游服务提供的真实模型
[models]
"anthropic/claude-sonnet-4.5" = "claude-sonnet-4.6"
"anthropic/claude-opus-4.1" = "claude-opus-4.6"
"anthropic/claude-4-sonnet" = ""
"anthropic/claude-4-opus" = ""
"anthropic/claude-3.7-sonnet" = ""
"openai/gpt-5" = "gpt-5.4"
"openai/gpt-4.1" = ""
"openai/gpt-4o" = ""
"google/gemini-3-pro-perview" = ""
"google/gemini-2.5-pro" = ""
"minimax/minimax-m2" = ""
"qwen/qwen3-coder" = ""
`, cfg.Upstream, cfg.Listen, cfg.Hijack)
	return os.WriteFile(path, []byte(content), 0644)
}

func bindStartFlags(cmd *cobra.Command, opts *startOptions, includeDaemon bool) {
	if includeDaemon {
		cmd.Flags().BoolVarP(&opts.daemonMode, "daemon", "d", false, "Run as background daemon")
	}
	cmd.Flags().StringVar(&opts.upstream, "upstream", "", "Override upstream URL")
	cmd.Flags().StringVar(&opts.configPath, "config", "", "Config file path")
	cmd.Flags().StringVar(&opts.listen, "listen", "", "Override listen address")
	cmd.Flags().StringVarP(&opts.logLevel, "log-level", "l", "", "Log level: trace|debug|info|warn|error")
	cmd.Flags().BoolVar(&opts.logBody, "log-body", false, "Print full request/response bodies at trace level")
}

func (o startOptions) daemonArgs() []string {
	args := []string{"start"}
	if o.configPath != "" {
		args = append(args, "--config", o.configPath)
	}
	if o.upstream != "" {
		args = append(args, "--upstream", o.upstream)
	}
	if o.listen != "" {
		args = append(args, "--listen", o.listen)
	}
	if o.logLevel != "" {
		args = append(args, "--log-level", o.logLevel)
	}
	if o.logBody {
		args = append(args, "--log-body")
	}
	return args
}

func (o startOptions) resolvedConfigPath() (string, error) {
	if o.configPath != "" {
		return o.configPath, nil
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func (o startOptions) overrides() map[string]string {
	overrides := map[string]string{}
	if o.upstream != "" {
		overrides["upstream"] = o.upstream
	}
	if o.listen != "" {
		overrides["listen"] = o.listen
	}
	if o.logLevel != "" {
		overrides["log_level"] = o.logLevel
	}
	if o.logBody {
		overrides["log_body"] = "true"
	}
	return overrides
}

func runProxy(opts startOptions) error {
	dir, _ := config.ConfigDir()
	caDir := filepath.Join(dir, "ca")
	if _, err := os.Stat(filepath.Join(caDir, "root-ca.pem")); os.IsNotExist(err) {
		return fmt.Errorf("not initialized. Run 'trae-proxy init' first")
	}

	configPath, err := opts.resolvedConfigPath()
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath, opts.overrides())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if changed, report := config.Migrate(configPath, cfg); changed {
		for _, msg := range report {
			fmt.Printf("[migrate] %s\n", msg)
		}
	}

	fmt.Printf("[start] adding hosts entry for %s...\n", cfg.Hijack)
	if runtime.GOOS == "darwin" {
		fmt.Printf("[start] 需要系统权限修改 /etc/hosts，即将弹出系统授权对话框...\n")
	}
	if err := hosts.Add(cfg.Hijack); err != nil {
		return fmt.Errorf("add hosts: %w", err)
	}
	var removeOnce sync.Once
	cleanupHosts := func() {
		removeOnce.Do(func() { hosts.Remove() })
	}
	defer cleanupHosts()

	tlsCfg, err := tlsutil.LoadServerTLSConfig(caDir)
	if err != nil {
		return fmt.Errorf("load TLS config: %w", err)
	}

	logLevel, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		return err
	}
	logger := logging.New(logLevel, cfg.LogBody, os.Stderr)

	srv := proxy.NewServer(cfg, logger)
	srv.TLSConfig = tlsCfg

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[trae-proxy] shutting down...")
		cleanupHosts()
		cancel()
	}()

	return srv.ListenAndServe(ctx)
}

func restartDaemon(opts startOptions) error {
	if pid, running := daemonIsRunning(); running {
		fmt.Printf("[restart] stopping daemon (pid %d)...\n", pid)
		if err := daemonStop(); err != nil {
			return err
		}
		fmt.Println("[restart] daemon stopped")
	} else if pid > 0 {
		fmt.Printf("[restart] removing stale pid %d...\n", pid)
		if err := os.Remove(daemon.PIDPath()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale pid: %w", err)
		}
	}

	fmt.Println("[restart] removing hosts entry...")
	removeHosts()

	fmt.Println("[restart] starting daemon...")
	if err := daemonStart(opts.daemonArgs()); err != nil {
		return err
	}
	return nil
}

func shouldColorize(out *os.File) bool {
	info, err := out.Stat()
	if err != nil {
		return false
	}
	return shouldColorizeFileInfo(info, os.Getenv("TERM"), os.Getenv("NO_COLOR"))
}

func shouldColorizeFileInfo(info os.FileInfo, term, noColor string) bool {
	if noColor != "" || term == "dumb" {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func colorStatusLine(line string, level colorLevel, enabled bool) string {
	if !enabled {
		return line
	}

	color := ""
	switch level {
	case levelOK:
		color = colorGreen
	case levelWarn:
		color = colorYellow
	case levelError:
		color = colorRed
	default:
		return line
	}

	return color + line + colorReset
}

// findPort443Process returns PID of a process named "trae-proxy" listening on :443, or 0 if none.
func killProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err == nil {
		time.Sleep(500 * time.Millisecond)
		if findPort443Process() == 0 {
			return
		}
	}
	fmt.Printf("[uninstall] 进程未退出，强制终止...\n")
	if runtime.GOOS == "darwin" {
		_ = privilege.RunPrivileged(fmt.Sprintf("kill -9 %d", pid))
	} else {
		_ = proc.Kill()
	}
}

func darwinUninstallPrivileged(caCertPath string) error {
	var cmds []string

	if _, err := os.Stat(caCertPath); err == nil {
		fmt.Println("[uninstall] removing CA from system trust store...")
		cmds = append(cmds, fmt.Sprintf("security remove-trusted-cert -d %s",
			shellQuote(caCertPath)))
	}

	data, err := os.ReadFile(hosts.HostsPath())
	if err == nil {
		var filtered []string
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "# trae-proxy") {
				filtered = append(filtered, line)
			}
		}
		content := strings.Join(filtered, "\n")

		tmpFile, err := os.CreateTemp("", "trae-proxy-hosts-*")
		if err == nil {
			tmpPath := tmpFile.Name()
			tmpFile.WriteString(content)
			tmpFile.Close()
			defer os.Remove(tmpPath)

			fmt.Println("[uninstall] removing hosts entry...")
			cmds = append(cmds,
				fmt.Sprintf("cat %s > %s && rm -f %s && dscacheutil -flushcache && killall -HUP mDNSResponder",
					shellQuote(tmpPath), shellQuote(hosts.HostsPath()), shellQuote(tmpPath)))
		}
	}

	if len(cmds) == 0 {
		return nil
	}
	return privilege.RunPrivileged(strings.Join(cmds, " && "))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func removePrivileged(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return privilege.RunPrivileged("rm -f " + shellQuote(path))
	case "linux":
		cmd := exec.Command("sudo", "rm", "-f", path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	default:
		return exec.Command("cmd", "/C", "del", "/F", path).Run()
	}
}

func findPort443Process() int {
	if runtime.GOOS == "windows" {
		return 0
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:443", "-sTCP:LISTEN").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "trae-proxy") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pid, err := strconv.Atoi(fields[1])
				if err == nil {
					return pid
				}
			}
		}
	}
	return 0
}
