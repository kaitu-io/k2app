package k2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
)

// WSMessage is a WebSocket message for ICE candidate exchange.
// Mirrors signal.WSMessage — client-side copy to avoid importing the server package.
type WSMessage struct {
	Type      string `json:"type"`               // "session-start", "credentials", "candidate", "gathering-done", "session-end"
	Session   string `json:"session,omitempty"`   // session ID
	Ufrag     string `json:"ufrag,omitempty"`     // ICE ufrag
	Pwd       string `json:"pwd,omitempty"`       // ICE pwd
	Candidate string `json:"candidate,omitempty"` // ICE candidate string
}

// TURNCredentials holds temporary TURN REST API credentials for ICE relay.
type TURNCredentials struct {
	URLs     []string `json:"urls"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int      `json:"ttl"`
}

// RegisterRequest is sent by home to register its address.
type RegisterRequest struct {
	UDID     string   `json:"udid"`
	Networks []string `json:"networks,omitempty"` // advertised CIDR networks
	DNS      string   `json:"dns,omitempty"`      // advertised DNS server address
}

// PeerInfo is returned by lookup.
type PeerInfo struct {
	UDID     string           `json:"udid"`
	Networks []string         `json:"networks"`
	DNS      string           `json:"dns"`
	Online   bool             `json:"online"`
	TURN     *TURNCredentials `json:"turn,omitempty"`
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
			return nil, fmt.Errorf("k2p: marshal: %w", err)
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
		return nil, fmt.Errorf("k2p: build request: %w", err)
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	return c.http.Do(httpReq)
}

// Register reports this peer's presence to the API server.
func (c *SignalClient) Register(ctx context.Context, req RegisterRequest) error {
	resp, err := c.doJSON(ctx, "POST", "/k2p/register", req)
	if err != nil {
		return fmt.Errorf("k2p: register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("k2p: register: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Claim asserts ownership of a UDID and returns a registration token.
// Returns an error wrapping a 409 status if the UDID is already claimed
// by a different token.
func (c *SignalClient) Claim(ctx context.Context, udid string) (string, error) {
	body := struct {
		UDID string `json:"udid"`
	}{UDID: udid}
	resp, err := c.doJSON(ctx, "POST", "/k2p/claim", body)
	if err != nil {
		return "", fmt.Errorf("k2p: claim: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return "", fmt.Errorf("k2p: claim: UDID %q already claimed", udid)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("k2p: claim: HTTP %d", resp.StatusCode)
	}
	var result struct {
		RegToken string `json:"reg_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("k2p: claim: decode: %w", err)
	}
	return result.RegToken, nil
}

// Lookup queries the API server for a peer's info.
func (c *SignalClient) Lookup(ctx context.Context, udid string) (PeerInfo, error) {
	resp, err := c.doJSON(ctx, "GET", "/k2p/lookup/"+udid, nil)
	if err != nil {
		return PeerInfo{}, fmt.Errorf("k2p: lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return PeerInfo{}, fmt.Errorf("k2p: peer %q not found", udid)
	}
	if resp.StatusCode != 200 {
		return PeerInfo{}, fmt.Errorf("k2p: lookup: HTTP %d", resp.StatusCode)
	}
	var info PeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return PeerInfo{}, fmt.Errorf("k2p: decode lookup: %w", err)
	}
	return info, nil
}

// CandidateExchange manages ICE candidate exchange over a WebSocket connection.
// The read pump goroutine demuxes incoming messages into inCh and credsCh.
type CandidateExchange struct {
	conn     *websocket.Conn
	session  string
	inCh     chan WSMessage
	credsCh  chan WSMessage
	done     chan struct{}
	closeOnce sync.Once
	writeMu  deadlock.Mutex // protects conn.WriteJSON
	ownsConn bool           // true = Close() closes conn; false = shared conn (Home-side)
}

func newCandidateExchange(conn *websocket.Conn, session string) *CandidateExchange {
	e := &CandidateExchange{
		conn:     conn,
		session:  session,
		inCh:     make(chan WSMessage, 16),
		credsCh:  make(chan WSMessage, 1),
		done:     make(chan struct{}),
		ownsConn: true,
	}
	safego.Go(func() {
		defer close(e.inCh)
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "credentials" {
				select {
				case e.credsCh <- msg:
				default:
				}
			}
			select {
			case e.inCh <- msg:
			case <-e.done:
				return
			}
		}
	})
	return e
}

