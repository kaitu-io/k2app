package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	utls "github.com/metacubex/utls"
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

// roundTripOverConn writes one HTTP/1.1 request over an already-established
// connection (plaintext or TLS-terminated) and reads the response. Uses
// Connection: close — one request per connection, no keep-alive. The caller
// owns conn's lifecycle (close after this returns).
func roundTripOverConn(conn net.Conn, req *RelayRequest, timeout time.Duration) (*RelayResponse, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	hreq, err := http.NewRequest(method, "https://"+req.CenterHost+req.Path, body)
	if err != nil {
		return nil, err
	}
	hreq.Host = req.CenterHost
	hreq.Close = true // Connection: close
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}

	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	if err := hreq.Write(conn); err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), hreq)
	if err != nil {
		return nil, err
	}
	return captureResponse(resp)
}

// RelayFetch sends one control-plane HTTP request to Center through a node
// (camouflage entry): TCP → uTLS(ECH + pin) → HTTP/1.1 round-trip. The node
// terminates TLS and reverse-proxies to Center (Phase 1). ECH is required
// (BuildClientTLSConfig errors otherwise) — it must travel in the descriptor.
func RelayFetch(ctx context.Context, req *RelayRequest) (*RelayResponse, error) {
	return relayFetchWithOverride(ctx, req, nil)
}

func relayFetchWithOverride(ctx context.Context, req *RelayRequest, override *tls.Config) (*RelayResponse, error) {
	conn, err := relayDial(ctx, req, override)
	if err != nil {
		slog.Warn("DIAG: relay-fetch-fail", "centerHost", req.CenterHost, "path", req.Path, "nodeIP", req.IP, "err", err)
		return nil, err
	}
	defer conn.Close()
	resp, err := roundTripOverConn(conn, req, 15*time.Second)
	if err != nil {
		slog.Warn("DIAG: relay-fetch-fail", "centerHost", req.CenterHost, "path", req.Path, "nodeIP", req.IP, "err", err)
		return nil, err
	}
	slog.Info("DIAG: relay-fetch", "centerHost", req.CenterHost, "method", req.Method, "path", req.Path, "status", resp.Status, "nodeIP", req.IP)
	return resp, nil
}

// relayDial dials the node and completes the camouflage uTLS handshake.
// override != nil → use it directly (test / self-signed, skips ECH), mirroring
// tcpws.go:327 / probe.go:149. Production: build from BuildClientTLSConfig and
// ENFORCE pin via VerifyPeerCertificate (tcpws template omits this — see header).
func relayDial(ctx context.Context, req *RelayRequest, override *tls.Config) (net.Conn, error) {
	port := req.Port
	if port == 0 {
		port = 443
	}
	dialAddr := net.JoinHostPort(req.IP, strconv.Itoa(port))

	var tlsCfg *tls.Config
	clientHelloID := utls.HelloChrome_120_PQ
	if override != nil {
		tlsCfg = override.Clone()
	} else {
		var err error
		tlsCfg, clientHelloID, err = BuildClientTLSConfig(Config{
			Host:        req.CenterHost, // inner SNI
			ECH:         req.ECH,
			Pin:         req.Pin,
			Fingerprint: "chrome",
		})
		if err != nil {
			return nil, err
		}
	}

	d := &net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", dialAddr)
	if err != nil {
		return nil, fmt.Errorf("wire: relay TCP dial %s: %w", dialAddr, err)
	}

	utlsCfg := &utls.Config{
		ServerName:                     tlsCfg.ServerName,
		InsecureSkipVerify:             tlsCfg.InsecureSkipVerify,
		MinVersion:                     tlsCfg.MinVersion,
		MaxVersion:                     tlsCfg.MaxVersion,
		EncryptedClientHelloConfigList: tlsCfg.EncryptedClientHelloConfigList,
		// SECURITY: enforce pin/CA — tcpws.go:341 omits these, leaving pinned
		// connections unverified. Relay carries tokens/passwords through
		// third-party nodes, so the pin MUST actually run.
		RootCAs:               tlsCfg.RootCAs,
		VerifyPeerCertificate: tlsCfg.VerifyPeerCertificate,
	}
	uconn := utls.UClient(conn, utlsCfg, clientHelloID)
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("wire: relay uTLS handshake: %w", err)
	}
	return uconn, nil
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

// relayEnvelope mirrors the daemon/webapp {code,message,data} contract so the
// string boundary (gomobile appext + daemon) is uniform.
type relayEnvelope struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    *RelayResponse `json:"data,omitempty"`
}

// RelayFetchJSON is the string-in/string-out wrapper for the gomobile (appext)
// and daemon boundaries. It never panics and never returns an empty string.
func RelayFetchJSON(reqJSON string) string {
	var req RelayRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		return marshalEnvelope(relayEnvelope{Code: -1, Message: "bad relay request: " + err.Error()})
	}
	resp, err := RelayFetch(context.Background(), &req)
	if err != nil {
		return marshalEnvelope(relayEnvelope{Code: 502, Message: err.Error()})
	}
	return marshalEnvelope(relayEnvelope{Code: 0, Message: "ok", Data: resp})
}

func marshalEnvelope(e relayEnvelope) string {
	b, err := json.Marshal(e)
	if err != nil {
		return `{"code":-1,"message":"relay marshal error"}`
	}
	return string(b)
}
