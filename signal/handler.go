package signal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// Handler handles HTTP requests for the k2p signal server.
type Handler struct {
	mux      *http.ServeMux
	store    *Store
	upgrader websocket.Upgrader
	relay    *Relay
}

// NewHandler creates a Handler backed by store and registers all routes.
func NewHandler(store *Store) *Handler {
	h := &Handler{
		mux:   http.NewServeMux(),
		store: store,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	h.relay = NewRelay()
	h.mux.HandleFunc("POST /k2p/claim", h.handleClaim)
	h.mux.HandleFunc("POST /k2p/register", h.handleRegister)
	h.mux.HandleFunc("GET /k2p/lookup/{udid}", h.handleLookup)
	h.mux.HandleFunc("POST /k2p/connect", h.handleConnect)
	h.mux.HandleFunc("GET /k2p/ws", h.handleWS)
	h.mux.HandleFunc("GET /k2p/relay", h.relay.HandleRelay)
	return h
}

// ServeHTTP implements http.Handler by delegating to the internal mux.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// handleClaim handles POST /k2p/claim.
// Decodes a ClaimRequest, claims the UDID, and returns a ClaimResponse.
// Returns 409 if the UDID is already registered.
func (h *Handler) handleClaim(w http.ResponseWriter, r *http.Request) {
	var req ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	token, err := h.store.Claim(req.UDID)
	if err != nil {
		http.Error(w, "conflict", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ClaimResponse{RegToken: token}) //nolint:errcheck
}

// handleRegister handles POST /k2p/register.
// Requires a Bearer token in the Authorization header.
// Returns 401 if missing, 403 if invalid, 400 on other errors.
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	token := extractBearer(r)
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := h.store.Update(req.UDID, token, req); err != nil {
		// Distinguish bad token (403) from other errors (400).
		if strings.Contains(err.Error(), "invalid token") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleLookup handles GET /k2p/lookup/{udid}.
// Returns a LookupResponse (200) or 404 if the UDID is unknown.
func (h *Handler) handleLookup(w http.ResponseWriter, r *http.Request) {
	udid := r.PathValue("udid")

	entry, ok := h.store.Get(udid)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resp := LookupResponse{
		UDID:     entry.UDID,
		Addr:     entry.Addr,
		NATType:  entry.NATType,
		Networks: entry.Networks,
		DNS:      entry.DNS,
		Online:   h.store.IsOnline(udid),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// handleConnect handles POST /k2p/connect.
// Decodes a ConnectRequest, retrieves the Home's WebSocket connection,
// and forwards a PunchNotify message. Returns 404 if the Home is offline,
// 502 on write failure.
func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req ConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ws := h.store.GetWS(req.HomeUDID)
	if ws == nil {
		http.Error(w, "home not connected", http.StatusNotFound)
		return
	}

	notify := PunchNotify{
		AwayAddr: req.AwayAddr,
		NATType:  req.NATType,
	}
	msg, err := json.Marshal(notify)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil {
		slog.Warn("DIAG: punch-notify write fail",
			"home", req.HomeUDID,
			"away", req.AwayAddr,
			"err", err,
		)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleWS handles GET /k2p/ws.
// Reads udid and token query params, validates the token, upgrades to WebSocket,
// registers the connection in the store, and holds the connection in a read loop.
// On disconnect, the WS entry is cleared.
func (h *Handler) handleWS(w http.ResponseWriter, r *http.Request) {
	udid := r.URL.Query().Get("udid")
	token := r.URL.Query().Get("token")

	if !h.store.ValidateToken(udid, token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes its own error response on failure.
		slog.Warn("DIAG: ws-upgrade-fail", "udid", udid, "err", err)
		return
	}

	h.store.SetWS(udid, conn)
	defer func() {
		h.store.SetWS(udid, nil)
		conn.Close()
	}()

	// Read loop — holds the connection alive until the client disconnects.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// extractBearer parses the "Authorization: Bearer <token>" header and returns
// the token. Returns an empty string if the header is absent or malformed.
func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}
