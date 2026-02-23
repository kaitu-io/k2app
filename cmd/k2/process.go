package main

import (
	"context"
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
	// Phase 1: Set up logging with defaults before config is loaded so that any
	// early errors are captured with structured logging.
	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2")
	if err != nil {
		slog.Error("Failed to set up default logging", "err", err)
		os.Exit(1)
	}
	defer cleanup()

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

	// 3. Determine PID file path and check for stale/running instance.
	pidPath := daemonPIDPath(cfg)
	if pidPath != "" {
		stalePID, err := config.CheckStale(pidPath)
		if err != nil {
			slog.Error("PID file error", "err", err)
			os.Exit(1)
		}
		if stalePID > 0 {
			slog.Info("Removed stale PID file", "pid", stalePID)
		}
		// Check if daemon is already running.
		existingPID, err := config.ReadPID(pidPath)
		if err == nil && existingPID > 0 && config.IsProcessAlive(existingPID) {
			slog.Error("k2 is already running", "pid", existingPID)
			os.Exit(1)
		}

		// Write our PID.
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
