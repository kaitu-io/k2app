//go:build linux

package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/safego"
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

const maxRequestBody = 1 << 20 // 1 MB

func (g *Gateway) handleCore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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
	case "version":
		writeJSON(w, Response{Code: 0, Data: map[string]string{
			"version": g.version,
			"commit":  g.commit,
		}})
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
	safego.Go(func() {
		if err := g.doUp(cfg); err != nil {
			slog.Warn("gateway: doUp failed", "err", err)
		}
	})
	writeJSON(w, Response{Code: 0, Message: "connecting"})
}

func (g *Gateway) handleDown(w http.ResponseWriter) {
	safego.Go(g.doDown)
	writeJSON(w, Response{Code: 0, Message: "disconnecting"})
}

func (g *Gateway) handleStatus(w http.ResponseWriter) {
	g.mu.Lock()
	status := g.lastStatus
	interceptorName := ""
	if g.interceptor != nil {
		interceptorName = g.interceptor.Name()
	}
	var subnets []string
	listenPort := 0
	if g.provider != nil {
		subnets = g.provider.cfg.LANSubnets
		listenPort = g.provider.cfg.ListenPort
	}
	rdm := g.routerDeviceMgr
	g.mu.Unlock()

	// Read rdm fields OUTSIDE g.mu to avoid lock ordering violation
	var maxLanClient int
	var routerDeviceMode string
	if rdm != nil {
		rdm.mu.RLock()
		maxLanClient = rdm.quota
		routerDeviceMode = rdm.mode
		rdm.mu.RUnlock()
	}

	type gatewayStatus struct {
		engine.Status
		LANSubnets       []string `json:"lanSubnets,omitempty"`
		Interceptor      string   `json:"interceptor,omitempty"`
		ListenPort       int      `json:"listenPort,omitempty"`
		MaxLanClient     int      `json:"maxLanClient,omitempty"`
		RouterDeviceMode string   `json:"routerDeviceMode,omitempty"`
	}
	writeJSON(w, Response{Code: 0, Data: gatewayStatus{
		Status:           status,
		Interceptor:      interceptorName,
		LANSubnets:       subnets,
		ListenPort:       listenPort,
		MaxLanClient:     maxLanClient,
		RouterDeviceMode: routerDeviceMode,
	}})
}

func (g *Gateway) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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

// --- Router device management handlers ---
// Each resolves g.routerDeviceMgr under g.mu, same pattern as handleStatus.

func (g *Gateway) requireRDM(w http.ResponseWriter) *RouterDeviceManager {
	g.mu.Lock()
	rdm := g.routerDeviceMgr
	g.mu.Unlock()
	if rdm == nil {
		writeJSON(w, Response{Code: 1, Message: "not connected"})
	}
	return rdm
}

func (g *Gateway) handleRouterDevices(w http.ResponseWriter, r *http.Request) {
	if rdm := g.requireRDM(w); rdm != nil {
		rdm.handleList(w, r)
	}
}

func (g *Gateway) handleRouterDeviceAllow(w http.ResponseWriter, r *http.Request) {
	if rdm := g.requireRDM(w); rdm != nil {
		rdm.handleAllow(w, r)
	}
}

func (g *Gateway) handleRouterDeviceRemove(w http.ResponseWriter, r *http.Request) {
	if rdm := g.requireRDM(w); rdm != nil {
		rdm.handleRemove(w, r)
	}
}

func (g *Gateway) handleRouterDeviceMode(w http.ResponseWriter, r *http.Request) {
	if rdm := g.requireRDM(w); rdm != nil {
		rdm.handleMode(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func defaultConfigPath() string {
	return "/etc/k2r/k2r.yml"
}
