package sniff

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"time"
)

// SniffTLS extracts the SNI (Server Name Indication) from a TLS ClientHello message.
// Returns the server name, or "" if the data is not a valid ClientHello or contains no SNI.
// Ported from github.com/sagernet/sing-box/common/sniff/tls.go
func SniffTLS(data []byte) string {
	var serverName string
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = tls.Server(&readOnlyConn{reader: bytes.NewReader(data)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			serverName = hello.ServerName
			return nil, nil
		},
	}).HandshakeContext(ctx)
	return serverName
}

// readOnlyConn is a minimal net.Conn adapter that only supports Read.
// All other methods are no-ops. Used to feed bytes into tls.Server for SNI extraction.
type readOnlyConn struct {
	reader *bytes.Reader
}

func (c *readOnlyConn) Read(b []byte) (int, error)         { return c.reader.Read(b) }
func (c *readOnlyConn) Write([]byte) (int, error)          { return 0, net.ErrClosed }
func (c *readOnlyConn) Close() error                       { return nil }
func (c *readOnlyConn) LocalAddr() net.Addr                { return nil }
func (c *readOnlyConn) RemoteAddr() net.Addr               { return nil }
func (c *readOnlyConn) SetDeadline(time.Time) error        { return nil }
func (c *readOnlyConn) SetReadDeadline(time.Time) error    { return nil }
func (c *readOnlyConn) SetWriteDeadline(time.Time) error   { return nil }
