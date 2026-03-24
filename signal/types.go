package signal

import (
	"time"

	"github.com/gorilla/websocket"
)

type ClaimRequest struct {
	UDID string `json:"udid"`
}

type ClaimResponse struct {
	RegToken string `json:"reg_token"`
}

type RegisterRequest struct {
	UDID     string   `json:"udid"`
	Addr     string   `json:"addr"`
	NATType  string   `json:"nat_type"`
	Networks []string `json:"networks"`
	DNS      string   `json:"dns"`
}

type ConnectRequest struct {
	HomeUDID string `json:"home_udid"`
	AwayAddr string `json:"away_addr"`
	NATType  string `json:"nat_type"`
}

type LookupResponse struct {
	UDID     string   `json:"udid"`
	Addr     string   `json:"addr"`
	NATType  string   `json:"nat_type"`
	Networks []string `json:"networks"`
	DNS      string   `json:"dns"`
	Online   bool     `json:"online"`
}

type PunchNotify struct {
	AwayAddr string `json:"away_addr"`
	NATType  string `json:"nat_type"`
}

type PeerEntry struct {
	UDID         string
	RegTokenHash string
	Addr         string
	NATType      string
	Networks     []string
	DNS          string
	UpdatedAt    time.Time
	WS           *websocket.Conn
}
