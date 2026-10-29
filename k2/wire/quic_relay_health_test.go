package wire

import (
	"context"
	"errors"
	"testing"
	"time"
)

// markRelayUnhealthy drives the datagram mux past relayTimeoutThreshold and
// returns it, so callers can assert on the mux afterwards.
func markRelayUnhealthy(t *testing.T, client *QUICClient) *quicUDPMux {
	t.Helper()
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramRelayHealthy() {
		t.Fatal("mux should be unhealthy after relayTimeoutThreshold timeouts")
	}
	return mux
}

// TestQUICClient_DialUDP_RelayUnhealthy_ReturnsFallback verifies that when
// relay datagrams become unhealthy, DialUDP surfaces ErrDatagramRelayUnhealthy
// so the caller can fall back to stream-UDP. Relay-layer metrics are
// observability only — they must never drive a wire-layer action (reconnect,
// transport switch).
func TestQUICClient_DialUDP_RelayUnhealthy_ReturnsFallback(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	markRelayUnhealthy(t, client)

	if _, err := client.DialUDP(ctx, "8.8.8.8:53"); !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}
}

// TestQUICClient_DialUDP_RelayUnhealthy_IgnoresTCPHealth verifies the fallback
// verdict is independent of TCP stream health. TCP health gates destructive
// wire-layer actions, not the relay-unhealthy report.
func TestQUICClient_DialUDP_RelayUnhealthy_IgnoresTCPHealth(t *testing.T) {
	for _, tc := range []struct {
		name          string
		lastTCPSucces int64
	}{
		{"tcp never succeeded", 0},
		{"tcp recently healthy", time.Now().UnixNano()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, cleanup := setupQUICPair(t)
			defer cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := client.connect(ctx); err != nil {
				t.Fatalf("connect: %v", err)
			}

			client.lastTCPSuccessAt.Store(tc.lastTCPSucces)
			markRelayUnhealthy(t, client)

			if _, err := client.DialUDP(ctx, "8.8.8.8:53"); !errors.Is(err, ErrDatagramRelayUnhealthy) {
				t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
			}
		})
	}
}

// TestQuicUDPMux_RecordRecvClearsTimeouts verifies a single successful receive
// clears the timeout streak. There is no sustained-recovery gate: the streak
// counter that used to guard a hop-counter reset went away with port hopping,
// so any receive means the datagram path is alive again.
func TestQuicUDPMux_RecordRecvClearsTimeouts(t *testing.T) {
	mux := &quicUDPMux{}

	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramRelayHealthy() {
		t.Fatal("mux should be unhealthy after relayTimeoutThreshold timeouts")
	}

	mux.recordRecv()

	if !mux.datagramRelayHealthy() {
		t.Fatal("a single recv must restore health")
	}
	if got := mux.relayTimeouts.Load(); got != 0 {
		t.Fatalf("relayTimeouts = %d after recv, want 0", got)
	}
}

// TestQuicUDPConn_WriteTo_LargePayloadUsesOverflow verifies that when SendDatagram
// returns DatagramTooLargeError, the packet is transparently sent via the overflow
// stream instead of failing. Overflow is NOT counted as a timeout (it's not a network problem).
func TestQuicUDPConn_WriteTo_LargePayloadUsesOverflow(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get a working PacketConn
	pconn, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer pconn.Close()

	// Record mux state before
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	timeoutsBefore := mux.relayTimeouts.Load()

	// Send a payload that exceeds the peer's MaxDatagramFrameSize.
	// quic-go caps MaxDatagramFrameSize at MaxPacketBufferSize (~64KB).
	// 70KB payload + UDPFrame overhead will exceed any reasonable limit,
	// triggering overflow fallback.
	largePayload := make([]byte, 70000)
	_, werr := pconn.WriteTo(largePayload, nil)
	if werr != nil {
		t.Fatalf("WriteTo should succeed via overflow, got: %v", werr)
	}

	// Verify: overflow is NOT counted as a timeout (it's not a network failure)
	timeoutsAfter := mux.relayTimeouts.Load()
	if timeoutsAfter != timeoutsBefore {
		t.Fatalf("relayTimeouts: before=%d after=%d, want unchanged (overflow is not a timeout)", timeoutsBefore, timeoutsAfter)
	}
}
