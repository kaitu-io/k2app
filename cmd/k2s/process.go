package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/server"
)

// runServer starts k2s as a foreground server process.
// configPath is the -c flag value (empty = auto-search, then default).
// This is the main entry point when the binary is invoked with no subcommand.
func runServer(configPath string) {
	// Phase 1: Set up logging with defaults before config is loaded so that any
	// early errors are captured with structured logging.
	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2s")
	if err != nil {
		slog.Error("Failed to set up default logging", "err", err)
		os.Exit(1)
	}
	defer cleanup()

	slog.Info("========== k2s server starting ==========",
		"version", version, "commit", commit, "pid", os.Getpid())

	// Beta build activates beta channel unconditionally (mirrors desktop behavior).
	if strings.Contains(version, "-beta") && readChannel() != "beta" {
		_ = saveChannel("beta")
		slog.Info("Beta build detected, activated beta channel")
	}

	go checkVersionAsync()

	// 1. Load config.
	cfg, cfgPathUsed, err := loadOrDefaultServerConfig(configPath)
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		os.Exit(1)
	}
	if cfgPathUsed != "" {
		slog.Info("Using config", "path", cfgPathUsed)
	}

	// K2_LOG_LEVEL env var overrides config file's log level.
	// This allows Docker Compose to control log level via .env without
	// modifying the sidecar-generated config file.
	if envLevel := os.Getenv("K2_LOG_LEVEL"); envLevel != "" {
		cfg.Log.Level = envLevel
	}

	// Phase 2: Re-configure logging with the settings from the loaded config.
	cleanup2, err := config.SetupLogging(cfg.Log, "k2s")
	if err != nil {
		slog.Error("Failed to set up logging from config", "err", err)
		os.Exit(1)
	}
	defer cleanup2()

	// 2. Validate config.
	if err := config.ValidateServer(cfg); err != nil {
		slog.Error("Config validation failed", "err", err)
		os.Exit(1)
	}

	// 3. Write PID file (nginx-style: unconditional overwrite, no stale/alive checks).
	// Duplicate instance prevention relies on port binding failure, not PID files.
	// PID file exists solely for "k2s -s stop|reload" signal dispatch.
	pidPath := serverPIDPath(cfg)
	if pidPath != "" {
		if err := config.WritePID(pidPath); err != nil {
			slog.Error("Failed to write PID", "err", err)
			os.Exit(1)
		}
		defer config.RemovePID(pidPath)
	}

	// 4. Create and configure server.
	srv, err := server.New(cfg)
	if err != nil {
		slog.Error("Failed to create server", "err", err)
		os.Exit(1)
	}

	// 5. Set up context + signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				// Server reload is complex (cert rotation etc.) — log and ignore for now.
				slog.Info("Received SIGHUP — reload not yet supported for k2s")
			default:
				slog.Info("Shutting down...")
				cancel()
				return
			}
		}
	}()

	// 6. Run server (blocks until shutdown).
	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("Server error", "err", err)
		os.Exit(1)
	}
}

// loadOrDefaultServerConfig loads a ServerConfig from the given path, or searches for one,
// or returns the default config. Returns the config, the resolved path (empty if no file
// was found/used), and any error.
func loadOrDefaultServerConfig(configPath string) (*config.ServerConfig, string, error) {
	if configPath != "" {
		cfg, err := config.LoadServer(configPath)
		if err != nil {
			return nil, "", fmt.Errorf("load config %s: %w", configPath, err)
		}
		return cfg, configPath, nil
	}

	// Search standard locations.
	found, err := config.SearchServerConfig()
	if err != nil {
		return nil, "", fmt.Errorf("search config: %w", err)
	}
	if found != "" {
		cfg, err := config.LoadServer(found)
		if err != nil {
			return nil, "", fmt.Errorf("load config %s: %w", found, err)
		}
		return cfg, found, nil
	}

	// No config file — use defaults (zero-config mode).
	cfg := config.DefaultServerConfig()
	return cfg, "", nil
}

// loadServerConfigFile loads a ServerConfig from a specific path.
// Used by the signal path to read the pid_file field from config.
func loadServerConfigFile(path string) (*config.ServerConfig, error) {
	return config.LoadServer(path)
}

// serverPIDPath returns the PID file path for the server.
// If cfg has a PIDFile set, use it; otherwise use the platform default.
func serverPIDPath(cfg *config.ServerConfig) string {
	if cfg != nil && cfg.PIDFile != "" {
		return cfg.PIDFile
	}
	return defaultPIDPath()
}

// defaultPIDPath returns the platform-default PID file path for k2s.
func defaultPIDPath() string {
	return config.DefaultPIDPath("k2s")
}

// runTestConfig loads and validates the server config at cfgPath, writing results to w.
// Returns nil if config is valid, error otherwise.
func runTestConfig(cfgPath string, w io.Writer) error {
	// If no path given, search standard locations.
	if cfgPath == "" {
		found, err := config.SearchServerConfig()
		if err != nil {
			return fmt.Errorf("search config: %w", err)
		}
		if found == "" {
			return fmt.Errorf("no config file found")
		}
		cfgPath = found
	}

	cfg, err := config.LoadServer(cfgPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", cfgPath, err)
	}

	if err := config.ValidateServer(cfg); err != nil {
		return fmt.Errorf("validate config %s: %w", cfgPath, err)
	}

	fmt.Fprintf(w, "Configuration file %s is OK\n", cfgPath)
	return nil
}
