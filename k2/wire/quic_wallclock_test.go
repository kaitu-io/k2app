package wire

import (
	"context"
	"testing"
	"time"
)

// TestQUICClient_WallClockStaleness_Normal verifies that connect() reuses
// the cached connection when called again shortly after.
func TestQUICClient_WallClockStaleness_Normal(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx := context.Background()

	// Server: accept first stream
	go func() {
		conn, _, err := server.AcceptTCP(ctx)
		if err != nil {
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
	}()

	// Establish connection
	conn1, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn1.Close()

	// Record the cached QUIC connection
	client.mu.Lock()
	connBefore := client.conn
	client.mu.Unlock()
	if connBefore == nil {
		t.Fatal("expected cached connection")
	}

	// Wait a short time (well within 30s MaxIdleTimeout)
	time.Sleep(100 * time.Millisecond)

	// Server: accept second stream (same connection, no rebuild)
	go func() {
		conn, _, err := server.AcceptTCP(ctx)
		if err != nil {
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
	}()

	conn2, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn2.Close()

	// Same cached connection should be reused (no rebuild)
	client.mu.Lock()
	connAfter := client.conn
	client.mu.Unlock()
	if connAfter != connBefore {
		t.Fatal("expected same connection to be reused within MaxIdleTimeout")
	}
}
