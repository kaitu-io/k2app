package signal

import (
	"time"

	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

type ClaimRequest struct {
	UDID string `json:"udid"`
}

type ClaimResponse struct {
	RegToken string `json:"reg_token"`
}

type RegisterRequest struct {
	UDID     string   `json:"udid"`
	Networks []string `json:"networks,omitempty"`
	DNS      string   `json:"dns,omitempty"`
}

type LookupResponse struct {
	UDID     string           `json:"udid"`
	Networks []string         `json:"networks"`
	DNS      string           `json:"dns"`
	Online   bool             `json:"online"`
	TURN     *TURNCredentials `json:"turn,omitempty"`
}

// WSMessage is a WebSocket message for ICE candidate exchange.
type WSMessage struct {
	Type      string `json:"type"`               // "session-start", "credentials", "candidate", "gathering-done", "session-end"
	Session   string `json:"session,omitempty"`   // session ID
	Ufrag     string `json:"ufrag,omitempty"`     // ICE ufrag
	Pwd       string `json:"pwd,omitempty"`       // ICE pwd
	Candidate string `json:"candidate,omitempty"` // ICE candidate string
}

type PeerEntry struct {
	UDID         string
	RegTokenHash string
	Networks     []string
	DNS          string
	UpdatedAt    time.Time
	WS           *websocket.Conn
	WriteMu      deadlock.Mutex // protects WS writes
}
