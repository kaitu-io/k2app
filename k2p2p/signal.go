package k2p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kaitu-io/k2/safego"
)

// RegisterRequest is sent by home to register its address.
type RegisterRequest struct {
	UDID    string `json:"udid"`
	Addr    string `json:"addr"`     // STUN-discovered public UDP address
	NATType string `json:"nat_type"` // "easy", "hard", "open"
}

// ConnectRequest is sent by away to request connection to a home peer.
type ConnectRequest struct {
	HomeUDID string `json:"home_udid"`
	AwayAddr string `json:"away_addr"`
	NATType  string `json:"nat_type"`
}

// PeerInfo is returned by lookup.
type PeerInfo struct {
	UDID      string    `json:"udid"`
	Addr      string    `json:"addr"`
	NATType   string    `json:"nat_type"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PunchNotify is received by home when away wants to connect.
type PunchNotify struct {
	AwayAddr string `json:"away_addr"`
	NATType  string `json:"nat_type"`
}

// SignalClient communicates with the central API server for peer discovery.
type SignalClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewSignalClient creates a signaling client for the given API server.
func NewSignalClient(baseURL, token string) *SignalClient {
	return &SignalClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *SignalClient) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("k2p2p: marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	var httpReq *http.Request
	var err error
	if reqBody != nil {
		httpReq, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	} else {
		httpReq, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("k2p2p: build request: %w", err)
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	return c.http.Do(httpReq)
}

// Register reports this peer's STUN-discovered address to the API server.
func (c *SignalClient) Register(ctx context.Context, req RegisterRequest) error {
	resp, err := c.doJSON(ctx, "POST", "/p2p/register", req)
	if err != nil {
		return fmt.Errorf("k2p2p: register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("k2p2p: register: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Lookup queries the API server for a peer's address.
func (c *SignalClient) Lookup(ctx context.Context, udid string) (PeerInfo, error) {
	resp, err := c.doJSON(ctx, "GET", "/p2p/lookup/"+udid, nil)
	if err != nil {
		return PeerInfo{}, fmt.Errorf("k2p2p: lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return PeerInfo{}, fmt.Errorf("k2p2p: peer %q not found", udid)
	}
	if resp.StatusCode != 200 {
		return PeerInfo{}, fmt.Errorf("k2p2p: lookup: HTTP %d", resp.StatusCode)
	}
	var info PeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return PeerInfo{}, fmt.Errorf("k2p2p: decode lookup: %w", err)
	}
	return info, nil
}

// ListenNotify opens a WebSocket to the API server and returns a channel
// of PunchNotify messages. The channel is closed when ctx is cancelled
// or the connection drops. Uses safego.Go for panic safety.
func (c *SignalClient) ListenNotify(ctx context.Context, udid string) (<-chan PunchNotify, error) {
	wsURL := c.baseURL + "/p2p/ws?udid=" + udid
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)

	// DialContext returns (*Conn, *http.Response, error) — discard Response.
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("k2p2p: websocket dial: %w", err)
	}

	ch := make(chan PunchNotify, 8)
	safego.Go(func() {
		defer close(ch)
		defer conn.Close()
		for {
			if ctx.Err() != nil {
				return
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var notify PunchNotify
			if err := json.Unmarshal(msg, &notify); err != nil {
				continue
			}
			select {
			case ch <- notify:
			case <-ctx.Done():
				return
			}
		}
	})

	return ch, nil
}

// RequestConnect asks the API server to notify a home peer that away
// wants to connect.
func (c *SignalClient) RequestConnect(ctx context.Context, req ConnectRequest) error {
	resp, err := c.doJSON(ctx, "POST", "/p2p/connect", req)
	if err != nil {
		return fmt.Errorf("k2p2p: connect request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("k2p2p: connect request: HTTP %d", resp.StatusCode)
	}
	return nil
}
