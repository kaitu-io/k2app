package wire

import (
	"testing"

	"github.com/apernet/quic-go"
)

func TestNewQUICClientFromConn_FactoryExists(t *testing.T) {
	// Compile-time check: verify the function signature exists.
	// Full integration test requires a live QUIC connection.
	var _ func(*quic.Conn, *quic.Transport, Config) *QUICClient = NewQUICClientFromConn
}

func TestNewQUICServerFromListener_FactoryExists(t *testing.T) {
	var _ func(*quic.Listener) *QUICServer = NewQUICServerFromListener
}
