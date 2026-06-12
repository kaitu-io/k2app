package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// usage_api.go exposes a loopback-only, READ-ONLY observability surface:
//   GET /usage → live node traffic counters {rx,tx,total,ts}
// for ops/smoke (`curl 127.0.0.1:9099/usage`).
//
// History: under the retired sidecar split this API also had mutating
// POST /reset and POST /verdict endpoints, polled/pushed by the sidecar that
// held K2_NODE_SECRET. Under Option D (Plan 3-k2s Phase B) the usage-cutoff
// loop moved IN-PROCESS (server/usage_reporter.go): k2s itself reads its
// counters, POSTs Center, and applies the verdict/epoch-reset via direct method
// calls on the ProxyHandler. With no external poller, /reset and /verdict have
// no caller and were removed; only the read-only /usage endpoint remains.
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
// Routes (loopback-guarded, read-only):
//   GET /usage → {"rx","tx","total","ts"}
//
// Mutating endpoints (POST /reset, POST /verdict) were removed under Option D —
// the usage-cutoff loop is now in-process (see usage_reporter.go).
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

	return &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
