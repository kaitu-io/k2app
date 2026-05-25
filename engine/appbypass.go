package engine

import (
	"fmt"
	"log/slog"

	"github.com/kaitu-io/k2/appbypass"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// buildAppBypassRoute builds up to two platform-specific direct routes from
// any combination of the AppBypassRegion preset + AppBypassCustom additions:
//
//   - desktop route: ProcessName (preset.exact + custom) + ProcessNamePrefix (preset)
//   - android route: PackageName (preset.exact + custom) + PackageNamePrefix (preset) + InstallerPackage (preset)
//
// Both routes target "direct". Splitting per-platform preserves the rule
// engine's AND-across-groups semantics (mixing desktop + android fields in
// one route would never match on either platform because the absent
// platform's observed fields are always empty).
//
// The preset is optional: when AppBypassRegion is empty (e.g. global-mode
// connect) or the preset YAML is missing, custom entries alone still
// produce direct routes so user-added apps (e.g. "Steam") get bypassed
// regardless of routing mode.
//
// Returns nil (no routes) when neither preset nor custom contributes any
// criteria. Returns (nil, err) only on filesystem read failure.
func buildAppBypassRoute(cfg *Config) ([]*config.RouteConfig, error) {
	if cfg == nil {
		return nil, nil
	}
	custom := cfg.AppBypassCustom

	// Load preset lazily — only when both region is set and a cache dir
	// exists to read from. A missing preset is silently ignored so custom
	// entries can still drive the route.
	var preset *appbypass.Preset
	if cfg.AppBypassRegion != "" && cfg.Client != nil && cfg.Client.CacheDir != "" {
		ps, err := appbypass.Load(cfg.Client.CacheDir)
		if err != nil {
			return nil, err
		}
		if !ps.Empty() {
			preset = ps.Get(cfg.AppBypassRegion)
			if preset == nil {
				slog.Info("appbypass: no preset for region", "region", cfg.AppBypassRegion)
			}
		}
	}

	// Short-circuit when both inputs are empty.
	if preset == nil && len(custom.ProcessAdds) == 0 && len(custom.PackageAdds) == 0 {
		return nil, nil
	}

	var (
		dpExact, dpPrefix              []string
		instExact, pkgExact, pkgPrefix []string
	)
	if preset != nil {
		dpExact, dpPrefix = preset.Desktop.ProcessExact, preset.Desktop.ProcessPrefix
		instExact, pkgExact, pkgPrefix = preset.Android.InstallerExact, preset.Android.PackageExact, preset.Android.PackagePrefix
	}

	var routes []*config.RouteConfig

	desktopMC := rule.MatchConfig{
		ProcessName:       unionStrings(dpExact, custom.ProcessAdds),
		ProcessNamePrefix: dpPrefix,
	}
	if desktopMC.HasCriteria() {
		routes = append(routes, &config.RouteConfig{Via: "direct", Match: desktopMC})
	}

	androidMC := rule.MatchConfig{
		InstallerPackage:  instExact,
		PackageName:       unionStrings(pkgExact, custom.PackageAdds),
		PackageNamePrefix: pkgPrefix,
	}
	if androidMC.HasCriteria() {
		routes = append(routes, &config.RouteConfig{Via: "direct", Match: androidMC})
	}

	if len(routes) == 0 {
		return nil, nil
	}
	slog.Info("appbypass: routes built",
		"region", cfg.AppBypassRegion,
		"preset_loaded", preset != nil,
		"route_count", len(routes),
		"desktop_process_exact", len(desktopMC.ProcessName),
		"desktop_process_prefix", len(desktopMC.ProcessNamePrefix),
		"android_installer", len(androidMC.InstallerPackage),
		"android_package_exact", len(androidMC.PackageName),
		"android_package_prefix", len(androidMC.PackageNamePrefix),
	)
	return routes, nil
}

// SetAppBypassRegion sets the region used by the next route rebuild. Does NOT
// trigger reload — caller pairs with RefreshAppBypass() or relies on next
// connect.
func (e *Engine) SetAppBypassRegion(region string) {
	e.mu.Lock()
	e.appBypassRegion = region
	e.mu.Unlock()
}

// SetAppBypassCustom sets the custom overrides used by the next route rebuild.
// Does NOT trigger reload — caller pairs with RefreshAppBypass().
func (e *Engine) SetAppBypassCustom(c AppBypassCustom) {
	e.mu.Lock()
	e.appBypassCustom = c
	e.mu.Unlock()
}

// RefreshAppBypass rebuilds rule routes (including the appbypass direct
// routes — one per platform) from current stored config and atomically swaps
// via rule.Engine.Reload. Called by the daemon after POST /api/app-bypass/*
// succeeds.
//
// No-op when engine is not running (rule engine nil or client nil) — the
// next connect will pick up the updated config.
func (e *Engine) RefreshAppBypass() error {
	e.mu.Lock()
	re := e.ruleEngine
	client := e.client
	region := e.appBypassRegion
	custom := e.appBypassCustom
	e.mu.Unlock()

	if re == nil || client == nil {
		return nil
	}

	abCfg := Config{
		AppBypassRegion: region,
		AppBypassCustom: custom,
		Client:          client,
	}
	abRoutes, err := buildAppBypassRoute(&abCfg)
	if err != nil {
		return err
	}
	finalRoutes := make([]config.RouteConfig, 0, len(abRoutes)+len(client.Routes))
	for _, r := range abRoutes {
		finalRoutes = append(finalRoutes, *r)
	}
	finalRoutes = append(finalRoutes, client.Routes...)

	bundles, err := rule.Load(client.CacheDir)
	if err != nil {
		return fmt.Errorf("appbypass refresh: load bundles: %w", err)
	}
	idx := rule.Index(bundles)
	outboundMap := buildOutboundMap(client.Routes)
	entries, _, _ := buildRouteEntries(finalRoutes, idx, outboundMap)
	return re.Reload(entries, bundles)
}

// unionStrings returns the deduped union of a and b, preserving first-seen order.
func unionStrings(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range b {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
