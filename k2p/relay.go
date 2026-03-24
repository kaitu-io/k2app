package k2p

import (
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

// RelayConn wraps a WebSocket connection to implement net.PacketConn.
// Each WS binary message = one UDP datagram.
type RelayConn struct {
	ws           *websocket.Conn
	peerAddr     net.Addr // synthetic peer address
	readDeadline time.Time
	mu           deadlock.Mutex // protects ws.WriteMessage
}

// DialRelay connects to the relay server at wsURL and returns a RelayConn.
func DialRelay(wsURL string) (*RelayConn, error) {
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("k2p: dial relay: %w", err)
	}
	return &RelayConn{
		ws:       ws,
		peerAddr: &net.UDPAddr{IP: net.IPv4zero, Port: 0},
	}, nil
}

// ReadFrom reads a binary WS message and returns it with the synthetic peer address.
func (c *RelayConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	_, msg, err := c.ws.ReadMessage()
	if err != nil {
		return 0, nil, fmt.Errorf("k2p: relay read: %w", err)
	}
	n = copy(p, msg)
	return n, c.peerAddr, nil
}

// WriteTo writes p as a single binary WS message. addr is ignored.
func (c *RelayConn) WriteTo(p []byte, _ net.Addr) (n int, err error) {
	c.mu.Lock()
	err = c.ws.WriteMessage(websocket.BinaryMessage, p)
	c.mu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("k2p: relay write: %w", err)
	}
	return len(p), nil
}

// Close closes the underlying WebSocket connection.
func (c *RelayConn) Close() error {
	return c.ws.Close()
}

// LocalAddr returns an empty UDP address (relay connections have no local UDP addr).
func (c *RelayConn) LocalAddr() net.Addr {
	return &net.UDPAddr{}
}

// SetDeadline sets the read deadline.
func (c *RelayConn) SetDeadline(t time.Time) error {
	c.readDeadline = t
	return c.ws.SetReadDeadline(t)
}

// SetReadDeadline sets the read deadline on the underlying WebSocket connection.
func (c *RelayConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	return c.ws.SetReadDeadline(t)
}

// SetWriteDeadline is a no-op; relay writes are not deadline-gated.
func (c *RelayConn) SetWriteDeadline(_ time.Time) error {
	return nil
}
