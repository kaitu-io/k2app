package daemon

import (
	"log/slog"
	"net/http"

	"github.com/kaitu-io/k2/rule"
)

// getPresetsData is the response envelope for action "get-presets".
type getPresetsData struct {
	Regions []presetRegion `json:"regions"`
}

type presetRegion struct {
	ID               string          `json:"id"`
	SetCount         int             `json:"set_count"`
	AppPatternCounts patternCountSet `json:"app_pattern_counts"`
}

type patternCountSet struct {
	Darwin     int `json:"darwin"`
	Windows    int `json:"windows"`
	Android    int `json:"android"`
	Installers int `json:"installers"`
}

// getPresets returns metadata about every *.krs bundle loaded from the
// daemon's rule cache. Used by webapp to populate region-selection
// menus. Stateless — re-reads the cache on every call. Cache I/O is
// O(N krs files) and bounded (currently 15 regions), so no caching layer.
func (d *Daemon) getPresets(w http.ResponseWriter, _ map[string]any) {
	d.mu.RLock()
	cacheDir := d.ruleCacheDir
	d.mu.RUnlock()

	out := getPresetsData{Regions: []presetRegion{}}
	if cacheDir == "" {
		writeJSON(w, Response{Code: 0, Message: "ok", Data: out})
		return
	}

	named, err := rule.LoadNamed(cacheDir)
	if err != nil {
		slog.Warn("get-presets: LoadNamed failed", "err", err)
		writeJSON(w, Response{Code: 0, Message: "ok", Data: out})
		return
	}
	for _, nb := range named {
		r := presetRegion{
			ID:       nb.Region,
			SetCount: len(nb.Bundle.Sets),
		}
		if nb.Bundle.Apps != nil {
			r.AppPatternCounts.Darwin = len(nb.Bundle.Apps.Darwin.Apps)
			r.AppPatternCounts.Windows = len(nb.Bundle.Apps.Windows.Apps)
			r.AppPatternCounts.Android = len(nb.Bundle.Apps.Android.Apps)
			r.AppPatternCounts.Installers = len(nb.Bundle.Apps.Android.Installers)
		}
		out.Regions = append(out.Regions, r)
	}

	writeJSON(w, Response{Code: 0, Message: "ok", Data: out})
}
