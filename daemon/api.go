package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// Request is the JSON request body for POST /api/core.
type Request struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
}

// Response is the JSON response body.
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (d *Daemon) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, Response{Code: 0, Message: "pong"})
}

func (d *Daemon) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	writeJSON(w, Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"goroutines":  runtime.NumGoroutine(),
			"heap_alloc":  m.HeapAlloc,
			"heap_sys":    m.HeapSys,
			"gc_cycles":   m.NumGC,
			"gc_pause_ns": m.PauseNs[(m.NumGC+255)%256],
		},
	})
}

func (d *Daemon) handleCore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, Response{Code: 405, Message: "method not allowed"})
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad request: %v", err)})
		return
	}

	switch req.Action {
	case "up":
		d.handleUp(w, req.Params)
	case "down":
		d.handleDown(w)
	case "reconnect":
		d.handleReconnect(w)
	case "status":
		d.handleStatus(w)
	case "speedtest":
		d.handleSpeedtest(w, req.Params)
	case "get_speedtest_status":
		d.handleGetSpeedtestStatus(w)
	case "get_config":
		d.handleGetConfig(w)
	case "version":
		d.handleVersion(w)
	default:
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("unknown action: %s", req.Action)})
	}
}

func (d *Daemon) handleUp(w http.ResponseWriter, params map[string]any) {
	wireURL, _ := params["wire_url"].(string)
	configPath, _ := params["config_path"].(string)
	pidFloat, _ := params["pid"].(float64) // JSON numbers are float64
	pid := int(pidFloat)

	if wireURL == "" && configPath == "" {
		// Use last saved config.
		d.mu.RLock()
		wireURL = d.lastWireURL
		configPath = d.lastConfigPath
		d.mu.RUnlock()
	}

	if wireURL == "" && configPath == "" {
		writeJSON(w, Response{Code: 510, Message: "no configuration provided and no saved config"})
		return
	}

	if err := d.doUp(wireURL, configPath, pid); err != nil {
		writeJSON(w, Response{Code: 511, Message: fmt.Sprintf("connect failed: %v", err)})
		return
	}

	writeJSON(w, Response{Code: 0, Message: "connecting"})
}

func (d *Daemon) handleDown(w http.ResponseWriter) {
	d.doDown()
	writeJSON(w, Response{Code: 0, Message: "disconnected"})
}

func (d *Daemon) handleReconnect(w http.ResponseWriter) {
	d.mu.RLock()
	wireURL := d.lastWireURL
	configPath := d.lastConfigPath
	d.mu.RUnlock()

	if wireURL == "" && configPath == "" {
		writeJSON(w, Response{Code: 510, Message: "no saved config to reconnect with"})
		return
	}

	if err := d.doUp(wireURL, configPath, 0); err != nil {
		writeJSON(w, Response{Code: 511, Message: fmt.Sprintf("reconnect failed: %v", err)})
		return
	}

	writeJSON(w, Response{Code: 0, Message: "reconnecting"})
}

func (d *Daemon) handleStatus(w http.ResponseWriter) {
	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.statusInfo()})
}

func (d *Daemon) handleSpeedtest(w http.ResponseWriter, params map[string]any) {
	if d.speedtest.IsRunning() {
		writeJSON(w, Response{Code: 512, Message: "speedtest already running"})
		return
	}

	serverID, _ := params["server_id"].(string)
	go d.runSpeedtest(serverID)
	writeJSON(w, Response{Code: 0, Message: "speedtest started"})
}

func (d *Daemon) handleGetSpeedtestStatus(w http.ResponseWriter) {
	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.speedtest.GetStatus()})
}

func (d *Daemon) handleGetConfig(w http.ResponseWriter) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	writeJSON(w, Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"wire_url":    d.lastWireURL,
			"config_path": d.lastConfigPath,
		},
	})
}

func (d *Daemon) handleVersion(w http.ResponseWriter) {
	writeJSON(w, Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"version": "dev",
			"go":      runtime.Version(),
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		},
	})
}

func writeJSON(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// corsMiddleware adds CORS headers for localhost and Tauri.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	allowed := []string{
		"http://localhost",
		"http://127.0.0.1",
		"https://localhost",
		"https://127.0.0.1",
		"tauri://localhost",
		"https://tauri.localhost",
	}
	for _, a := range allowed {
		if origin == a || strings.HasPrefix(origin, a+":") {
			return true
		}
	}
	return false
}
