package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"github.com/electricbubble/gadb"
	"github.com/sasha-s/go-deadlock"
)

// AndroidHelper manages ADB-based Android app installation.
// It handles adb binary download, device detection, and APK installation.
type AndroidHelper struct {
	mu              deadlock.Mutex
	adbPath         string       // path to cached adb binary (empty if reusing existing server)
	adbPort         int          // port of the adb server we're using (5037 or 5038)
	weStartedServer bool         // true if we started the adb server (false = reusing existing)
	state           InstallState // current install progress
	driverScanDone  bool         // pnputil scan triggered once per daemon lifetime
}

// InstallState tracks the progress of an APK installation.
type InstallState struct {
	Phase    string `json:"phase"`    // idle/prepare_adb/downloading/pushing/installing/done/error
	Progress int    `json:"progress"` // 0-100
	Version  string `json:"version"`  // present for Kaitu (from latest.json), empty for direct URL
	Error    string `json:"error"`
}

// registerHelperRoutes adds the /api/helper endpoint to the daemon mux.
// Called from Daemon.Run(). The daemon package is never compiled for mobile
// (appext imports engine directly, not daemon), so no build tag is needed.
func (d *Daemon) registerHelperRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/helper", d.handleHelper)
}

func (d *Daemon) handleHelper(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, Response{Code: 405, Message: "method not allowed"})
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 400, Message: "invalid request"})
		return
	}

	switch req.Action {
	case "adb-detect":
		d.helper.handleDetect(w)
	case "adb-install":
		d.helper.handleInstall(w, req.Params)
	case "adb-status":
		d.helper.handleStatus(w)
	case "app-list-running":
		d.handleAppListRunning(w, r)
	default:
		writeJSON(w, Response{Code: 400, Message: "unknown helper action"})
	}
}

// deviceInfo is the JSON-serializable device entry returned by adb-detect.
type deviceInfo struct {
	Serial string `json:"serial"`
	State  string `json:"state"`
	Model  string `json:"model"`
}

// handleDetect returns adb readiness and connected device list.
// When adb is not yet prepared, it triggers preparation first.
// On Windows, triggers pnputil driver scan when no devices are found.
func (h *AndroidHelper) handleDetect(w http.ResponseWriter) {
	if err := h.prepareAdb(); err != nil {
		slog.Warn("helper: adb prepare failed during detect", "error", err)
		writeJSON(w, Response{
			Code: 0,
			Data: map[string]any{
				"adb_ready":         false,
				"devices":           []deviceInfo{},
				"installing_driver": false,
			},
		})
		return
	}

	devices := h.listDevices()
	installingDriver := false

	if len(devices) == 0 && runtime.GOOS == "windows" {
		installingDriver = h.triggerDriverScan()
	}

	writeJSON(w, Response{
		Code: 0,
		Data: map[string]any{
			"adb_ready":         true,
			"devices":           devices,
			"installing_driver": installingDriver,
		},
	})
}

// listDevices queries the adb server for connected devices via gadb.
// DeviceList (host:devices-l) requires 4+ fields per line, so unauthorized
// devices are silently skipped. We supplement with DeviceSerialList
// (host:devices, 2+ fields) to detect unauthorized devices.
func (h *AndroidHelper) listDevices() []deviceInfo {
	h.mu.Lock()
	port := h.adbPort
	h.mu.Unlock()
	if port == 0 {
		return []deviceInfo{}
	}
	client, err := gadb.NewClientWith("localhost", port)
	if err != nil {
		slog.Warn("helper: gadb client failed", "error", err)
		return []deviceInfo{}
	}

	// DeviceList returns only authorized devices (host:devices-l requires 4+ fields)
	devs, _ := client.DeviceList()

	// DeviceSerialList returns ALL serials including unauthorized (host:devices, 2+ fields)
	allSerials, _ := client.DeviceSerialList()

	// Build set of serials already in devs
	seen := map[string]bool{}
	result := make([]deviceInfo, 0, len(allSerials))
	for _, d := range devs {
		model, _ := d.Model()
		state, _ := d.State()
		seen[d.Serial()] = true
		result = append(result, deviceInfo{
			Serial: d.Serial(),
			State:  string(state),
			Model:  model,
		})
	}

	// Append unauthorized devices (in allSerials but not in devs)
	for _, serial := range allSerials {
		if !seen[serial] {
			result = append(result, deviceInfo{
				Serial: serial,
				State:  "unauthorized",
				Model:  "",
			})
		}
	}
	return result
}

// handleInstall starts an async APK installation.
func (h *AndroidHelper) handleInstall(w http.ResponseWriter, params map[string]any) {
	h.mu.Lock()
	busy := h.state.Phase != "idle" && h.state.Phase != "done" && h.state.Phase != "error"
	h.mu.Unlock()

	if busy {
		writeJSON(w, Response{Code: 1, Message: "install already in progress"})
		return
	}

	// Extract params
	serial, _ := params["serial"].(string)
	apkURL, _ := params["url"].(string)

	// Reset state for new install
	h.mu.Lock()
	h.state = InstallState{Phase: "prepare_adb"}
	h.mu.Unlock()

	go h.doInstall(serial, apkURL)

	writeJSON(w, Response{Code: 0, Message: "installing"})
}

// handleStatus returns the current install progress.
func (h *AndroidHelper) handleStatus(w http.ResponseWriter) {
	h.mu.Lock()
	state := h.state
	h.mu.Unlock()

	writeJSON(w, Response{Code: 0, Data: state})
}

// setPhase updates the install phase and progress atomically.
func (h *AndroidHelper) setPhase(phase string, progress int) {
	h.mu.Lock()
	h.state.Phase = phase
	h.state.Progress = progress
	h.state.Error = ""
	h.mu.Unlock()
}

// setProgress updates the download/push progress percentage.
func (h *AndroidHelper) setProgress(progress int) {
	h.mu.Lock()
	h.state.Progress = progress
	h.mu.Unlock()
}

// setError transitions to error state with a message.
func (h *AndroidHelper) setError(msg string) {
	h.mu.Lock()
	h.state.Phase = "error"
	h.state.Error = msg
	h.mu.Unlock()
}
