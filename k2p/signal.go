package k2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// RegisterRequest is sent by home to register its address.
type RegisterRequest struct {
	UDID     string   `json:"udid"`
	Addr     string   `json:"addr"`              // STUN-discovered public UDP address
	NATType  string   `json:"nat_type"`          // "easy", "hard", "open"
	Networks []string `json:"networks,omitempty"` // advertised CIDR networks
	DNS      string   `json:"dns,omitempty"`      // advertised DNS server address
}

// PeerInfo is returned by lookup.
type PeerInfo struct {
	UDID      string    `json:"udid"`
	Addr      string    `json:"addr"`
	NATType   string    `json:"nat_type"`
	Networks  []string  `json:"networks"`
	DNS       string    `json:"dns"`
	Online    bool      `json:"online"`
	UpdatedAt time.Time `json:"updated_at"`
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

// Register reports this peer's STUN-discovered address to the API server.
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

// Lookup queries the API server for a peer's address.
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