// Session returns the session ID for this exchange.
func (e *CandidateExchange) Session() string { return e.session }

// SendCandidate sends an ICE candidate to the remote peer.
func (e *CandidateExchange) SendCandidate(candidate string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.conn.WriteJSON(WSMessage{
		Type:      "candidate",
		Session:   e.session,
		Candidate: candidate,
	})
}

// SendCredentials sends ICE credentials (ufrag/pwd) to the remote peer.
func (e *CandidateExchange) SendCredentials(ufrag, pwd string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.conn.WriteJSON(WSMessage{
		Type:    "credentials",
		Session: e.session,
		Ufrag:   ufrag,
		Pwd:     pwd,
	})
}

// Incoming returns a channel of incoming messages for this exchange.
func (e *CandidateExchange) Incoming() <-chan WSMessage { return e.inCh }

// Close stops the exchange. If ownsConn is true, also closes the WebSocket.
// Safe for concurrent calls via sync.Once.
func (e *CandidateExchange) Close() error {
	e.closeOnce.Do(func() { close(e.done) })
	if e.ownsConn {
		return e.conn.Close()
	}
	return nil
}

// waitForCredentials blocks until the remote peer sends ICE credentials.
func waitForCredentials(ctx context.Context, exchange *CandidateExchange) (ufrag, pwd string, err error) {
	select {
	case msg := <-exchange.credsCh:
		return msg.Ufrag, msg.Pwd, nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// ConnectHome opens a WebSocket to the signal server as the Away peer,
// waits for session-start, and returns a CandidateExchange for ICE negotiation.
func (c *SignalClient) ConnectHome(ctx context.Context, homeUDID string) (*CandidateExchange, error) {
	base := c.baseURL
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	wsURL := base + "/k2p/ws?udid=" + homeUDID + "&role=away"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("k2p: away WS dial: %w", err)
	}

	var startMsg WSMessage
	if err := conn.ReadJSON(&startMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("k2p: read session-start: %w", err)
	}

	return newCandidateExchange(conn, startMsg.Session), nil
}

// ListenHome opens a WebSocket to the signal server as the Home peer,
// demuxes incoming sessions, and returns a channel of CandidateExchange
// (one per incoming Away peer connection attempt).
func (c *SignalClient) ListenHome(ctx context.Context, udid string) (<-chan *CandidateExchange, error) {
	base := c.baseURL
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	wsURL := base + "/k2p/ws?udid=" + udid + "&token=" + c.token + "&role=home"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("k2p: home WS dial: %w", err)
	}

	ch := make(chan *CandidateExchange, 4)

	safego.Go(func() {
		defer close(ch)
		defer conn.Close()
		exchanges := make(map[string]*CandidateExchange)
		defer func() {
			// Close inCh for all remaining exchanges so consumers
			// (handleAwaySession's for-range loop) don't block forever.
			for _, ex := range exchanges {
				close(ex.inCh)
			}
		}()

		for {
			if ctx.Err() != nil {
				return
			}
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Type {
			case "session-start":
				ex := &CandidateExchange{
					conn:     conn,
					session:  msg.Session,
					inCh:     make(chan WSMessage, 16),
					credsCh:  make(chan WSMessage, 1),
					done:     make(chan struct{}),
					ownsConn: false, // shared Home WS
				}
				exchanges[msg.Session] = ex
				select {
				case ch <- ex:
				case <-ctx.Done():
					return
				}
			case "candidate", "credentials", "gathering-done":
				ex, ok := exchanges[msg.Session]
				if !ok {
					continue
				}
				if msg.Type == "credentials" {
					select {
					case ex.credsCh <- msg:
					default:
					}
				}
				select {
				case ex.inCh <- msg:
				case <-ex.done:
					delete(exchanges, msg.Session)
				}
			case "session-end":
				if ex, ok := exchanges[msg.Session]; ok {
					close(ex.inCh)
					delete(exchanges, msg.Session)
				}
			}
		}
	})

	return ch, nil
}
