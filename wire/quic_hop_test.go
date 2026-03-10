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

// TestQUICClient_DialUDP_RelayUnhealthy_ReturnsFallback verifies that when
// relay datagrams become unhealthy, DialUDP returns ErrDatagramRelayUnhealthy
// without attempting port hops. Relay-layer metrics must not drive wire-layer
// decisions (port hop, reconnect).
func TestQUICClient_DialUDP_RelayUnhealthy_ReturnsFallback(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
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
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramRelayHealthy() {
		t.Fatal("mux should be unhealthy")
	}

	// DialUDP should return ErrDatagramRelayUnhealthy (no hop)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}

	if client.portHopAttempts.Load() != 0 {
		t.Fatalf("portHopAttempts = %d, want 0 (relay metrics must not trigger hop)", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_RelayUnhealthy_HopStateUntouched verifies that
// relay unhealthy DialUDP never modifies the hop counter, regardless of
// current hop state.
func TestQUICClient_DialUDP_RelayUnhealthy_HopStateUntouched(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Pre-set hop state to some value
	client.portHopAttempts.Store(2)

	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for j := int32(0); j < relayTimeoutThreshold; j++ {
		mux.recordTimeout()
	}

	// DialUDP should return ErrDatagramRelayUnhealthy without touching hop state
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}

	if client.portHopAttempts.Load() != 2 {
		t.Fatalf("portHopAttempts = %d, want 2 (unchanged)", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_HopResetOnLossRecovery verifies that hop counter
// resets only when sliding-window loss rate recovers below threshold,
// not on a single successful receive.
func TestQUICClient_DialUDP_HopResetOnLossRecovery(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	client.portHopAttempts.Store(2)
	client.portHopExhaustedAt.Store(time.Now().Add(-1 * time.Minute).UnixNano())

	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()

	// A single recv should NOT reset hop counter
	mux.recordRecv()
	if client.portHopAttempts.Load() != 2 {
		t.Fatalf("hopAttempts = %d, want 2 (single recv must not reset)", client.portHopAttempts.Load())
	}

	// Simulate good traffic (0% loss) with enough samples
	for i := 0; i < 20; i++ {
		mux.recordSend()
		mux.recordRecv()
	}
	mux.expireLossWindow()
	mux.evaluateRelayLossRate() // triggers hop reset (loss < 15%)

	if client.portHopAttempts.Load() != 0 {
		t.Fatalf("hopAttempts = %d, want 0 after loss recovery", client.portHopAttempts.Load())
	}
	if client.portHopExhaustedAt.Load() != 0 {
		t.Fatalf("portHopExhaustedAt = %d, want 0 after loss recovery", client.portHopExhaustedAt.Load())
	}
}

// TestQUICClient_DialUDP_HopNotResetWithHighLoss verifies that hop counter
// stays elevated when loss rate remains above recovery threshold.
func TestQUICClient_DialUDP_HopNotResetWithHighLoss(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	client.portHopAttempts.Store(2)

	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()

	// Simulate 30% loss (above 15% recovery threshold)
	for i := 0; i < 20; i++ {
		mux.recordSend()
		if i%10 < 7 {
			mux.recordRecv() // 70% recv = 30% loss
		}
	}
	mux.expireLossWindow()
	mux.evaluateRelayLossRate()

	if client.portHopAttempts.Load() != 2 {
		t.Fatalf("hopAttempts = %d, want 2 (30%% loss above recovery threshold)", client.portHopAttempts.Load())
	}
}

// TestQuicUDPConn_WriteTo_LargePayloadUsesOverflow verifies that when SendDatagram
// returns DatagramTooLargeError, the packet is transparently sent via the overflow
// stream instead of failing. The send is counted in relaySent (for throughput
// tracking) but NOT in relayTimeouts (it's not a network problem).
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
	sentBefore := mux.relaySent.Load()
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

	// Verify: send was counted but NOT as a timeout (overflow is not a network failure)
	sentAfter := mux.relaySent.Load()
	timeoutsAfter := mux.relayTimeouts.Load()

	if sentAfter != sentBefore+1 {
		t.Fatalf("relaySent: before=%d after=%d, want +1", sentBefore, sentAfter)
	}
	if timeoutsAfter != timeoutsBefore {
		t.Fatalf("relayTimeouts: before=%d after=%d, want unchanged (overflow is not a timeout)", timeoutsBefore, timeoutsAfter)
	}
}

// TestQUICClient_DialUDP_UnhealthyRelay_NoHop verifies that when relay UDP is
// unhealthy, DialUDP always returns ErrDatagramRelayUnhealthy regardless of TCP
// health status. Relay-layer metrics must never drive wire-layer port hops.
func TestQUICClient_DialUDP_UnhealthyRelay_NoHop(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
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
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// Ensure TCP is NOT recently healthy (no lastTCPSuccessAt)
	client.lastTCPSuccessAt.Store(0)

	// DialUDP should return ErrDatagramRelayUnhealthy, NOT trigger hop
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}

	// No hop should have been attempted
	if client.portHopAttempts.Load() != 0 {
		t.Fatalf("portHopAttempts = %d, want 0 (relay metrics must never trigger hop)", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_NoHopWithoutConfig verifies that without hop config,
// unhealthy datagrams immediately return ErrDatagramRelayUnhealthy (no hop attempt).
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
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// Should immediately return ErrDatagramRelayUnhealthy (no hop)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}

	if client.portHopAttempts.Load() != 0 {
		t.Fatalf("portHopAttempts = %d, want 0 (no hop without config)", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_RelayUnhealthy_NoRecoveryProbe verifies that relay
// UDP unhealthy state always returns ErrDatagramRelayUnhealthy regardless of prior
// hop exhaustion state. Relay metrics never trigger hop or recovery probes.
func TestQUICClient_DialUDP_RelayUnhealthy_NoRecoveryProbe(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Simulate prior hop exhaustion (doesn't matter anymore — relay never hops)
	client.portHopAttempts.Store(int32(3))
	client.portHopExhaustedAt.Store(time.Now().Add(-4 * time.Minute).UnixNano())

	// Make datagram path unhealthy
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// DialUDP should return ErrDatagramRelayUnhealthy (no hop, no recovery probe)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}

	// Hop state should be unchanged (relay path doesn't touch it)
	if got := client.portHopAttempts.Load(); got != int32(3) {
		t.Fatalf("portHopAttempts = %d, want %d (relay path must not modify hop state)", got, int32(3))
	}
}

// TestQUICClient_DialUDP_RelayUnhealthy_AlwaysFallback verifies that relay
// UDP unhealthy always returns ErrDatagramRelayUnhealthy, regardless of hop config
// or exhaustion state. This is the simplest case — relay metrics never trigger
// any wire-layer action.
func TestQUICClient_DialUDP_RelayUnhealthy_AlwaysFallback(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Make datagram path unhealthy with both timeout and loss
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrDatagramRelayUnhealthy", err)
	}
}

// TestQUICClient_DialUDP_HopRecoveryResetOnNetworkChange verifies that
// ResetConnections() clears the portHopExhaustedAt timestamp along with
// the hop counter, giving QUIC a fully fresh start on the new network.
func TestQUICClient_DialUDP_HopRecoveryResetOnNetworkChange(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	// Simulate a prior exhaustion
	client.portHopAttempts.Store(int32(3))
	client.portHopExhaustedAt.Store(time.Now().Add(-1 * time.Minute).UnixNano())

	// Network change → ResetConnections
	tm := NewTransportManager()
	tm.SetQUIC(client)
	tm.ResetConnections()

	if got := client.portHopAttempts.Load(); got != 0 {
		t.Fatalf("portHopAttempts = %d, want 0 (cleared by ResetConnections)", got)
	}
	if got := client.portHopExhaustedAt.Load(); got != 0 {
		t.Fatalf("portHopExhaustedAt = %d, want 0 (cleared by ResetConnections)", got)
	}
}
