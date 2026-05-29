package rule

import (
	"log/slog"

	"github.com/kaitu-io/k2-rules/krs"
)

// ClassifyResult is one installed app's region-default routing decision.
// JSON tags match the webapp wire shape so the desktop daemon's classify-apps
// HTTP action and the mobile appext.ClassifyApps gomobile export marshal an
// identical payload.
type ClassifyResult struct {
	ID         string `json:"id"`
	Default    string `json:"default"` // "direct" | "proxy"
	HitKind    string `json:"hit_kind,omitempty"`
	HitPattern string `json:"hit_pattern,omitempty"`
}

// ClassifyInstalled answers "for region X, which of these installed apps default
// to direct vs proxy?". It loads the region's <region>.krs bundle from cacheDir
// and runs krs.MatchInstalled — the SAME codepath the engine consumes at connect
// time via match.region — so UI badges and routing always agree.
//
// This is the single source of truth shared by the desktop daemon's classify-apps
// action (daemon/api_classify_apps.go) and the mobile appext.ClassifyApps export
// (appext/classify.go), so neither platform can drift from the other.
//
// goos selects the platform pattern subset (krs.MatchInstalled: "android" /
// "darwin" / "windows"; anything else matches nothing). Callers pass runtime.GOOS.
//
// Fail-soft: empty region, empty cacheDir, missing/parse-failed bundle, or a
// region whose bundle carries no app patterns → every app defaults to "proxy"
// (traffic stays in the tunnel, the safe default). Never returns an error.
func ClassifyInstalled(cacheDir, region, goos string, apps []krs.MatchableApp) []ClassifyResult {
	out := make([]ClassifyResult, 0, len(apps))

	var patterns *krs.AppPatterns
	if region != "" && cacheDir != "" {
		named, err := LoadNamed(cacheDir)
		if err != nil {
			slog.Warn("classify: LoadNamed failed", "err", err, "cacheDir", cacheDir)
		}
		for _, nb := range named {
			if nb.Region == region && nb.Bundle != nil && nb.Bundle.Apps != nil {
				patterns = nb.Bundle.Apps
				break
			}
		}
	}

	var matched []krs.MatchedEntry
	if patterns != nil {
		matched = krs.MatchInstalled(patterns, apps, goos)
	}
	matchedByID := make(map[string]krs.MatchedEntry, len(matched))
	for _, m := range matched {
		matchedByID[m.ID] = m
	}

	for _, a := range apps {
		res := ClassifyResult{ID: a.ID, Default: "proxy"}
		if m, ok := matchedByID[a.ID]; ok {
			res.Default = "direct"
			res.HitKind = m.HitKind
			res.HitPattern = m.HitPattern
		}
		out = append(out, res)
	}
	return out
}
