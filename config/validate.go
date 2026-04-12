package config

import (
	"fmt"
	"net"
	"net/netip"
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

	// Reject routes that use reserved but not-yet-implemented via schemes
	// (device://, auto://). Defined in config.go IsReservedVia for forward-compat.
	if err := validateReservedViaSchemes(cfg.Routes); err != nil {
		return err
	}

	return nil
}

// validateReservedViaSchemes rejects routes whose Via uses a scheme reserved
// for a future feature (device://, auto://). Returns a ValidationError on the
// first offending route so the caller can surface a clear message.
func validateReservedViaSchemes(routes []RouteConfig) error {
	for i, r := range routes {
		if IsReservedVia(r.Via) {
			return &ValidationError{
				Field: fmt.Sprintf("routes[%d].via", i),
				Message: fmt.Sprintf("scheme %q is reserved for a future feature and not yet implemented (got %q)",
					ViaScheme(r.Via), TruncateVia(r.Via)),
			}
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

// ValidateRouteCIDRConflicts checks for CIDR conflicts between route ip_cidr
// entries and local network interfaces. Returns an error if a route's CIDR
// overlaps with a local interface, which would cause routing ambiguity.
func ValidateRouteCIDRConflicts(routes []RouteConfig) error {
	localNets, err := localCIDRs()
	if err != nil || len(localNets) == 0 {
		return nil // can't check, proceed
	}

	for _, route := range routes {
		if route.Via == "direct" || route.Via == "reject" || route.Via == "" {
			continue
		}
		for _, cidr := range route.Match.IPCIDR {
			prefix, parseErr := netip.ParsePrefix(cidr)
			if parseErr != nil {
				continue
			}
			for _, local := range localNets {
				if prefixesOverlap(prefix, local) {
					return fmt.Errorf("route via %s has ip_cidr %s which overlaps with local interface %s — "+
						"use specific host routes (e.g. /32) to avoid routing conflicts",
						TruncateVia(route.Via), cidr, local)
				}
			}
		}
	}
	return nil
}

func localCIDRs() ([]netip.Prefix, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []netip.Prefix
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() {
				continue
			}
			a, ok := netip.AddrFromSlice(ip4)
			if !ok {
				continue
			}
			ones, _ := ipnet.Mask.Size()
			result = append(result, netip.PrefixFrom(a.Unmap(), ones).Masked())
		}
	}
	return result, nil
}

func prefixesOverlap(a, b netip.Prefix) bool {
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

// TruncateVia redacts credentials from a via URL for safe logging.
func TruncateVia(via string) string {
	if idx := strings.Index(via, "@"); idx != -1 {
		schemeEnd := strings.Index(via, "://")
		if schemeEnd >= 0 {
			return via[:schemeEnd+3] + "***@" + via[idx+1:]
		}
	}
	return via
}
