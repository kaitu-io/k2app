package daemon

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

// Reload re-reads the config file at d.ConfigPath and applies dynamic field
// changes. Static field changes (listen, mode, tun, proxy.listen, pid_file) are
// logged as warnings but not applied — they require a daemon restart.
//
// Reload does NOT hold opMu during config file read. opMu is acquired only
// during the apply step to stay consistent with the concurrency model.
func (d *Daemon) Reload() error {
	if d.ConfigPath == "" {
		return fmt.Errorf("no config file to reload")
	}

	// Read and parse config WITHOUT holding any lock — I/O should be lock-free.
	newCfg, err := config.LoadClient(d.ConfigPath)
	if err != nil {
		return fmt.Errorf("reload: load config: %w", err)
	}

	// Validate the newly parsed config.
	if err := config.ValidateClient(newCfg); err != nil {
		return fmt.Errorf("reload: validate config: %w", err)
	}

	// Apply dynamic changes under the field lock (NOT opMu).
	d.mu.Lock()
	serverChanged := d.applyDynamicConfig(newCfg)
	d.mu.Unlock()

	if serverChanged && d.State() == engine.StateConnected {
		slog.Warn("reload: server URL changed while connected — reconnect required", "hint", "send 'up' again or restart")
	}

	slog.Info("reload: config applied", "path", d.ConfigPath)
	return nil
}

// applyDynamicConfig compares newCfg against the current lastConfig and:
//   - Warns about static field changes (listen, mode, tun, proxy.listen, pid_file).
//   - Applies dynamic field changes (server, dns, rule, log.level).
//   - Returns true if the server URL changed.
//
// Caller must hold d.mu when calling this function.
func (d *Daemon) applyDynamicConfig(newCfg *config.ClientConfig) (serverChanged bool) {
	old := d.lastConfig

	// If there is no previous config, treat everything as new.
	if old == nil {
		d.lastConfig = newCfg
		return false
	}

	// --- Static fields: warn if changed, do NOT apply. ---
	if newCfg.Listen != old.Listen {
		slog.Warn("reload: static field changed, restart required",
		"field", "listen", "old", old.Listen, "new", newCfg.Listen)
	}
	if newCfg.Mode != old.Mode {
		slog.Warn("reload: static field changed, restart required",
		"field", "mode", "old", old.Mode, "new", newCfg.Mode)
	}
	if newCfg.Tun.IPv4 != old.Tun.IPv4 {
		slog.Warn("reload: static field changed, restart required",
		"field", "tun.ipv4", "old", old.Tun.IPv4, "new", newCfg.Tun.IPv4)
	}
	if newCfg.Tun.IPv6 != old.Tun.IPv6 {
		slog.Warn("reload: static field changed, restart required",
		"field", "tun.ipv6", "old", old.Tun.IPv6, "new", newCfg.Tun.IPv6)
	}
	if newCfg.Proxy.Listen != old.Proxy.Listen {
		slog.Warn("reload: static field changed, restart required",
		"field", "proxy.listen", "old", old.Proxy.Listen, "new", newCfg.Proxy.Listen)
	}
	if newCfg.PIDFile != old.PIDFile {
		slog.Warn("reload: static field changed, restart required",
		"field", "pid_file", "old", old.PIDFile, "new", newCfg.PIDFile)
	}

	// --- Dynamic fields: apply immediately. ---

	// Server URL.
	if newCfg.Server != old.Server {
		slog.Info("reload: server URL updated", "old", old.Server, "new", newCfg.Server)
		old.Server = newCfg.Server
		serverChanged = true
	}

	// DNS configuration.
	if !stringSlicesEqual(newCfg.DNS.Direct, old.DNS.Direct) {
		slog.Info("reload: dns.direct updated", "servers", newCfg.DNS.Direct)
		old.DNS.Direct = newCfg.DNS.Direct
	}
	if !stringSlicesEqual(newCfg.DNS.Proxy, old.DNS.Proxy) {
		slog.Info("reload: dns.proxy updated", "servers", newCfg.DNS.Proxy)
		old.DNS.Proxy = newCfg.DNS.Proxy
	}

	// Routes: hot-reload of routes is a Phase 2 feature.
	// TODO(routes): compare old/new routes and apply changes dynamically.

	// CacheDir.
	if newCfg.CacheDir != old.CacheDir {
		slog.Info("reload: cache_dir updated")
		old.CacheDir = newCfg.CacheDir
	}

	// Log level.
	if newCfg.Log.Level != old.Log.Level {
		slog.Info("reload: log.level updated", "old", old.Log.Level, "new", newCfg.Log.Level)
		old.Log.Level = newCfg.Log.Level
	}

	return serverChanged
}

// stringSlicesEqual returns true if two string slices have the same elements
// in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// redirectLog replaces the global slog default handler with one that writes to w
// and returns an opaque restore token. Used in tests to capture slog output.
func redirectLog(w *strings.Builder) interface{} {
	prev := slog.Default()
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(w, opts)
	slog.SetDefault(slog.New(handler))
	return prev
}

// restoreLog restores the global slog default from the token returned by redirectLog.
func restoreLog(orig interface{}) {
	if prev, ok := orig.(*slog.Logger); ok {
		slog.SetDefault(prev)
	}
}
