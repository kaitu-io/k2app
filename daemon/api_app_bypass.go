package daemon

import (
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

// Placeholders for Tasks 17-19. Real bodies replace these in their tasks.
// Registering all 4 action cases now (Step 4 below) locks in the API surface
// so the webapp side can be written against a stable contract.

func (d *Daemon) appBypassSetCustom(w http.ResponseWriter, _ map[string]any) {
	writeJSON(w, Response{Code: 501, Message: "not implemented (Task 17)"})
}
func (d *Daemon) appBypassSetRegion(w http.ResponseWriter, _ map[string]any) {
	writeJSON(w, Response{Code: 501, Message: "not implemented (Task 18)"})
}
func (d *Daemon) appBypassPreview(w http.ResponseWriter, _ map[string]any) {
	writeJSON(w, Response{Code: 501, Message: "not implemented (Task 19)"})
}

// Silence unused-symbol warnings until Tasks 17-19 replace placeholders.
var _ = trimRegion
var _ = applyDelta
var _ = (*Daemon).applyEngineNow
