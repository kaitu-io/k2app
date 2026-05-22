//go:build linux

package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// appListRunningResp is the JSON payload returned by /api/helper app-list-running.
type appListRunningResp struct {
	Apps []runningAppJSON `json:"apps"`
}

// runningAppJSON describes a single running executable observed via /proc.
type runningAppJSON struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	ProcessNames []string `json:"process_names"`
}

// parseProcExeBasename extracts a comparable basename from a /proc/<pid>/exe
// readlink target. Returns "" for empty or directory-only inputs.
//
// Strips the kernel's " (deleted)" suffix that appears when the binary has
// been replaced mid-run (apt/dpkg/snap upgrade). The provider-layer
// ProcessSearcher also strips this suffix so the UI's view and the
// daemon's runtime attribution stay byte-identical — otherwise a user who
// adds a freshly-upgraded app to bypass would see the rule fail to fire.
func parseProcExeBasename(link string) string {
	link = strings.TrimSuffix(link, " (deleted)")
	if link == "" || link == "/" {
		return ""
	}
	cleaned := filepath.Clean(link)
	base := filepath.Base(cleaned)
	if base == "/" || base == "." {
		return ""
	}
	return base
}

// handleAppListRunning enumerates currently running executables by reading
// /proc/<pid>/exe symlinks. Returns one entry per distinct exe path.
// Permission-denied / zombie processes are silently skipped — this is a
// best-effort snapshot consumed by the App Bypass UI.
func (d *Daemon) handleAppListRunning(w http.ResponseWriter, _ *http.Request) {
	selfPid := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		writeJSON(w, Response{Code: 1, Message: "read /proc failed: " + err.Error()})
		return
	}

	seen := map[string]runningAppJSON{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == selfPid {
			continue
		}
		link, err := os.Readlink("/proc/" + e.Name() + "/exe")
		if err != nil {
			continue // permission denied, zombie, or kernel thread — skip
		}
		basename := parseProcExeBasename(link)
		if basename == "" {
			continue
		}
		if _, dup := seen[link]; dup {
			continue
		}
		seen[link] = runningAppJSON{
			ID:           link,
			Label:        basename,
			ProcessNames: []string{basename},
		}
	}

	apps := make([]runningAppJSON, 0, len(seen))
	for _, app := range seen {
		apps = append(apps, app)
	}

	writeJSON(w, Response{Code: 0, Data: appListRunningResp{Apps: apps}})
}
