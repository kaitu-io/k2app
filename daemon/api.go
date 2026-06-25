package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/wire"
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

	slog.Debug("daemon: api request", "action", req.Action)

	switch req.Action {
	case "up":
		d.handleUp(w, req.Params)
	case "down":
		d.handleDown(w)
	case "status":
		d.handleStatus(w)
	case "version":
		d.handleVersion(w)
	case "probe":
		d.handleProbe(w, req.Params)
	case "classify-apps":
		d.classifyApps(w, req.Params)
	case "relay-fetch":
		d.handleRelay(w, req.Params)
	case "get-presets":
		d.getPresets(w, req.Params)
	default:
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("unknown action: %s", req.Action)})
	}
}

func (d *Daemon) handleUp(w http.ResponseWriter, params map[string]any) {
	pidFloat, _ := params["pid"].(float64) // JSON numbers are float64
	pid := int(pidFloat)

	var cfg *config.ClientConfig

	// Parse config from params.
	if raw, ok := params["config"]; ok {
		// Re-marshal the config sub-object to JSON, then unmarshal into ClientConfig.
		data, err := json.Marshal(raw)
		if err != nil {
			writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad config: %v", err)})
			return
		}
		cfg = &config.ClientConfig{}
		if err := json.Unmarshal(data, cfg); err != nil {
			writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("parse config: %v", err)})
			return
		}
		config.SetDefaults(cfg)
	}

	if cfg == nil {
		// Use last saved config.
		d.mu.RLock()
		cfg = d.lastConfig
		d.mu.RUnlock()
	}

	if cfg == nil {
		writeJSON(w, Response{Code: 510, Message: "no configuration provided and no saved config"})
		return
	}

	if err := d.doUp(cfg, pid); err != nil {
		writeJSON(w, Response{Code: 511, Message: fmt.Sprintf("connect failed: %v", err)})
		return
	}

	writeJSON(w, Response{Code: 0, Message: "connecting"})
}

func (d *Daemon) handleDown(w http.ResponseWriter) {
	d.doDown()
	writeJSON(w, Response{Code: 0, Message: "disconnected"})
}

func (d *Daemon) handleStatus(w http.ResponseWriter) {
	writeJSON(w, Response{Code: 0, Message: "ok", Data: d.statusInfo()})
}

func (d *Daemon) handleVersion(w http.ResponseWriter) {
	v := d.Version
	if v == "" {
		v = "dev"
	}
	data := map[string]any{
		"version": v,
		"go":      runtime.Version(),
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
	if d.Commit != "" {
		data["commit"] = d.Commit
	}
	writeJSON(w, Response{Code: 0, Message: "ok", Data: data})
}

func (d *Daemon) handleStats(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()
	var history []engine.HealthSnapshot
	if eng != nil {
		history = eng.StatsHistory()
	}
	if history == nil {
		history = []engine.HealthSnapshot{}
	}
	writeJSON(w, Response{Code: 0, Message: "ok", Data: history})
}

func (d *Daemon) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, Response{Code: 405, Message: "method not allowed"})
		return
	}
	var req struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 400, Message: "invalid request"})
		return
	}
	config.SetLogLevel(req.Level)
	slog.Info("log level changed", "level", req.Level)
	writeJSON(w, Response{Code: 0, Message: "ok"})
}

// handleRelay relays one control-plane HTTP request to Center through a node
// (antiblock Phase 2). Params is a wire.RelayRequest. Stateless — independent
// of VPN/engine state, so logout/expired/quota'd users can still reach Center.
func (d *Daemon) handleRelay(w http.ResponseWriter, params map[string]any) {
	reqJSON, err := json.Marshal(params)
	if err != nil {
		writeJSON(w, Response{Code: 400, Message: fmt.Sprintf("bad relay params: %v", err)})
		return
	}
	// wire returns a full {code,message,data} envelope as a JSON string; pass it
	// through verbatim so the response shape matches the rest of /api/core.
	out := wire.RelayFetchJSON(string(reqJSON))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(out))
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
