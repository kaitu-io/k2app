package wire

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Errors
var (
	ErrNotConnected      = errors.New("wire: not connected")
	ErrUDPNotSupported   = errors.New("wire: UDP not supported by server")
	ErrDatagramRelayUnhealthy = errors.New("wire: datagram relay path unhealthy")
)

// DirectDialer is the interface for physical-interface-bound dialing.
// Implemented by directdial.Dialer and protectedDialer; nil means no binding (default net.Dial).
type DirectDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	ListenPacket(network, addr string) (net.PacketConn, error)
}

// DialerController is an optional interface that DirectDialer implementations
// can implement to provide a syscall control function for raw socket operations
// (e.g. DNS direct queries that need socket protection on Android).
type DialerController interface {
	ControlFunc() func(network, address string, c syscall.RawConn) error
}

// WireStats is a point-in-time snapshot of wire transport metrics.
type WireStats struct {
	Transport   string        // "quic" or "tcpws"
	PacketLossRate float64       // 0.0-1.0 (QUIC only)
	Throughput     float64       // bps (QUIC only)
	SendingRate    float64       // bps (QUIC only)
	RTT            time.Duration // smoothed (QUIC only)
	MinRTT         time.Duration // minimum observed (QUIC only)
	Cwnd           int64         // bytes (QUIC only)
	Mode           int           // K2CCMode (QUIC only)
	MIID           uint64        // Monitor Interval ID (QUIC only)
	Epsilon        float64       // AIMD-ε adaptive step size (QUIC only)
	TxBytes        int64
	RxBytes        int64
	WireFallback   bool // TCP-WS serving traffic due to QUIC failure

	// Relay-layer observability (NOT for wire-layer decisions).
	RelayUDPTimeouts int32 // consecutive timeout count
	PortHopAttempts  int32 // port hop attempts

	// UDP drop counters are cumulative since the connection was established,
	// not a per-interval delta — unlike RelayUDPTimeouts above (which resets
	// to 0 on every successful receive), nothing ever resets these. A large
	// value may be hours old; to judge current loss rate, diff two heartbeat
	// samples rather than reading the raw value.
	//
	// What 0 means, per transport — every path that silently discards a UDP
	// packet increments one of these, so 0 is always "nothing was dropped",
	// never "nobody counted":
	//
	//	                        UDPTxDrops              UDPRxDrops / NoSession
	//	QUIC datagram           send budget saturated   instrumented
	//	QUIC stream-UDP mux     n/a — always 0          instrumented
	//	TCP-WS smux mux         n/a — always 0          instrumented
	//
	// The two "n/a" cells are not gaps. A stream-backed transport has no
	// lossy send path: writeFrame either writes the frame or returns an
	// error, which tears the session down (visible as a session end, not as
	// a drop). Only the QUIC datagram path can accept a packet from the app
	// and then silently discard it, so only it can report a TX drop.
	// Transport (plus WireFallback) tells you which row you are reading.
	UDPTxDrops int64 // uplink datagrams dropped (send budget saturated)
	UDPRxDrops int64 // downlink packets dropped for a LIVE session (channel full)

	// UDPRxDropsNoSession counts downlink packets addressed to a session ID
	// the client no longer knows. Kept out of UDPRxDrops because it is
	// dominated by a benign tail — the peer keeps sending for a moment after
	// the app closed the socket or the idle reaper evicted the session, so
	// this scales with session churn, not with loss. A rising UDPRxDrops is
	// a stuttering user; a rising UDPRxDropsNoSession usually is not.
	UDPRxDropsNoSession int64
}

// StatsProvider is an optional interface for Dialers that expose metrics.
// Checked via type assertion — does not break the Dialer interface.
type StatsProvider interface {
	WireStats() WireStats
}

// Dialer is the k2v5 client transport interface.
// It manages both QUIC and TCP-WS transports internally.
// Transport selection is transparent to callers.
type Dialer interface {
	DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error)
	DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
	// DialStreamUDP opens a stream-backed UDP session (reliable, congestion-controlled).
	// Used for DNS queries where reliability matters more than low overhead.
	// QUIC: uses QUIC stream (not DATAGRAM). TCP-WS: same as DialUDP (already stream-based).
	DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error)
	// Reset clears cached connection state without closing the Dialer. After
	// Reset, the next DialTCP/DialUDP/DialStreamUDP establishes a fresh
	// connection. Called by TransportManager / engine on network change.
	// Implementations that have no cacheable state may make this a no-op.
	Reset()
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

// UDPSessionInfo provides diagnostic metadata for a UDP relay session.
// Implemented by serverUDPConn. Asserted by server.ProxyHandler for log attribution.
type UDPSessionInfo interface {
	UDPSessionID() uint32
	UDPRemoteAddr() string
	Logger() *slog.Logger
}

// UDPHandlerFunc is a convenience adapter for UDPHandler.
type UDPHandlerFunc func(conn net.PacketConn, addr string)

// HandleUDP implements UDPHandler.
func (f UDPHandlerFunc) HandleUDP(conn net.PacketConn, addr string) { f(conn, addr) }

// Config holds parsed k2v5 URL configuration.
type Config struct {
	UDID        string
	Token       string
	Host        string // TLS SNI domain (URL hostname) — NOT used for dialing when DialAddr is set
	Port        int
	ECH         string
	Fingerprint string
	HopStart    int
	HopEnd      int
	Insecure    bool   // skip TLS certificate verification (for self-signed certs)
	Pin         string // pin=sha256:<rsa>,sha256:<ec> — public key pin hashes
	IPv4        string // ip= parameter: raw IPv4 address for dialing
	IPv6        string // ip6= parameter: raw IPv6 address for dialing
	Transport   string // tp= parameter: "quic", "tcpws", or "" (auto, default)
	Mode        string // mode= parameter: "gateway" or "" (normal)
	DialAddr    string // Resolved dial address (set by engine: ip6 preferred > ip4). Not parsed from URL.
	Version     string // client app version for metadata attribution (set by engine from ClientConfig.ClientVersion). Not parsed from URL.
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
	if v := q.Get("ip"); v != "" {
		cfg.IPv4 = v
	}
	if v := q.Get("ip6"); v != "" {
		cfg.IPv6 = normalizeSslipIPv6(v)
	}
	if v := q.Get("tp"); v != "" {
		cfg.Transport = v
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

// normalizeSslipIPv6 converts an ip6= parameter value to raw IPv6.
// If the value is a sslip.io domain (e.g., "2001-db8--1.sslip.io"),
// it strips the suffix and converts dashes back to colons.
// Raw IPv6 values (e.g., "2001:db8::1") pass through unchanged.
func normalizeSslipIPv6(s string) string {
	if host, ok := strings.CutSuffix(s, ".sslip.io"); ok {
		return strings.ReplaceAll(host, "-", ":")
	}
	return s
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
