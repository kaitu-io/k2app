package daemon

import (
	"encoding/json"
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
	Classifications []rule.ClassifyResult `json:"classifications"`
}

// classifyApps is the stateless classifier for "given region X, which of
// these installed apps default to direct vs proxy?". The actual matching lives
// in rule.ClassifyInstalled (shared with the mobile appext.ClassifyApps export)
// so UI badges and engine routing agree across every platform.
//
// Unknown region or missing bundle = every app defaults to proxy
// (fail-soft, no error).
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

	apps := make([]krs.MatchableApp, 0, len(req.Installed))
	for _, item := range req.Installed {
		apps = append(apps, krs.MatchableApp{
			ID:                   item.ID,
			Label:                item.Label,
			InstallerPackageName: item.InstallerPackageName,
			ProcessNames:         item.ProcessNames,
		})
	}

	results := rule.ClassifyInstalled(cacheDir, req.Region, runtime.GOOS, apps)
	writeJSON(w, Response{Code: 0, Message: "ok", Data: classifyAppsData{Classifications: results}})
}
