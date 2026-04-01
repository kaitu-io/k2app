package signal

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Handler handles HTTP requests for the k2p signal server.
type Handler struct {
	mux        *http.ServeMux
	store      *Store
	router     *SessionRouter
	turnSecret string
	turnURLs   []string
	upgrader   websocket.Upgrader
}

// NewHandler creates a Handler backed by store and registers all routes.
// turnSecret and turnURLs are optional; when turnSecret is non-empty,
// TURN credentials are included in lookup responses.
func NewHandler(store *Store, turnSecret string, turnURLs []string) *Handler {
	h := &Handler{
		mux:        http.NewServeMux(),
		store:      store,
		router:     NewSessionRouter(),
		turnSecret: turnSecret,
		turnURLs:   turnURLs,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	h.mux.HandleFunc("POST /k2p/claim", h.handleClaim)
	h.mux.HandleFunc("POST /k2p/register", h.handleRegister)
	h.mux.HandleFunc("GET /k2p/lookup/{udid}", h.handleLookup)
	h.mux.HandleFunc("GET /k2p/ws", h.handleWS)
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

	if err := ValidateUDID(req.UDID); err != nil {
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

	if err := ValidateUDID(req.UDID); err != nil {
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
		Networks: entry.Networks,
		DNS:      entry.DNS,
		Online:   h.store.IsOnline(udid),
	}

	if h.turnSecret != "" {
		creds := GenerateTURNCredentials(h.turnSecret, udid, 1*time.Hour)
		creds.URLs = h.turnURLs
		resp.TURN = &creds
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

// handleWS handles GET /k2p/ws.
// Accepts role=home (authenticated, registers for sessions) or role=away (creates a session).
// Home: validates token, upgrades, enters read loop routing messages to Away peers.
// Away: upgrades, creates session with Home, enters read loop routing messages to Home.
func (h *Handler) handleWS(w http.ResponseWriter, r *http.Request) {
	udid := r.URL.Query().Get("udid")
	role := r.URL.Query().Get("role")
	token := r.URL.Query().Get("token")

	switch role {
	case "home":
		h.handleWSHome(w, r, udid, token)
	case "away":
		h.handleWSAway(w, r, udid)
	default:
		http.Error(w, "bad request: role=home|away required", http.StatusBadRequest)
	}
}

func (h *Handler) handleWSHome(w http.ResponseWriter, r *http.Request, udid, token string) {
	if !h.store.ValidateToken(udid, token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("DIAG: ws-upgrade-fail", "udid", udid, "role", "home", "err", err)
		return
	}

	h.store.SetWS(udid, conn)
	h.router.SetHome(udid, conn)
	defer func() {
		h.store.SetWS(udid, nil)
		h.router.ClearHome(udid)
		conn.Close()
	}()

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		h.router.Route("home", msg)
	}
}

func (h *Handler) handleWSAway(w http.ResponseWriter, r *http.Request, udid string) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("DIAG: ws-upgrade-fail", "udid", udid, "role", "away", "err", err)
		return
	}
	defer conn.Close()

	sessionID, err := h.router.CreateSession(udid, conn)
	if err != nil {
		conn.WriteJSON(WSMessage{Type: "error"}) //nolint:errcheck
		return
	}
	defer h.router.CleanupSession(sessionID, "away")

	conn.WriteJSON(WSMessage{Type: "session-start", Session: sessionID}) //nolint:errcheck

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		msg.Session = sessionID // enforce session ID
		h.router.Route("away", msg)
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
