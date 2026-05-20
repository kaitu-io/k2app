//go:build !linux

package daemon

import "net/http"

// handleAppListRunning is a stub on non-Linux platforms. The /proc-based
// enumeration is Linux-specific; macOS and Windows clients should not invoke
// this action.
func (d *Daemon) handleAppListRunning(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, Response{Code: 400, Message: "app-list-running not supported on this platform"})
}
