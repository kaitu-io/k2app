// wire/quic_p2p.go
//
// Factory functions for creating QUICClient/QUICServer from pre-established
// connections (k2p NAT traversal). These stay in the wire package to access
// unexported fields while respecting encapsulation.
package wire

import (
	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/k2/wire/k2cc"
)

// NewQUICClientFromConn creates a QUICClient backed by an existing QUIC
// connection (e.g., from NAT-traversed hole punch). Initializes K2CC
// congestion control, session allocator, and UDP mux — the same setup
// that QUICClient.connect() does at wire/quic.go:453-472.
func NewQUICClientFromConn(conn *quic.Conn, transport *quic.Transport, cfg Config) *QUICClient {
	c := NewQUICClient(cfg, nil) // k2p data plane has no Center cookie flow

	sender := k2cc.NewCCSenderGFWMode(k2cc.DefaultMaxRate)
	conn.SetCongestionControl(sender)

	c.mu.Lock()
	c.conn = conn
	c.transport = transport
	c.ccSender = sender
	c.alloc = NewSessionIDAllocator()
	c.udpMux = newQuicUDPMux(conn)
	c.mu.Unlock()

	go c.acceptOverflowStreams(conn, c.udpMux)

	return c
}

// NewQUICServerFromListener creates a QUICServer and starts accepting
// connections from the given listener. Convenience wrapper combining
// NewQUICServer() + Listen().
func NewQUICServerFromListener(ln *quic.Listener) *QUICServer {
	s := NewQUICServer()
	s.Listen(ln)
	return s
}

// NewQUICServerFromConn creates a QUICServer that handles an already-established
// QUIC connection (e.g., after peer auth on a punched connection). Also starts
// accepting new connections from ln for future peers.
func NewQUICServerFromConn(conn *quic.Conn, ln *quic.Listener) *QUICServer {
	s := NewQUICServer()
	go s.handleConn(conn)
	if ln != nil {
		s.Listen(ln)
	}
	return s
}
