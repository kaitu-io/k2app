package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

// classifyAppsRequest mirrors the wire shape. installer_package_name and
// process_names feed krs.MatchInstalled directly — webapp must populate
// them per Plan C platform contract (process_names case-preserved on
// Darwin; krs.MatchDarwinProcess is case-sensitive).
type classifyAppsRequest struct {
	Region    string                  `json:"region"`
	Installed []classifyInstalledItem `json:"installed"`
}

type classifyInstalledItem struct {
	ID                   string   `json:"id"`
	Label                string   `json:"label"`
	InstallerPackageName string   `json:"installer_package_name"`
	ProcessNames         []string `json:"process_names"`
}

// classifyAppsData is the response envelope.
type classifyAppsData struct {
	Classifications []classifyResult `json:"classifications"`
}

type classifyResult struct {
	ID         string `json:"id"`
	Default    string `json:"default"`               // "direct" | "proxy"
	HitKind    string `json:"hit_kind,omitempty"`    // "installer" | "app"
	HitPattern string `json:"hit_pattern,omitempty"` // matched glob
}

// classifyApps is the stateless classifier for "given region X, which of
// these installed apps default to direct vs proxy?". Identical match
// codepath as the engine consumes at runtime (krs.MatchInstalled over the
// same bundle.Apps), so UI badges and routing agree.
//
// Plan B replaces app-bypass-preview. Unknown region or missing bundle =
// every app defaults to proxy (fail-soft, no error).
func (d *Daemon) classifyApps(w http.ResponseWriter, params map[string]any) {
	raw, err := json.Marshal(params)
	if err != nil {
		writeJSON(w, Response{Code: 400, Message: "bad params: " + err.Error()})
		return
	}
	var req classifyAppsRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, Response{Code: 400, Message: "bad params: " + err.Error()})
		return
	}

	d.mu.RLock()
	cacheDir := d.ruleCacheDir
	d.mu.RUnlock()

	out := classifyAppsData{
		Classifications: make([]classifyResult, 0, len(req.Installed)),
	}

	var apps *krs.AppPatterns
	if req.Region != "" && cacheDir != "" {
		named, err := rule.LoadNamed(cacheDir)
		if err != nil {
			slog.Warn("classify-apps: LoadNamed failed", "err", err)
		}
		for _, nb := range named {
			if nb.Region == req.Region && nb.Bundle != nil && nb.Bundle.Apps != nil {
				apps = nb.Bundle.Apps
				break
			}
		}
	}

	matchInputs := make([]krs.MatchableApp, 0, len(req.Installed))
	for _, item := range req.Installed {
		matchInputs = append(matchInputs, krs.MatchableApp{
			ID:                   item.ID,
			Label:                item.Label,
			InstallerPackageName: item.InstallerPackageName,
			ProcessNames:         item.ProcessNames,
		})
	}

	var matched []krs.MatchedEntry
	if apps != nil {
		matched = krs.MatchInstalled(apps, matchInputs, runtime.GOOS)
	}
	matchedByID := make(map[string]krs.MatchedEntry, len(matched))
	for _, m := range matched {
		matchedByID[m.ID] = m
	}

	for _, item := range req.Installed {
		res := classifyResult{ID: item.ID, Default: "proxy"}
		if m, ok := matchedByID[item.ID]; ok {
			res.Default = "direct"
			res.HitKind = m.HitKind
			res.HitPattern = m.HitPattern
		}
		out.Classifications = append(out.Classifications, res)
	}

	writeJSON(w, Response{Code: 0, Message: "ok", Data: out})
}
