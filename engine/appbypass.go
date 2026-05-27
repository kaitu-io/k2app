package engine

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// buildAppBypassRoute builds up to two platform-specific direct routes from
// the AppBypassRegion preset (sourced from <region>.krs's bundle.Apps) plus
// AppBypassCustom additions:
//
//   - desktop route: ProcessName + ProcessNamePrefix (from krs Darwin/Windows.Apps via SplitGlobs + custom adds)
//   - android route: InstallerPackage + PackageName + PackageNamePrefix (from krs Android via SplitGlobs)
//
// Splitting per-platform preserves the rule engine's AND-across-groups
// semantics (mixing desktop + android fields in one route would never match
// on either platform because the absent platform's observed fields are
// always empty).
//
// Returns nil (no routes) when neither preset nor custom contributes any
// criteria. Returns (nil, err) only on filesystem read failure.
//
// SplitGlobs is transitional — Plan B retires the krs→MatchConfig translation
// when engine.Start consumes bundle.Apps directly via match.region.
func buildAppBypassRoute(cfg *Config) ([]*config.RouteConfig, error) {
	if cfg == nil {
		return nil, nil
	}
	custom := cfg.AppBypassCustom

	var (
		dpExact, dpPrefix              []string
		instExact, pkgExact, pkgPrefix []string
		presetLoaded                   bool
	)
	if cfg.AppBypassRegion != "" && cfg.Client != nil && cfg.Client.CacheDir != "" {
		named, err := rule.LoadNamed(cfg.Client.CacheDir)
		if err != nil {
			return nil, err
		}
		for _, nb := range named {
			if nb.Region != cfg.AppBypassRegion || nb.Bundle == nil || nb.Bundle.Apps == nil {
				continue
			}
			apps := nb.Bundle.Apps
			switch runtime.GOOS {
			case "darwin":
				// rule.MatchConfig.ProcessNamePrefix is matched case-insensitively
				// (rule engine pre-lowercases the entries + lowercases the observed
				// process name). krs Darwin.Apps is stored case-sensitive, so we
				// lowercase here to align with engine semantics. (Windows.Apps is
				// already lowercased at krs compile time — apps.go normalizeStrings.)
				dpExact, dpPrefix = rule.SplitGlobs(lowerSlice(apps.Darwin.Apps))
			case "windows":
				dpExact, dpPrefix = rule.SplitGlobs(apps.Windows.Apps)
			}
			instExact = append([]string(nil), apps.Android.Installers...)
			pkgExact, pkgPrefix = rule.SplitGlobs(apps.Android.Apps)
			presetLoaded = true
			break
		}
		if !presetLoaded {
			slog.Info("appbypass: no preset for region", "region", cfg.AppBypassRegion)
		}
	}

	if !presetLoaded && len(custom.ProcessAdds) == 0 && len(custom.PackageAdds) == 0 {
		return nil, nil
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
		"preset_loaded", presetLoaded,
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

	named, err := rule.LoadNamed(client.CacheDir)
	if err != nil {
		return fmt.Errorf("appbypass refresh: load bundles: %w", err)
	}
	bundles := make([]*krs.Bundle, 0, len(named))
	for _, nb := range named {
		bundles = append(bundles, nb.Bundle)
	}
	idx := krs.Index(bundles)
	outboundMap := buildOutboundMap(client.Routes)
	entries, _, _ := buildRouteEntries(finalRoutes, idx, outboundMap)
	return re.ReloadNamed(entries, named)
}

// lowerSlice returns a fresh slice with each entry lowercased. Empty input → nil.
func lowerSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
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
