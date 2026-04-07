package signal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

// session pairs a Home WS and an Away WS for ICE candidate exchange.
type session struct {
	id   string
	home *wsConn
	away *wsConn
}

// SessionRouter manages WS sessions for ICE candidate exchange.
type SessionRouter struct {
	mu       deadlock.Mutex
	sessions map[string]*session
	homeWS   map[string]*wsConn // UDID → Home WS
}

// wsConn wraps a websocket.Conn with a write mutex for serialization.
type wsConn struct {
	conn    *websocket.Conn
	writeMu deadlock.Mutex
}

func (w *wsConn) WriteJSON(v any) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return w.conn.WriteJSON(v)
}

// NewSessionRouter creates a SessionRouter with empty session and home maps.
func NewSessionRouter() *SessionRouter {
	return &SessionRouter{
		sessions: make(map[string]*session),
		homeWS:   make(map[string]*wsConn),
	}
}

// SetHome registers a Home WS connection for the given UDID.
func (r *SessionRouter) SetHome(udid string, ws *websocket.Conn) {
	r.mu.Lock()
	r.homeWS[udid] = &wsConn{conn: ws}
	r.mu.Unlock()
}

// ClearHome removes the Home WS registration for the given UDID.
func (r *SessionRouter) ClearHome(udid string) {
	r.mu.Lock()
	delete(r.homeWS, udid)
	r.mu.Unlock()
}

// CreateSession pairs an Away WS with the Home WS for the given UDID.
// Returns the session ID or an error if Home is not connected.
// Sends a "session-start" message to Home.
func (r *SessionRouter) CreateSession(udid string, awayWS *websocket.Conn) (string, error) {
	id := generateSessionID()

	r.mu.Lock()
	home := r.homeWS[udid]
	if home == nil {
		r.mu.Unlock()
		return "", fmt.Errorf("signal: home %q not connected", udid)
	}
	awayWrapped := &wsConn{conn: awayWS}
	r.sessions[id] = &session{id: id, home: home, away: awayWrapped}
	r.mu.Unlock()

	msg := WSMessage{Type: "session-start", Session: id}
	if err := home.WriteJSON(msg); err != nil {
		r.mu.Lock()
		delete(r.sessions, id)
		r.mu.Unlock()
		return "", err
	}
	return id, nil
}

// Route forwards a WSMessage to the peer in the session opposite to senderRole.
func (r *SessionRouter) Route(senderRole string, msg WSMessage) {
	r.mu.Lock()
	sess, ok := r.sessions[msg.Session]
	r.mu.Unlock()
	if !ok {
		return
	}

	var target *wsConn
	if senderRole == "home" {
		target = sess.away
	} else {
		target = sess.home
	}
	if target != nil {
		target.WriteJSON(msg) //nolint:errcheck
	}
}

// CleanupSession removes the session and notifies the remaining peer with "session-end".
func (r *SessionRouter) CleanupSession(sessionID string, disconnectedRole string) {
	r.mu.Lock()
	sess, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	endMsg := WSMessage{Type: "session-end", Session: sessionID}
	if disconnectedRole == "away" && sess.home != nil {
		sess.home.WriteJSON(endMsg) //nolint:errcheck
	} else if disconnectedRole == "home" && sess.away != nil {
		sess.away.WriteJSON(endMsg) //nolint:errcheck
	}
}

func generateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
