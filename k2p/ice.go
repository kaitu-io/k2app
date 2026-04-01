package k2p

import (
	"net"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
)

// icePacketConn adapts *ice.Conn (net.Conn) to net.PacketConn for QUIC transport.
// ice.Conn is backed by UDP — Read() preserves datagram boundaries.
// WriteTo ignores addr, ReadFrom returns synthetic peer addr.
//
// If agent is non-nil, Close() also closes the Agent to stop its internal
// goroutines (connectivity checks, keepalive). The Agent must outlive the Conn
// so it is closed AFTER conn.Close().
type icePacketConn struct {
	conn     *ice.Conn
	peerAddr net.Addr
	agent    *ice.Agent // optional; closed on Close() to prevent goroutine leak
}

func newICEPacketConn(conn *ice.Conn, agent *ice.Agent) *icePacketConn {
	return &icePacketConn{
		conn:     conn,
		peerAddr: conn.RemoteAddr(),
		agent:    agent,
	}
}

func (c *icePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.conn.Read(p)
	return n, c.peerAddr, err
}

func (c *icePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.conn.Write(p)
}

func (c *icePacketConn) Close() error {
	connErr := c.conn.Close()
	if c.agent != nil {
		c.agent.Close()
	}
	return connErr
}
func (c *icePacketConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *icePacketConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *icePacketConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *icePacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// DefaultSTUNURLs returns public STUN server URIs for ICE candidate gathering.
func DefaultSTUNURLs() []*stun.URI {
	urls := []string{
		"stun:stun.l.google.com:19302",
		"stun:stun1.l.google.com:19302",
		"stun:stun.cloudflare.com:3478",
		"stun:stun.miwifi.com:3478",
		"stun:stun.chat.bilibili.com:3478",
	}
	var result []*stun.URI
	for _, u := range urls {
		parsed, err := stun.ParseURI(u)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

// NewICEAgent creates an ICE agent configured with STUN and optional TURN servers.
func NewICEAgent(cfg Config) (*ice.Agent, error) {
	urls := DefaultSTUNURLs()

	for _, turnURL := range cfg.TURNURLs {
		parsed, err := stun.ParseURI(turnURL)
		if err != nil {
			continue
		}
		parsed.Username = cfg.TURNUsername
		parsed.Password = cfg.TURNPassword
		urls = append(urls, parsed)
	}

	return ice.NewAgentWithOptions(
		ice.WithUrls(urls),
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
	)
}
