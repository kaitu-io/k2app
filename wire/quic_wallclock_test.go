package wire

import (
	"context"
	"testing"
	"time"
)

// TestQUICClient_WallClockStaleness_Normal verifies that connect() reuses
// the cached connection when the wall-clock gap is within MaxIdleTimeout.
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

// TestQUICClient_WallClockStaleness_Sleep verifies that connect() detects
// a stale connection when the wall-clock gap exceeds MaxIdleTimeout.
// Simulates macOS sleep by setting lastConnectWall far in the past.
func TestQUICClient_WallClockStaleness_Sleep(t *testing.T) {
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

	// Simulate macOS sleep: set lastConnectWall to 60 seconds ago.
	// This is what happens when Go runtime resumes after a 60s sleep —
	// wall clock shows 60s gap but Go timers think only ~1s passed.
	client.lastConnectWall.Store(time.Now().Add(-60 * time.Second).UnixNano())

	// Server: accept second stream (rebuilt connection)
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

	// Connection should have been rebuilt (different pointer)
	client.mu.Lock()
	connAfter := client.conn
	client.mu.Unlock()
	if connAfter == connBefore {
		t.Fatal("expected connection to be rebuilt after wall-clock staleness")
	}
}

// TestQUICClient_WallClockStaleness_FirstConnect verifies that the first
// connect() does not trigger staleness detection (lastConnectWall is zero).
func TestQUICClient_WallClockStaleness_FirstConnect(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx := context.Background()

	// Verify lastConnectWall starts at 0
	if got := client.lastConnectWall.Load(); got != 0 {
		t.Fatalf("lastConnectWall = %d, want 0 on fresh client", got)
	}

	// Server: accept stream
	go func() {
		conn, _, err := server.AcceptTCP(ctx)
		if err != nil {
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
	}()

	// First connect should succeed without any staleness issue
	conn1, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatal("first connect should succeed:", err)
	}
	conn1.Close()

	// lastConnectWall should now be set
	if got := client.lastConnectWall.Load(); got == 0 {
		t.Fatal("lastConnectWall should be set after first connect")
	}
}
