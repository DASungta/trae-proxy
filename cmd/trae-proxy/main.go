package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/daemon"
	"github.com/zhangyc/trae-proxy/internal/hosts"
	"github.com/zhangyc/trae-proxy/internal/proxy"
	tlsutil "github.com/zhangyc/trae-proxy/internal/tls"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "trae-proxy",
		Short:   "HTTPS proxy for Anthropic API translation",
		Version: version,
	}

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(uninstallCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate CA, install trust, create default config",
		RunE: func(cmd *cobra.Command, args []string) error {
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
	var (
		daemonMode bool
		upstream   string
		configPath string
		listen     string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the proxy (adds hosts entry, starts HTTPS server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemonMode {
				return daemon.Daemonize()
			}

			dir, _ := config.ConfigDir()
			caDir := filepath.Join(dir, "ca")
			if _, err := os.Stat(filepath.Join(caDir, "root-ca.pem")); os.IsNotExist(err) {
				return fmt.Errorf("not initialized. Run 'trae-proxy init' first")
			}

			if configPath == "" {
				configPath = filepath.Join(dir, "config.toml")
			}

			overrides := map[string]string{}
			if upstream != "" {
				overrides["upstream"] = upstream
			}
			if listen != "" {
				overrides["listen"] = listen
			}

			cfg, err := config.Load(configPath, overrides)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			fmt.Printf("[start] adding hosts entry for %s...\n", cfg.Hijack)
			if err := hosts.Add(cfg.Hijack); err != nil {
				return fmt.Errorf("add hosts: %w", err)
			}

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
		},
	}

	cmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "Run as background daemon")
	cmd.Flags().StringVar(&upstream, "upstream", "", "Override upstream URL")
	cmd.Flags().StringVar(&configPath, "config", "", "Config file path")
	cmd.Flags().StringVar(&listen, "listen", "", "Override listen address")

	return cmd
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon and remove hosts entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			if pid, running := daemon.IsRunning(); running {
				fmt.Printf("[stop] stopping daemon (pid %d)...\n", pid)
				if err := daemon.StopDaemon(); err != nil {
					return err
				}
				fmt.Println("[stop] daemon stopped")
			} else {
				fmt.Println("[stop] daemon not running")
			}

			fmt.Println("[stop] removing hosts entry...")
			hosts.Remove()
			fmt.Println("[stop] done")
			return nil
		},
	}
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

			has, _ := hosts.HasEntry(cfg.Hijack)
			if has {
				fmt.Printf("[hosts] ✓ %s → 127.0.0.1\n", cfg.Hijack)
			} else {
				fmt.Printf("[hosts] ✗ %s not redirected\n", cfg.Hijack)
			}

			if pid, running := daemon.IsRunning(); running {
				fmt.Printf("[daemon] ✓ running (pid %d)\n", pid)
			} else if pid > 0 {
				fmt.Printf("[daemon] ✗ dead (stale pid %d)\n", pid)
			} else {
				fmt.Println("[daemon] ✗ not running")
			}

			fmt.Println()
			fmt.Printf("Upstream: %s\n", cfg.Upstream)
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

func writeDefaultConfig(path string, cfg *config.Config) error {
	content := fmt.Sprintf(`# trae-proxy configuration

upstream = "%s"
listen = "%s"
hijack = "%s"

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
