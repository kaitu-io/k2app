package main

import (
	"fmt"
	"io"

	"github.com/kaitu-io/k2/config"
)

// runServer starts k2s as a foreground server process.
// configPath is the -c flag value (empty = auto-search, then default).
func runServer(configPath string) {
	_ = configPath
	panic("not yet implemented")
}

// loadOrDefaultServerConfig loads a ServerConfig from the given path, or searches for one,
// or returns the default config. Returns the config, the resolved path (empty if no file
// was found/used), and any error.
func loadOrDefaultServerConfig(configPath string) (*config.ServerConfig, string, error) {
	return nil, "", fmt.Errorf("not yet implemented")
}

// loadServerConfigFile loads a ServerConfig from a specific path.
// Used by signal path to read pid_file from config.
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
	return fmt.Errorf("not yet implemented")
}
