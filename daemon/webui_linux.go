//go:build linux

package daemon

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/kaitu-io/k2/webui"
)

// webuiStateDir is the directory where Linux k2 persists its webui state
// (encrypted storage, update-state.json). Created by the daemon on first
// access with mode 0700.
const webuiStateDir = "/etc/kaitu"

// k2DesktopReleaseEndpoints lists the CDN base URLs used by the Linux k2
// desktop OTA upgrader. Tried in order; first successful LATEST +
// checksums.txt fetch wins.
var k2DesktopReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop",
	"https://dl.kaitu.io/kaitu/desktop",
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
func (d *Daemon) registerWebUI(mux *http.ServeMux) {
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

	// SPA catch-all. Longest-prefix match in http.ServeMux means this
	// does not shadow the explicit /ping, /stats, /api/* routes.
	if h := webui.NewWebappHandler(d.Version, d.Commit, runtime.GOARCH); h != nil {
		mux.Handle("/", h)
	}
}
