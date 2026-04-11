package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/daemon"
	"github.com/zhangyc/trae-proxy/internal/hosts"
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
	return &cobra.Command{
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
				src, err := os.ReadFile("config.example.toml")
				if err != nil {
					cfg := config.DefaultConfig()
					fmt.Printf("[init] generating default config at %s\n", configPath)
					_ = writeDefaultConfig(configPath, cfg)
				} else {
					os.WriteFile(configPath, src, 0644)
					fmt.Printf("[init] config copied to %s\n", configPath)
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
			if err := tlsutil.InstallCA(filepath.Join(caDir, "root-ca.pem")); err != nil {
				fmt.Printf("[init] WARNING: failed to install CA: %v\n", err)
				fmt.Println("[init] you may need to manually trust the CA")
			} else {
				fmt.Println("[init] CA installed successfully")
			}

			fmt.Println("\n[init] done! Run 'trae-proxy start' to begin.")
			return nil
		},
	}
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
	var purge bool
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

			dir, _ := config.ConfigDir()
			caDir := filepath.Join(dir, "ca")

			caCertPath := filepath.Join(caDir, "root-ca.pem")
			if _, err := os.Stat(caCertPath); err == nil {
				fmt.Println("[uninstall] removing CA from system trust store...")
				if err := tlsutil.UninstallCA(caCertPath); err != nil {
					fmt.Printf("[uninstall] WARNING: %v\n", err)
				}
			}

			fmt.Println("[uninstall] removing hosts entry...")
			hosts.Remove()

			// Remove the binary itself.
			exePath, err := os.Executable()
			if err == nil {
				exePath, err = filepath.EvalSymlinks(exePath)
			}
			if err == nil {
				fmt.Printf("[uninstall] removing binary %s...\n", exePath)
				if err := os.Remove(exePath); err != nil {
					fmt.Printf("[uninstall] WARNING: could not remove binary: %v\n", err)
				}
			}

			if purge {
				fmt.Printf("[uninstall] removing config directory %s...\n", dir)
				os.RemoveAll(dir)
			}

			fmt.Println("[uninstall] done")
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "Also remove config directory")
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

			fmt.Printf("[update] updated from %s to %s\n", version, tag)
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

upstream = "%s"

# Upstream protocol: "anthropic" (default) performs OpenAI → Anthropic Messages
# conversion. "openai" directly forwards OpenAI Chat Completions — use this when
# upstream is OpenAI-compatible (openrouter.ai, LM Studio, Ollama, most relays).
upstream_protocol = "anthropic"

listen = "%s"
hijack = "%s"

# When true, GET /v1/models forwards to the real hijack domain (bypassing /etc/hosts)
# instead of returning the fake list from [models] below.
# real_models = false

[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4-6" = "claude-sonnet-4-6"
"anthropic/claude-sonnet-4.5" = "claude-sonnet-4-5-20251001"
"anthropic/claude-sonnet-4-5" = "claude-sonnet-4-5-20251001"
"anthropic/claude-haiku-4.5" = "claude-haiku-4-5-20251001"
"anthropic/claude-haiku-4-5" = "claude-haiku-4-5-20251001"
"anthropic/claude-opus-4.6" = "claude-opus-4-6"
"anthropic/claude-opus-4-6" = "claude-opus-4-6"
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

	fmt.Printf("[start] adding hosts entry for %s...\n", cfg.Hijack)
	if err := hosts.Add(cfg.Hijack); err != nil {
		return fmt.Errorf("add hosts: %w", err)
	}
	defer hosts.Remove() // ensure cleanup on any exit (panic, fatal, etc.)

	tlsCfg, err := tlsutil.LoadServerTLSConfig(caDir)
	if err != nil {
		return fmt.Errorf("load TLS config: %w", err)
	}

	srv := proxy.NewServer(cfg)
	srv.TLSConfig = tlsCfg

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[trae-proxy] shutting down...")
		hosts.Remove()
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
