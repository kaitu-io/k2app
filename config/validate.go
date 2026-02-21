package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidationError represents a config validation failure for a specific field.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

// validLogLevels contains the accepted log level strings.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// validModes contains the accepted mode strings.
var validModes = map[string]bool{
	"tun":   true,
	"proxy": true,
}

// ValidateClient validates a ClientConfig and returns a *ValidationError if invalid.
// An empty Server field is allowed (may be provided separately via CLI flag).
func ValidateClient(cfg *ClientConfig) error {
	// Validate server URL if provided.
	if cfg.Server != "" {
		if err := validateServerURL(cfg.Server); err != nil {
			return &ValidationError{Field: "server", Message: err.Error()}
		}
	}

	// Validate listen address if provided.
	if cfg.Listen != "" {
		if err := validateHostPort(cfg.Listen); err != nil {
			return &ValidationError{Field: "listen", Message: err.Error()}
		}
	}

	// Validate mode if provided.
	if cfg.Mode != "" {
		if !validModes[cfg.Mode] {
			return &ValidationError{
				Field:   "mode",
				Message: fmt.Sprintf("must be 'tun' or 'proxy', got %q", cfg.Mode),
			}
		}
	}

	// Validate log level if provided.
	if cfg.Log.Level != "" {
		if err := validateLogLevel(cfg.Log.Level); err != nil {
			return &ValidationError{Field: "log.level", Message: err.Error()}
		}
	}

	return nil
}

// ValidateServer validates a ServerConfig and returns a *ValidationError if invalid.
// An empty Listen field is acceptable — setServerDefaults will apply ":443".
func ValidateServer(cfg *ServerConfig) error {
	// Validate listen address if explicitly provided.
	if cfg.Listen != "" {
		if err := validateHostPort(cfg.Listen); err != nil {
			return &ValidationError{Field: "listen", Message: err.Error()}
		}
	}

	// Validate log level if provided.
	if cfg.Log.Level != "" {
		if err := validateLogLevel(cfg.Log.Level); err != nil {
			return &ValidationError{Field: "log.level", Message: err.Error()}
		}
	}

	return nil
}

// validateServerURL checks that s is a parseable URL with a valid scheme and host.
func validateServerURL(s string) error {
	// Reject strings containing control characters or spaces that url.Parse may accept.
	if strings.ContainsAny(s, " \t\n\r") {
		return fmt.Errorf("must be a valid URL")
	}

	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("must have a URL scheme (e.g. k2v5://)")
	}
	if u.Host == "" {
		return fmt.Errorf("must have a host")
	}

	return nil
}

// validateHostPort checks that addr is a valid host:port string.
func validateHostPort(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port, got %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("port is required in %q", addr)
	}
	// Allow empty host (means "all interfaces") — e.g. ":443".
	_ = host
	return nil
}

// validateLogLevel returns an error if level is not one of the accepted values.
func validateLogLevel(level string) error {
	if !validLogLevels[level] {
		return fmt.Errorf("must be one of debug/info/warn/error, got %q", level)
	}
	return nil
}
