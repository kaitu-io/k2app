//go:build linux

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
	case "set-credential":
		g.handleSetCredential(w, req.Params)
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

// applyCredential parses a freshly-minted subscription/wire URL pasted into the
// panel and persists it to BOTH the source config (k2r.yml) and the reconnect
// cache (state.json), then returns the parsed config. It does NOT touch the
// network — the HTTP handler launches doUp asynchronously. Keeping this a pure
// parse+persist function makes it unit-testable without a live Gateway.
func (g *Gateway) applyCredential(rawURL string) (*GatewayConfig, error) {
	cfg := GatewayFromURL(rawURL)
	// GatewayFromURL never returns nil and never errors on garbage; a bad URL
	// simply yields a route whose Via is not a wire outbound. Validate the
	// route scheme (k2v5/k2p/k2subs) so "not-a-url"/"direct" is rejected.
	if cfg == nil || len(cfg.Routes) == 0 || !config.IsOutboundURL(cfg.Routes[0].Via) {
		return nil, fmt.Errorf("invalid credential url")
	}
	if err := PersistConfig(cfg); err != nil {
		return nil, err
	}
	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})
	return cfg, nil
}

// handleSetCredential implements the /api/core "set-credential" action: the
// user pastes a minted k2subs:// URL into the panel, k2r persists it (k2r.yml
// + state.json) and connects asynchronously. The raw URL contains the bearer
// token and is never logged.
func (g *Gateway) handleSetCredential(w http.ResponseWriter, params map[string]any) {
	rawURL, _ := params["url"].(string)
	if strings.TrimSpace(rawURL) == "" {
		writeJSON(w, Response{Code: 1, Message: "missing url"})
		return
	}
	cfg, err := g.applyCredential(rawURL)
	if err != nil {
		writeJSON(w, Response{Code: 1, Message: "invalid credential"})
		return
	}
	safego.Go(func() {
		if err := g.doUp(cfg); err != nil {
			slog.Warn("gateway: set-credential doUp failed", "err", err)
		}
	})
	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (g *Gateway) handleDown(w http.ResponseWriter) {
	// User explicitly asked to disconnect via `k2r down` / webapp. Persist
	// the disconnect intent so daemon restart does not auto-reconnect.
	safego.Go(func() { g.doDown(true) })
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
