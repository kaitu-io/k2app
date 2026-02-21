package daemon

import (
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/kaitu-io/k2/config"
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

	if serverChanged && d.State() == StateConnected {
		log.Printf("reload: server URL changed while connected — reconnect required (send 'up' again or restart)")
	}

	log.Printf("reload: config applied from %s", d.ConfigPath)
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
		log.Printf("reload: WARNING: static field 'listen' changed (%q → %q); restart required to take effect",
			old.Listen, newCfg.Listen)
	}
	if newCfg.Mode != old.Mode {
		log.Printf("reload: WARNING: static field 'mode' changed (%q → %q); restart required to take effect",
			old.Mode, newCfg.Mode)
	}
	if newCfg.Tun.IPv4 != old.Tun.IPv4 {
		log.Printf("reload: WARNING: static field 'tun.ipv4' changed (%q → %q); restart required to take effect",
			old.Tun.IPv4, newCfg.Tun.IPv4)
	}
	if newCfg.Tun.IPv6 != old.Tun.IPv6 {
		log.Printf("reload: WARNING: static field 'tun.ipv6' changed (%q → %q); restart required to take effect",
			old.Tun.IPv6, newCfg.Tun.IPv6)
	}
	if newCfg.Proxy.Listen != old.Proxy.Listen {
		log.Printf("reload: WARNING: static field 'proxy.listen' changed (%q → %q); restart required to take effect",
			old.Proxy.Listen, newCfg.Proxy.Listen)
	}
	if newCfg.PIDFile != old.PIDFile {
		log.Printf("reload: WARNING: static field 'pid_file' changed (%q → %q); restart required to take effect",
			old.PIDFile, newCfg.PIDFile)
	}

	// --- Dynamic fields: apply immediately. ---

	// Server URL.
	if newCfg.Server != old.Server {
		log.Printf("reload: server URL updated (%q → %q)", old.Server, newCfg.Server)
		old.Server = newCfg.Server
		serverChanged = true
	}

	// DNS configuration.
	if !stringSlicesEqual(newCfg.DNS.Direct, old.DNS.Direct) {
		log.Printf("reload: dns.direct updated: %v", newCfg.DNS.Direct)
		old.DNS.Direct = newCfg.DNS.Direct
	}
	if !stringSlicesEqual(newCfg.DNS.Proxy, old.DNS.Proxy) {
		log.Printf("reload: dns.proxy updated: %v", newCfg.DNS.Proxy)
		old.DNS.Proxy = newCfg.DNS.Proxy
	}

	// Rule configuration.
	if newCfg.Rule.RuleURL != old.Rule.RuleURL {
		log.Printf("reload: rule.rule_url updated")
		old.Rule.RuleURL = newCfg.Rule.RuleURL
	}
	if newCfg.Rule.GeoIPURL != old.Rule.GeoIPURL {
		log.Printf("reload: rule.geoip_url updated")
		old.Rule.GeoIPURL = newCfg.Rule.GeoIPURL
	}
	if newCfg.Rule.PornURL != old.Rule.PornURL {
		log.Printf("reload: rule.porn_url updated")
		old.Rule.PornURL = newCfg.Rule.PornURL
	}
	if newCfg.Rule.CacheDir != old.Rule.CacheDir {
		log.Printf("reload: rule.cache_dir updated")
		old.Rule.CacheDir = newCfg.Rule.CacheDir
	}
	if newCfg.Rule.Global != old.Rule.Global {
		log.Printf("reload: rule.global updated: %v", newCfg.Rule.Global)
		old.Rule.Global = newCfg.Rule.Global
	}

	// Log level.
	if newCfg.Log.Level != old.Log.Level {
		log.Printf("reload: log.level updated (%q → %q)", old.Log.Level, newCfg.Log.Level)
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

// redirectLog replaces the standard log output with w and returns an opaque
// restore token. Used in tests to capture log output.
func redirectLog(w *strings.Builder) interface{} {
	type savedLog struct {
		flags  int
		writer io.Writer
	}
	orig := &savedLog{
		flags:  log.Flags(),
		writer: log.Writer(),
	}
	log.SetOutput(w)
	log.SetFlags(0)
	return orig
}

// restoreLog restores the standard logger from the token returned by redirectLog.
func restoreLog(orig interface{}) {
	type savedLog struct {
		flags  int
		writer io.Writer
	}
	if s, ok := orig.(*savedLog); ok {
		log.SetOutput(s.writer)
		log.SetFlags(s.flags)
	}
}
