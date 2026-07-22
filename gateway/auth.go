//go:build linux

package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// currentControlKeyHash returns the in-memory control-key hash ("" = unbound).
// Lazily loads from disk on first call so zero-value Gateways (tests) and
// New()-built Gateways behave identically.
func (g *Gateway) currentControlKeyHash() string {
	g.mu.Lock()
	if !g.controlKeyLoaded {
		g.mu.Unlock()
		h := loadControlKeyHash() // file I/O outside the state lock
		g.mu.Lock()
		if !g.controlKeyLoaded {
			g.controlKeyHash = h
			g.controlKeyLoaded = true
		}
	}
	h := g.controlKeyHash
	g.mu.Unlock()
	return h
}

// setControlKeyHash persists then publishes a new control-key hash.
// File I/O happens before the state lock (no I/O under lock).
func (g *Gateway) setControlKeyHash(hash string) error {
	if err := saveControlKeyHash(hash); err != nil {
		return err
	}
	g.mu.Lock()
	g.controlKeyHash = hash
	g.controlKeyLoaded = true
	g.mu.Unlock()
	return nil
}

// isLoopback reports whether the request originates from 127.0.0.1/::1.
// The local CLI (k2r up/down/status/reset) talks over loopback, and anyone
// able to originate loopback traffic already owns the router.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// authorized reports whether r may exercise the control API. Deny-by-default:
// with no bound key, only loopback passes.
func (g *Gateway) authorized(r *http.Request) bool {
	if isLoopback(r) {
		return true
	}
	stored := g.currentControlKeyHash()
	if stored == "" {
		return false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return false
	}
	sum := hashControlKey(token)
	return subtle.ConstantTimeCompare([]byte(sum), []byte(stored)) == 1
}

// writeUnauthorized emits HTTP 401 with the Response envelope. The companion
// app keys off the 401 STATUS (not the envelope) to re-fetch its control key
// from Center — keep the status code stable (cross-repo contract).
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(Response{Code: 401, Message: "unauthorized"})
}

// reconcileControlKey adopts the account control-key hash from Center — the
// single authority. One rule covers first-setup verification (provisional
// push gets overwritten if stale), legacy adoption (no local hash), and
// rotation convergence: Center wins.
func (g *Gateway) reconcileControlKey(centerHash string) {
	if centerHash == "" || centerHash == g.currentControlKeyHash() {
		return
	}
	if err := g.setControlKeyHash(centerHash); err != nil {
		slog.Warn("gateway: reconcile control key failed", "err", err)
		return
	}
	slog.Info("DIAG: control-key-adopt", "source", "subs")
}

// requireAuth gates the whole mux: /ping and / stay open (discovery + notice
// page); /api/core defers to per-action enforcement in handleCore (the TOFU
// entry lives there); everything else needs authorized().
func (g *Gateway) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping", "/", "/api/core":
			next.ServeHTTP(w, r)
		default:
			if !g.authorized(r) {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}
