package wire

import (
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/apernet/quic-go"
)

// DefaultQUICConfig returns the QUIC configuration for k2v5 transport.
func DefaultQUICConfig() *quic.Config {
	return &quic.Config{
		EnableDatagrams:    true,
		MaxIdleTimeout:     30 * time.Second,
		KeepAlivePeriod:    10 * time.Second,
		MaxIncomingStreams: 256,
	}
}

// quicStreamConn wraps a quic.Stream to satisfy net.Conn.
type quicStreamConn struct {
	quic.Stream
	local  net.Addr
	remote net.Addr
}

func (c *quicStreamConn) LocalAddr() net.Addr  { return c.local }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.remote }

// QUICClient is the QUIC/H3 client transport.
type QUICClient struct {
	cfg        Config
	store      *TicketStore
	serverAddr string
	hopStart   int
	hopEnd     int
}

// NewQUICClient creates a new QUIC client transport.
func NewQUICClient(cfg Config, store *TicketStore) *QUICClient {
	return &QUICClient{
		cfg:        cfg,
		store:      store,
		serverAddr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		hopStart:   cfg.HopStart,
		hopEnd:     cfg.HopEnd,
	}
}

// hopEnabled returns true if port hopping is configured.
func (c *QUICClient) hopEnabled() bool {
	return c.hopStart > 0 && c.hopEnd > 0 && c.hopStart < c.hopEnd
}

// hopPort returns a random port in the hop range.
func (c *QUICClient) hopPort() int {
	return c.hopStart + rand.Intn(c.hopEnd-c.hopStart)
}

// QUICServer is the QUIC/H3 server transport.
type QUICServer struct {
	store *TicketStore
}

// NewQUICServer creates a new QUIC server transport.
func NewQUICServer(store *TicketStore) *QUICServer {
	return &QUICServer{store: store}
}
