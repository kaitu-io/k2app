package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
	"github.com/kaitu-io/k2/wintun"
)

// runDaemon starts k2 as a foreground daemon process.
// configPath is the -c flag value (empty = auto-search).
// This is the main entry point when the binary is invoked with no subcommand.
func runDaemon(configPath string) {
	// 1. Load config.
	cfg, cfgPathUsed, err := loadOrDefaultConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if cfgPathUsed != "" {
		log.Printf("Using config: %s", cfgPathUsed)
	}

	// 2. Validate config.
	if err := config.ValidateClient(cfg); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}

	// 3. Determine PID file path and check for stale/running instance.
	pidPath := daemonPIDPath(cfg)
	if pidPath != "" {
		stalePID, err := config.CheckStale(pidPath)
		if err != nil {
			log.Fatalf("PID file error: %v", err)
		}
		if stalePID > 0 {
			log.Printf("Removed stale PID file (was PID %d)", stalePID)
		}
		// Check if daemon is already running.
		existingPID, err := config.ReadPID(pidPath)
		if err == nil && existingPID > 0 && config.IsProcessAlive(existingPID) {
			log.Fatalf("k2 is already running (PID %d)", existingPID)
		}

		// Write our PID.
		if err := config.WritePID(pidPath); err != nil {
			log.Fatalf("Failed to write PID: %v", err)
		}
		defer config.RemovePID(pidPath)
	}

	// 4. Platform setup.
	if _, err := wintun.EnsureExtracted(); err != nil {
		log.Printf("wintun: %v", err)
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
				log.Println("Received SIGHUP, reloading config...")
				if err := d.Reload(); err != nil {
					log.Printf("Reload failed: %v", err)
				} else {
					log.Println("Config reloaded")
				}
			default:
				log.Println("Shutting down...")
				cancel()
				return
			}
		}
	}()

	// 8. Determine listen address.
	listenAddr := cfg.Listen

	// 9. Run daemon (blocks until shutdown).
	if err := d.Run(ctx, listenAddr); err != nil && err != context.Canceled {
		log.Fatalf("Daemon error: %v", err)
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
