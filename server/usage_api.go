package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// usage_api.go exposes a loopback-only HTTP control surface so the sidecar
// (which holds K2_NODE_SECRET) can poll node traffic counters and push
// Center's serve/stop verdict — without k2s ever knowing the node secret or
// talking to Center directly (Plan 3-k2s, A3).
//
// Design: bind to loopback (default 127.0.0.1:9099). A loopback guard on every
// handler is defense-in-depth even though the listen address is already
// loopback. No auth beyond that — the trust boundary is "anything that can
// reach this socket is on the box".

// isLoopback reports whether remoteAddr's host is a loopback IP.
// It tolerates both "host:port" and bare "host" forms. Any parse failure or
// non-loopback IP returns false (fail-closed).
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Maybe a bare host with no port.
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackOnly wraps a handler, rejecting any request whose RemoteAddr is not
// a loopback IP with 403.
func loopbackOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			slog.Warn("DIAG: usage-api-reject-nonloopback", "remote", r.RemoteAddr, "path", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newUsageAPI builds the loopback usage API server bound to listen.
// Routes (all loopback-guarded):
//   GET  /usage   → {"rx","tx","total","ts"}
//   POST /reset   → {"ok":true} (zeroes counters)
//   POST /verdict → body {"accepting":bool} → {"ok":true} (toggles accept gate)
func newUsageAPI(h *ProxyHandler, listen string) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/usage", loopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rx, tx := h.TrafficBytes()
		writeJSON(w, map[string]int64{
			"rx":    rx,
			"tx":    tx,
			"total": rx + tx,
			"ts":    time.Now().Unix(),
		})
	}))

	mux.HandleFunc("/reset", loopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.ResetTraffic()
		writeJSON(w, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/verdict", loopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Accepting bool `json:"accepting"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		h.SetAccepting(body.Accepting)
		writeJSON(w, map[string]bool{"ok": true})
	}))

	return &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
