package wire

import (
	"fmt"
	"time"

	smux "github.com/xtaci/smux/v2"
)

// DefaultSmuxConfig returns the smux configuration for k2v5 TCP-WS transport.
func DefaultSmuxConfig() *smux.Config {
	return &smux.Config{
		KeepAliveInterval: 10 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
		MaxFrameSize:      32768,  // 32 KB, matches WebSocket message limit
		MaxReceiveBuffer:  262144, // 256 KB
		MaxStreamBuffer:   65536,  // 64 KB per stream
	}
}

// TCPWSClient is the TCP-WS + smux client transport.
type TCPWSClient struct {
	cfg        Config
	store      *TicketStore
	serverAddr string
}

// NewTCPWSClient creates a new TCP-WS client transport.
func NewTCPWSClient(cfg Config, store *TicketStore) *TCPWSClient {
	return &TCPWSClient{
		cfg:        cfg,
		store:      store,
		serverAddr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
	}
}

// TCPWSServer is the TCP-WS + smux server transport.
type TCPWSServer struct {
	store *TicketStore
}

// NewTCPWSServer creates a new TCP-WS server transport.
func NewTCPWSServer(store *TicketStore) *TCPWSServer {
	return &TCPWSServer{store: store}
}
