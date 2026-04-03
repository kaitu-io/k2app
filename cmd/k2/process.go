package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
	"github.com/kaitu-io/k2/daemon/wintun"
)

// runDaemon starts k2 as a foreground daemon process.
// configPath is the -c flag value (empty = auto-search).
// This is the main entry point when the binary is invoked with no subcommand.
func runDaemon(configPath string) {
	defer daemon.HandlePanic()

	// Phase 1: Set up logging with defaults before config is loaded so that any
	// early errors are captured with structured logging.
	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2")
	if err != nil {
		slog.Error("Failed to set up default logging", "err", err)
		os.Exit(1)
	}
	defer cleanup()

	slog.Info("========== k2 daemon starting ==========",
		"version", version, "commit", commit, "pid", os.Getpid())

	// 1. Load config.
	cfg, cfgPathUsed, err := loadOrDefaultConfig(configPath)
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		os.Exit(1)
	}
	if cfgPathUsed != "" {
		slog.Info("Using config", "path", cfgPathUsed)
	}

	// Phase 2: Re-configure logging with the settings from the loaded config.
	cleanup2, err := config.SetupLogging(cfg.Log, "k2")
	if err != nil {
		slog.Error("Failed to set up logging from config", "err", err)
		os.Exit(1)
	}
	defer cleanup2()

	// 2. Validate config.
	if err := config.ValidateClient(cfg); err != nil {
		slog.Error("Config validation failed", "err", err)
		os.Exit(1)
	}

	// 3. Write PID file (nginx-style: unconditional overwrite, no stale/alive checks).
	// Duplicate instance prevention relies on port binding failure, not PID files.
	// PID file exists solely for "k2 -s stop|reload" signal dispatch.
	pidPath := daemonPIDPath(cfg)
	if pidPath != "" {
		if err := config.WritePID(pidPath); err != nil {
			slog.Error("Failed to write PID", "err", err)
			os.Exit(1)
		}
		defer config.RemovePID(pidPath)
	}

	// 4. Platform setup.
	if _, err := wintun.EnsureExtracted(); err != nil {
		slog.Info("wintun", "err", err)
	}
	daemon.ApplyResourceLimits()

	// 5. Version check in background.
	go checkVersionAsync()

	// 6. Create and configure daemon.
	d := daemon.New()
	d.Version = version
	d.Commit = commit
	if cfgPathUsed != "" {
		d.ConfigPath = cfgPathUsed
	}

	// 7. Set up context + signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				slog.Info("Received SIGHUP, reloading config...")
				if err := d.Reload(); err != nil {
					slog.Error("Reload failed", "err", err)
				} else {
					slog.Info("Config reloaded")
				}
			default:
				slog.Info("Shutting down...")
				cancel()
				return
			}
		}
	}()

	// 8. Determine listen address.
	listenAddr := cfg.Listen

	// 9. Run daemon (blocks until shutdown).
	if err := d.Run(ctx, listenAddr); err != nil && err != context.Canceled {
		slog.Error("Daemon error", "err", err)
		os.Exit(1)
	}
}

// loadOrDefaultConfig loads a ClientConfig from the given path, or searches for one.
// Returns the config, the resolved path (empty if no file was found/used), and any error.
func loadOrDefaultConfig(configPath string) (*config.ClientConfig, string, error) {
	if configPath != "" {
		cfg, err := config.LoadClient(configPath)
		if err != nil {
			return nil, "", fmt.Errorf("load config %s: %w", configPath, err)
		}
		return cfg, configPath, nil
	}

	// Search standard locations.
	found, err := config.SearchClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("search config: %w", err)
	}
	if found != "" {
		cfg, err := config.LoadClient(found)
		if err != nil {
			return nil, "", fmt.Errorf("load config %s: %w", found, err)
		}
		return cfg, found, nil
	}

	// No config file — use defaults (daemon mode, no auto-connect).
	cfg := &config.ClientConfig{}
	config.SetDefaults(cfg)
	return cfg, "", nil
}

// loadClientConfigFile loads a ClientConfig from a specific path.
// Used by signal path to read pid_file from config.
func loadClientConfigFile(path string) (*config.ClientConfig, error) {
	return config.LoadClient(path)
}

// daemonPIDPath returns the PID file path for the daemon.
// If cfg has a PIDFile set, use it; otherwise use the platform default.
func daemonPIDPath(cfg *config.ClientConfig) string {
	if cfg != nil && cfg.PIDFile != "" {
		return cfg.PIDFile
	}
	return defaultPIDPath()
}

// defaultPIDPath returns the platform-default PID file path.
func defaultPIDPath() string {
	return config.DefaultPIDPath("k2")
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("k2 run", flag.ExitOnError)
	configPath := fs.String("c", "", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Start daemon (system service entry point).

Usage: k2 run [-c config]

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	startDaemon(*configPath)
}

func cmdConfig(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(os.Stderr, "Usage: k2 config <test|show>\n\n  test  Test configuration\n  show  Print example config\n")
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	switch args[0] {
	case "test":
		fs := flag.NewFlagSet("k2 config test", flag.ExitOnError)
		cfgPath := fs.String("c", "", "config file path")
		fs.Parse(args[1:])
		var buf bytes.Buffer
		if err := runTestConfig(resolveConfigPath(*cfgPath), &buf); err != nil {
			out.fail("INVALID_CONFIG", err.Error(), "")
			return 2
		}
		out.printResult(buf.String())
		out.ok(map[string]string{"status": "ok"})
		return 0
	case "show":
		fmt.Print(demoConfig)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		return 1
	}
}

// runTestConfig loads and validates the config at cfgPath, writing results to w.
// Returns nil if config is valid, error otherwise.
func runTestConfig(cfgPath string, w io.Writer) error {
	// If no path given, search standard locations.
	if cfgPath == "" {
		found, err := config.SearchClientConfig()
		if err != nil {
			return fmt.Errorf("search config: %w", err)
		}
		if found == "" {
			return fmt.Errorf("no config file found")
		}
		cfgPath = found
	}

	cfg, err := config.LoadClient(cfgPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", cfgPath, err)
	}

	if err := config.ValidateClient(cfg); err != nil {
		return fmt.Errorf("validate config %s: %w", cfgPath, err)
	}

	fmt.Fprintf(w, "Configuration file %s is OK\n", cfgPath)
	return nil
}
