package wire

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Errors
var (
	ErrNotConnected   = errors.New("wire: not connected")
	ErrUDPNotSupported = errors.New("wire: UDP not supported by server")
)

// Dialer is the k2v5 client transport interface.
// It manages both QUIC and TCP-WS transports internally.
// Transport selection is transparent to callers.
type Dialer interface {
	DialTCP(ctx context.Context, addr string) (net.Conn, error)
	DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
	Close() error
}

// Listener is the k2v5 server transport interface.
// It manages QUIC/H3 and TCP-WS listeners on a shared port.
type Listener interface {
	AcceptTCP(ctx context.Context) (conn net.Conn, addr string, err error)
	ServeUDP(ctx context.Context, handler UDPHandler) error
	Close() error
}

// UDPHandler processes incoming UDP sessions on the server side.
type UDPHandler interface {
	HandleUDP(conn net.PacketConn, addr string)
}

// UDPHandlerFunc is a convenience adapter for UDPHandler.
type UDPHandlerFunc func(conn net.PacketConn, addr string)

// HandleUDP implements UDPHandler.
func (f UDPHandlerFunc) HandleUDP(conn net.PacketConn, addr string) { f(conn, addr) }

// Config holds parsed k2v5 URL configuration.
type Config struct {
	UDID        string
	Token       string
	Host        string
	Port        int
	ECH         string
	Fingerprint string
	HopStart    int
	HopEnd      int
	Insecure    bool   // skip TLS certificate verification (for self-signed certs)
	Pin         string // pin=sha256:<rsa>,sha256:<ec> — public key pin hashes
}

// ParseURL parses a k2v5:// URL into Config.
func ParseURL(raw string) (Config, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Config{}, fmt.Errorf("wire: parse URL: %w", err)
	}

	if u.Scheme != "k2v5" {
		return Config{}, fmt.Errorf("wire: unsupported scheme %q, want k2v5", u.Scheme)
	}

	if u.User == nil {
		return Config{}, errors.New("wire: missing auth (udid:token@)")
	}

	udid := u.User.Username()
	token, _ := u.User.Password()
	if udid == "" || token == "" {
		return Config{}, errors.New("wire: missing udid or token in auth")
	}

	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		return Config{}, errors.New("wire: missing port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Config{}, fmt.Errorf("wire: invalid port %q: %w", portStr, err)
	}

	cfg := Config{
		UDID:        udid,
		Token:       token,
		Host:        host,
		Port:        port,
		Fingerprint: "chrome",
	}

	q := u.Query()
	if v := q.Get("ech"); v != "" {
		cfg.ECH = v
	}
	if v := q.Get("fp"); v != "" {
		cfg.Fingerprint = v
	}
	if v := q.Get("insecure"); v == "1" || v == "true" {
		cfg.Insecure = true
	}
	if v := q.Get("pin"); v != "" {
		cfg.Pin = v
	}
	if v := q.Get("hop"); v != "" {
		start, end, err := parseHopRange(v)
		if err != nil {
			return Config{}, fmt.Errorf("wire: invalid hop range %q: %w", v, err)
		}
		cfg.HopStart = start
		cfg.HopEnd = end
	}

	return cfg, nil
}

// ParsePins parses the pin URL parameter into a list of SHA-256 hashes.
// Format: "sha256:<urlsafe_b64>,sha256:<urlsafe_b64>"
// Returns nil if pin is empty.
func ParsePins(pin string) ([][32]byte, error) {
	if pin == "" {
		return nil, nil
	}

	parts := strings.Split(pin, ",")
	hashes := make([][32]byte, 0, len(parts))
	for _, part := range parts {
		if !strings.HasPrefix(part, "sha256:") {
			return nil, fmt.Errorf("wire: pin %q missing sha256: prefix", part)
		}
		b64 := strings.TrimPrefix(part, "sha256:")
		decoded, err := base64.URLEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("wire: pin %q invalid base64: %w", part, err)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("wire: pin %q hash length %d, want 32", part, len(decoded))
		}
		var h [32]byte
		copy(h[:], decoded)
		hashes = append(hashes, h)
	}
	return hashes, nil
}

func parseHopRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("expected start-end format")
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if start >= end {
		return 0, 0, errors.New("start must be less than end")
	}
	return start, end, nil
}
