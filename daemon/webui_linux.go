//go:build linux

package daemon

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kaitu-io/k2/webui"
)

// webuiStateDir is the directory where Linux k2 persists its webui state
// (encrypted storage, update-state.json). Created by the daemon on first
// access with mode 0700.
const webuiStateDir = "/etc/kaitu"

// webOTADir is where Linux k2 keeps hot-updatable webapp bundles
// (pending/current/previous). current/ is served as the SPA disk
// override; see the webui package.
const webOTADir = webuiStateDir + "/web-ota"

// k2DesktopReleaseEndpoints lists the CDN base URLs used by the Linux k2
// desktop OTA upgrader. Tried in order; first successful LATEST +
// checksums.txt fetch wins.
var k2DesktopReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop",
	"https://dl.kaitu.io/kaitu/desktop",
}

// k2WebOTAEndpoints lists the brand CDN base URLs for the web OTA
// manifest (<base>/web/latest.json), same dual-domain pattern as
// k2DesktopReleaseEndpoints. Overridable via K2_WEBOTA_ENDPOINTS
// (comma-separated) for UAT and incident debugging.
var k2WebOTAEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu",
	"https://dl.kaitu.io/kaitu",
}

// webOTAEndpoints returns the effective endpoint list, honoring the
// K2_WEBOTA_ENDPOINTS env override.
func webOTAEndpoints() []string {
	env := os.Getenv("K2_WEBOTA_ENDPOINTS")
	if env == "" {
		return k2WebOTAEndpoints
	}
	var out []string
	for _, e := range strings.Split(env, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, strings.TrimSuffix(e, "/"))
		}
	}
	if len(out) == 0 {
		return k2WebOTAEndpoints
	}
	return out
}

// newK2DesktopUpgrader wires a webui.Upgrader for the Linux k2 desktop
// binary. RestartFunc invokes systemctl; callers must install the binary
// as a systemd service named "kaitu" for this to succeed.
func (d *Daemon) newK2DesktopUpgrader() *webui.Upgrader {
	return webui.NewUpgrader(d.Version, webui.UpgradeConfig{
		BinaryName:       "k2",
		StateDir:         webuiStateDir,
		ReleaseEndpoints: k2DesktopReleaseEndpoints,
		RestartFunc: func() {
			exec.Command("systemctl", "restart", "kaitu").Run()
		},
	})
}

// registerWebUI registers the Linux-only webui HTTP routes on mux:
// /api/storage, /api/platform, /api/upgrade, and / (SPA fallback). On
// non-Linux builds the stub in webui_nop.go is a no-op, so callers can
// invoke registerWebUI unconditionally.
//
// The /api/core, /api/events, /api/log-level, /api/stats, /stats, and
// /ping routes are registered by the daemon itself and are not touched
// here; they are the same on every platform.
//
// The /api/helper (ADB) routes are already registered before this call
// via registerHelperRoutes(); they are orthogonal to webui.
func (d *Daemon) registerWebUI(ctx context.Context, mux *http.ServeMux) {
	storage := webui.NewStorage(filepath.Join(webuiStateDir, "storage.json"))
	upgrader := d.newK2DesktopUpgrader()
	upgrader.RecoverOnStartup()

	mux.HandleFunc("/api/storage", webui.NewStorageHandler(storage))
	mux.HandleFunc("/api/platform", webui.NewPlatformHandler(webui.PlatformInfo{
		PlatformType: "desktop",
		Version:      d.Version,
		Commit:       d.Commit,
		Arch:         runtime.GOARCH,
	}))
	mux.HandleFunc("/api/upgrade", webui.NewUpgradeHandler(upgrader))

	// SPA catch-all with web OTA disk override. Longest-prefix match in
	// http.ServeMux means this does not shadow the explicit /ping,
	// /stats, /api/* routes. The OTA poller only runs when a webapp is
	// embedded — nowebapp builds get a nil handler and no poller.
	ota := webui.NewWebOTA(webui.WebOTAConfig{
		Endpoints:    webOTAEndpoints(),
		Dir:          webOTADir,
		ShellVersion: d.Version,
	})
	if h := webui.NewWebappHandlerWithOverride(d.Version, d.Commit, runtime.GOARCH, ota.CurrentDir()); h != nil {
		mux.Handle("/", h)
		ota.Start(ctx)
	}
}
