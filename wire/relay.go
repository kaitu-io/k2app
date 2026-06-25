package wire

import (
	"io"
	"net/http"
)

// RelayRequest is a single control-plane HTTP request to be sent to Center
// through a node (camouflage entry). The node descriptor (IP/Pin/ECH) and the
// HTTP request travel together. No hop field: the control-plane relay is fixed
// to TCP+TLS+ECH (no QUIC port-hopping). See antiblock spec §5.1/§5.2.
type RelayRequest struct {
	IP         string            `json:"ip"`         // node dial IP (v4 or v6 literal)
	Port       int               `json:"port"`       // node TLS port (0 → 443)
	Pin        string            `json:"pin"`        // sha256:<rsa>,sha256:<ec> public-key pin
	ECH        string            `json:"ech"`        // base64 ECH config list (required)
	CenterHost string            `json:"centerHost"` // inner SNI + Host header (e.g. k2.52j.me)
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
}

// RelayResponse is the captured HTTP response. Headers kept as map[string][]string
// so multi-valued headers (e.g. Set-Cookie) are lossless across the JSON boundary.
type RelayResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    string              `json:"body"`
}

// captureResponse drains an HTTP response into a RelayResponse and closes the body.
func captureResponse(resp *http.Response) (*RelayResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &RelayResponse{
		Status:  resp.StatusCode,
		Headers: map[string][]string(resp.Header),
		Body:    string(body),
	}, nil
}
