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
	"github.com/kaitu-io/k2/server"
)

// runServer starts k2s as a foreground server process.
// configPath is the -c flag value (empty = auto-search, then default).
// This is the main entry point when the binary is invoked with no subcommand.
func runServer(configPath string) {
	// 1. Load config.
	cfg, cfgPathUsed, err := loadOrDefaultServerConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if cfgPathUsed != "" {
		log.Printf("Using config: %s", cfgPathUsed)
	}

	// 2. Validate config.
	if err := config.ValidateServer(cfg); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}

	// 3. Determine PID file path and check for stale/running instance.
	pidPath := serverPIDPath(cfg)
	if pidPath != "" {
		stalePID, err := config.CheckStale(pidPath)
		if err != nil {
			log.Fatalf("PID file error: %v", err)
		}
		if stalePID > 0 {
			log.Printf("Removed stale PID file (was PID %d)", stalePID)
		}
		// Check if server is already running.
		existingPID, err := config.ReadPID(pidPath)
		if err == nil && existingPID > 0 && config.IsProcessAlive(existingPID) {
			log.Fatalf("k2s is already running (PID %d)", existingPID)
		}

		// Write our PID.
		if err := config.WritePID(pidPath); err != nil {
			log.Fatalf("Failed to write PID: %v", err)
		}
		defer config.RemovePID(pidPath)
	}

	// 4. Create and configure server.
	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
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
				log.Println("Received SIGHUP — reload not yet supported for k2s")
			default:
				log.Println("Shutting down...")
				cancel()
				return
			}
		}
	}()

	// 6. Run server (blocks until shutdown).
	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
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
