package engine

import (
	"log/slog"

	"github.com/kaitu-io/k2/appbypass"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// buildAppBypassRoute loads the preset for cfg.AppBypassRegion under
// Client.CacheDir and builds up to two platform-specific direct routes:
//
//   - desktop route: ProcessName (exact + custom) + ProcessNamePrefix
//   - android route: PackageName (exact + custom) + PackageNamePrefix + InstallerPackage
//
// Both routes target "direct". Splitting per-platform preserves the rule
// engine's AND-across-groups semantics (mixing desktop + android fields in
// one route would never match on either platform because the absent platform's
// observed fields are always empty).
//
// Returns nil (no routes) when:
//   - AppBypassRegion is empty
//   - Client is nil or Client.CacheDir is empty
//   - No preset exists for the region
//
// Returns shorter slices when only one platform's section has criteria.
// Returns (nil, err) only on filesystem read failure.
func buildAppBypassRoute(cfg *Config) ([]*config.RouteConfig, error) {
	if cfg == nil || cfg.AppBypassRegion == "" {
		return nil, nil
	}
	if cfg.Client == nil || cfg.Client.CacheDir == "" {
		return nil, nil
	}
	ps, err := appbypass.Load(cfg.Client.CacheDir)
	if err != nil {
		return nil, err
	}
	if ps.Empty() {
		return nil, nil
	}
	preset := ps.Get(cfg.AppBypassRegion)
	if preset == nil {
		slog.Info("appbypass: no preset for region", "region", cfg.AppBypassRegion)
		return nil, nil
	}
	custom := cfg.AppBypassCustom

	var routes []*config.RouteConfig

	// Desktop route: process names + prefixes only.
	desktopMC := rule.MatchConfig{
		ProcessName:       unionStrings(preset.Desktop.ProcessExact, custom.ProcessAdds),
		ProcessNamePrefix: preset.Desktop.ProcessPrefix,
	}
	if desktopMC.HasCriteria() {
		routes = append(routes, &config.RouteConfig{Via: "direct", Match: desktopMC})
	}

	// Android route: package names + prefixes + installer only.
	androidMC := rule.MatchConfig{
		InstallerPackage:  preset.Android.InstallerExact,
		PackageName:       unionStrings(preset.Android.PackageExact, custom.PackageAdds),
		PackageNamePrefix: preset.Android.PackagePrefix,
	}
	if androidMC.HasCriteria() {
		routes = append(routes, &config.RouteConfig{Via: "direct", Match: androidMC})
	}

	if len(routes) == 0 {
		return nil, nil
	}
	slog.Info("appbypass: routes built",
		"region", cfg.AppBypassRegion,
		"route_count", len(routes),
		"desktop_process_exact", len(desktopMC.ProcessName),
		"desktop_process_prefix", len(desktopMC.ProcessNamePrefix),
		"android_installer", len(androidMC.InstallerPackage),
		"android_package_exact", len(androidMC.PackageName),
		"android_package_prefix", len(androidMC.PackageNamePrefix),
	)
	return routes, nil
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
