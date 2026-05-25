package engine

import (
	"log/slog"

	"github.com/kaitu-io/k2/appbypass"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// buildAppBypassRoute consults the YAML preset for cfg.AppBypassRegion and
// merges with cfg.AppBypassCustom to produce a single direct-target
// config.RouteConfig suitable for prepending to client.Routes at Start.
//
// Returns (nil, nil) when:
//   - AppBypassRegion is empty
//   - Client is nil or Client.CacheDir is empty
//   - The preset is absent for the region
//   - The merged MatchConfig has no criteria
//
// Returns (nil, err) only on filesystem read failure. Per-file YAML errors
// are absorbed by appbypass.Load and yield (nil, nil).
func buildAppBypassRoute(cfg *Config) (*config.RouteConfig, error) {
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

	mc := rule.MatchConfig{
		InstallerPackage:  preset.Android.InstallerExact,
		PackageName:       unionStrings(preset.Android.PackageExact, custom.PackageAdds),
		PackageNamePrefix: preset.Android.PackagePrefix,
		ProcessName:       unionStrings(preset.Desktop.ProcessExact, custom.ProcessAdds),
		ProcessNamePrefix: preset.Desktop.ProcessPrefix,
	}
	if !mc.HasCriteria() {
		return nil, nil
	}
	slog.Info("appbypass: route built",
		"region", cfg.AppBypassRegion,
		"installer_count", len(mc.InstallerPackage),
		"package_exact_count", len(mc.PackageName),
		"package_prefix_count", len(mc.PackageNamePrefix),
		"process_exact_count", len(mc.ProcessName),
		"process_prefix_count", len(mc.ProcessNamePrefix),
	)
	return &config.RouteConfig{Via: "direct", Match: mc}, nil
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
