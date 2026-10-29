package appext

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

// classifyAppInput mirrors the webapp wire shape forwarded through the Capacitor
// bridge (capacitor-k2.ts → K2Plugin → VpnServiceBridge). Field names match the
// daemon's classifyInstalledItem so desktop and mobile speak one contract.
type classifyAppInput struct {
	ID                   string   `json:"id"`
	Label                string   `json:"label"`
	InstallerPackageName string   `json:"installer_package_name"`
	ProcessNames         []string `json:"process_names"`
}

type classifyAppsOutput struct {
	Classifications []rule.ClassifyResult `json:"classifications"`
}

// ClassifyApps is the stateless mobile app-region classifier. It mirrors the
// desktop daemon's classify-apps action via the shared rule.ClassifyInstalled,
// so the App Bypass page's "默认直连 / 默认代理" badges agree with engine routing.
//
// The native layer injects cacheDir — the same platform cache base passed to
// Engine.Start; rule bundles live in cacheDir+"/rules" (the layout PrefetchRules
// and engine.Start use). region is the user-selected region (e.g. "cn").
//
// installedJSON is a JSON array:
//
//	[{"id","label","installer_package_name","process_names":[...]}]
//
// Returns JSON:
//
//	{"classifications":[{"id","default":"direct|proxy","hit_kind","hit_pattern"}]}
//
// Fail-soft: malformed JSON, unknown region, or a missing bundle yields every
// app as "proxy" (traffic stays in the tunnel). Never panics on bad input.
//
// gomobile exports this as Appext.classifyApps (Java) / AppextClassifyApps (ObjC).
func ClassifyApps(cacheDir, region, installedJSON string) string {
	var inputs []classifyAppInput
	if installedJSON != "" {
		if err := json.Unmarshal([]byte(installedJSON), &inputs); err != nil {
			slog.Warn("appext: ClassifyApps bad installedJSON", "err", err)
			inputs = nil
		}
	}

	apps := make([]krs.MatchableApp, 0, len(inputs))
	for _, in := range inputs {
		apps = append(apps, krs.MatchableApp{
			ID:                   in.ID,
			Label:                in.Label,
			InstallerPackageName: in.InstallerPackageName,
			ProcessNames:         in.ProcessNames,
		})
	}

	results := rule.ClassifyInstalled(filepath.Join(cacheDir, "rules"), region, runtime.GOOS, apps)

	out, err := json.Marshal(classifyAppsOutput{Classifications: results})
	if err != nil {
		slog.Warn("appext: ClassifyApps marshal failed", "err", err)
		return `{"classifications":[]}`
	}
	return string(out)
}
