//go:build linux

package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/kaitu-io/k2/config"
)

// Request is the JSON body for /api/core.
type Request struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

// Response is the JSON envelope for all API responses.
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (g *Gateway) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, Response{Code: 0, Message: "pong"})
}

func (g *Gateway) handleCore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	switch req.Action {
	case "up":
		g.handleUp(w, req.Params)
	case "down":
		g.handleDown(w)
	case "status":
		g.handleStatus(w)
	default:
		writeJSON(w, Response{Code: 1, Message: "unknown action"})
	}
}

func (g *Gateway) handleUp(w http.ResponseWriter, params map[string]any) {
	var cfg *GatewayConfig
	if raw, ok := params["config"]; ok {
		data, _ := json.Marshal(raw)
		cfg = &GatewayConfig{}
		if err := json.Unmarshal(data, cfg); err != nil {
			writeJSON(w, Response{Code: 1, Message: "bad config: " + err.Error()})
			return
		}
		SetGatewayDefaults(cfg)
	} else {
		var err error
		cfg, err = LoadGateway(defaultConfigPath())
		if err != nil {
			writeJSON(w, Response{Code: 1, Message: "no config: " + err.Error()})
			return
		}
	}
	go func() {
		if err := g.doUp(cfg); err != nil {
			slog.Warn("gateway: doUp failed", "err", err)
		}
	}()
	writeJSON(w, Response{Code: 0, Message: "connecting"})
}

func (g *Gateway) handleDown(w http.ResponseWriter) {
	go g.doDown()
	writeJSON(w, Response{Code: 0, Message: "disconnecting"})
}

func (g *Gateway) handleStatus(w http.ResponseWriter) {
	g.mu.Lock()
	status := g.lastStatus
	g.mu.Unlock()
	writeJSON(w, Response{Code: 0, Data: status})
}

func (g *Gateway) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	config.SetLogLevel(body.Level)
	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func defaultConfigPath() string {
	return "/etc/k2r/k2r.yml"
}
