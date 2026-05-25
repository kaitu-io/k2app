package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/kaitu-io/k2/engine"
)

// appBypassState is the Response.Data shape for action "app-bypass-get" /
// "app-bypass-set-*" (all four return the same envelope so the webapp can
// treat any mutation as a full state refresh).
type appBypassState struct {
	FeatureSupported bool                `json:"feature_supported"`
	Region           string              `json:"region"`
	Custom           appBypassCustomJSON `json:"custom"`
}

type appBypassCustomJSON struct {
	ProcessAdds []string `json:"process_adds"`
	PackageAdds []string `json:"package_adds"`
}

// snapshotAppBypassState builds the current state from the daemon's
// in-memory overrides. Used by all four action helpers.
func (d *Daemon) snapshotAppBypassState() appBypassState {
	o := d.currentAppBypass()
	return appBypassState{
		FeatureSupported: isAppBypassPlatformSupported(),
		Region:           o.Region,
		Custom: appBypassCustomJSON{
			ProcessAdds: nilToEmpty(o.ProcessAdds),
			PackageAdds: nilToEmpty(o.PackageAdds),
		},
	}
}

// appBypassGet handles action "app-bypass-get". No params required.
func (d *Daemon) appBypassGet(w http.ResponseWriter, _ map[string]any) {
	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.snapshotAppBypassState()})
}

// isAppBypassPlatformSupported reports whether per-flow process attribution
// works on this platform. iOS NEPacketTunnelProvider can't; gateway (TPROXY)
// lacks per-app identity.
func isAppBypassPlatformSupported() bool {
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		return true
	default:
		return false
	}
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// trimRegion lowercases + trims a region candidate.
func trimRegion(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// applyEngineNow pushes current overrides into the running engine and
// triggers RefreshAppBypass. No-op if engine not running.
func (d *Daemon) applyEngineNow() {
	d.mu.RLock()
	eng := d.engine
	o := d.appBypassOverrides
	d.mu.RUnlock()
	if eng == nil {
		return
	}
	eng.SetAppBypassRegion(o.Region)
	eng.SetAppBypassCustom(engine.AppBypassCustom{
		ProcessAdds: o.ProcessAdds,
		PackageAdds: o.PackageAdds,
	})
	if err := eng.RefreshAppBypass(); err != nil {
		slog.Warn("appbypass: engine refresh failed (state persisted)", "err", err)
	}
}

// applyDelta = (existing ∪ adds) \ removes, deduped, order preserved.
func applyDelta(existing, adds, removes []string) []string {
	rm := make(map[string]struct{}, len(removes))
	for _, s := range removes {
		rm[s] = struct{}{}
	}
	seen := make(map[string]struct{}, len(existing)+len(adds))
	out := make([]string, 0, len(existing)+len(adds))
	for _, s := range existing {
		if _, drop := rm[s]; drop {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range adds {
		if _, drop := rm[s]; drop {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

type appBypassDelta struct {
	Process []string `json:"process,omitempty"`
	Package []string `json:"package,omitempty"`
}

type appBypassCustomReq struct {
	Add    appBypassDelta `json:"add"`
	Remove appBypassDelta `json:"remove"`
}

// appBypassSetCustom handles action "app-bypass-set-custom".
// params decodes into appBypassCustomReq; updates ProcessAdds + PackageAdds
// via applyDelta; persists via persistAppBypass; refreshes engine if running.
func (d *Daemon) appBypassSetCustom(w http.ResponseWriter, params map[string]any) {
	// Re-marshal params into the typed struct (same pattern as handleUp's
	// config decode at api.go:88-97).
	raw, err := json.Marshal(params)
	if err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad params: %v", err)})
		return
	}
	var req appBypassCustomReq
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad request: %v", err)})
		return
	}

	d.mu.Lock()
	o := d.appBypassOverrides
	o.ProcessAdds = applyDelta(o.ProcessAdds, req.Add.Process, req.Remove.Process)
	o.PackageAdds = applyDelta(o.PackageAdds, req.Add.Package, req.Remove.Package)
	d.appBypassOverrides = o
	d.mu.Unlock()

	d.persistAppBypass()
	d.applyEngineNow()

	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.snapshotAppBypassState()})
}

type appBypassRegionReq struct {
	Region string `json:"region"`
}

// appBypassSetRegion handles action "app-bypass-set-region".
// Empty region disables smart bypass; ISO 3166-1 alpha-2 (2 chars) otherwise.
func (d *Daemon) appBypassSetRegion(w http.ResponseWriter, params map[string]any) {
	raw, err := json.Marshal(params)
	if err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad params: %v", err)})
		return
	}
	var req appBypassRegionReq
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad request: %v", err)})
		return
	}
	region := trimRegion(req.Region)
	if region != "" && len(region) != 2 {
		writeJSON(w, Response{Code: 400,
			Message: "region must be empty or ISO 3166-1 alpha-2 (2 chars)"})
		return
	}

	d.mu.Lock()
	d.appBypassOverrides.Region = region
	d.mu.Unlock()

	d.persistAppBypass()
	d.applyEngineNow()

	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.snapshotAppBypassState()})
}

type appBypassPreviewResult struct {
	Matched      []appBypassPreviewEntry `json:"matched"`
	PhaseOneStub bool                    `json:"phase_one_stub"`
}

type appBypassPreviewEntry struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Names      []string `json:"names,omitempty"`
	HitKind    string   `json:"hit_kind,omitempty"`
	HitPattern string   `json:"hit_pattern,omitempty"`
}

// appBypassPreview handles action "app-bypass-preview".
// Phase 1: returns empty stub {matched: [], phase_one_stub: true}.
// Phase 2 will plug AppListProvider here to surface the actual matched apps.
func (d *Daemon) appBypassPreview(w http.ResponseWriter, _ map[string]any) {
	writeJSON(w, Response{
		Code:    0,
		Message: "ok",
		Data: appBypassPreviewResult{
			Matched:      []appBypassPreviewEntry{},
			PhaseOneStub: true,
		},
	})
}
