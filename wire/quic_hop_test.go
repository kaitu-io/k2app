package wire

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// setupQUICPairWithHop creates a QUIC pair where the client has port hopping
// configured. hopStart/hopEnd bracket the server port so hopPort() always
// reconnects to the same server (deterministic for testing).
func setupQUICPairWithHop(t *testing.T) (*QUICClient, *QUICServer, func()) {
	t.Helper()
	client, server, cleanup := setupQUICPair(t)

	// Set hop range to [serverPort, serverPort+1) so hopPort() always
	// returns serverPort — reconnect after hop succeeds deterministically.
	_, portStr, _ := net.SplitHostPort(client.serverAddr)
	port, _ := strconv.Atoi(portStr)
	client.hopStart = port
	client.hopEnd = port + 1

	return client, server, cleanup
}

// TestQUICClient_DialUDP_HopBeforeFallback verifies that when datagrams become
// unhealthy, DialUDP hops port (resetConnection + reconnect) instead of
// immediately returning ErrDatagramUnhealthy.
func TestQUICClient_DialUDP_HopBeforeFallback(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	// Establish initial connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Make datagram path unhealthy
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < datagramTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramHealthy() {
		t.Fatal("mux should be unhealthy")
	}

	// DialUDP should hop (reset + reconnect) instead of returning error
	pc, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP should succeed after hop, got: %v", err)
	}
	defer pc.Close()

	if client.datagramHopAttempts.Load() != 1 {
		t.Fatalf("datagramHopAttempts = %d, want 1", client.datagramHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_HopExhausted verifies that after maxDatagramHops
// unsuccessful hops, DialUDP returns ErrDatagramUnhealthy.
func TestQUICClient_DialUDP_HopExhausted(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Each iteration: connect → poison mux → DialUDP triggers hop
	for i := int32(0); i < maxDatagramHops; i++ {
		if _, err := client.connect(ctx); err != nil {
			t.Fatalf("connect (hop %d): %v", i, err)
		}
		client.mu.Lock()
		mux := client.udpMux
		client.mu.Unlock()
		for j := int32(0); j < datagramTimeoutThreshold; j++ {
			mux.recordTimeout()
		}
	}

	// Now hops are at maxDatagramHops-1 from previous rounds, but the connect
	// above established a new healthy mux. We need to poison it one more time.
	// Actually, let's re-think: each DialUDP call in the loop above would do
	// the hop. Let me use a simpler approach.

	// Reset and do it properly: pre-set hop attempts to max
	client.datagramHopAttempts.Store(maxDatagramHops)

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for j := int32(0); j < datagramTimeoutThreshold; j++ {
		mux.recordTimeout()
	}

	// DialUDP should return ErrDatagramUnhealthy (hops exhausted)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramUnhealthy", err)
	}

	if client.datagramHopAttempts.Load() != maxDatagramHops {
		t.Fatalf("datagramHopAttempts = %d, want %d", client.datagramHopAttempts.Load(), maxDatagramHops)
	}
}

// TestQUICClient_DialUDP_HopResetOnRecv verifies that a successful datagram
// receive resets the hop counter to zero.
func TestQUICClient_DialUDP_HopResetOnRecv(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Simulate 2 prior hops
	client.datagramHopAttempts.Store(2)

	// A datagram receive should reset hop counter
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	mux.recordRecv()

	if client.datagramHopAttempts.Load() != 0 {
		t.Fatalf("datagramHopAttempts = %d, want 0 after recordRecv", client.datagramHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_NoHopWithoutConfig verifies that without hop config,
// unhealthy datagrams immediately return ErrDatagramUnhealthy (no hop attempt).
func TestQUICClient_DialUDP_NoHopWithoutConfig(t *testing.T) {
	client, _, cleanup := setupQUICPair(t) // no hop config
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Make datagram path unhealthy
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < datagramTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// Should immediately return ErrDatagramUnhealthy (no hop)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramUnhealthy", err)
	}

	if client.datagramHopAttempts.Load() != 0 {
		t.Fatalf("datagramHopAttempts = %d, want 0 (no hop without config)", client.datagramHopAttempts.Load())
	}
}
