//go:build !linux

package daemon

import (
	"context"
	"net/http"
)

// registerWebUI is a no-op on non-Linux platforms. The Linux build pulls
// in the real implementation from webui_linux.go, which imports the
// k2/webui package (itself Linux-only). On macOS and Windows the Tauri
// shell provides the webapp, so the daemon has no SPA to serve and no
// web OTA poller to run.
func (d *Daemon) registerWebUI(ctx context.Context, mux *http.ServeMux) {}
