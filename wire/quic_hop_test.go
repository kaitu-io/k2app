package wire

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
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
// immediately returning ErrRelayUDPUnhealthy.
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
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.relayUDPHealthy() {
		t.Fatal("mux should be unhealthy")
	}

	// DialUDP should hop (reset + reconnect) instead of returning error
	pc, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP should succeed after hop, got: %v", err)
	}
	defer pc.Close()

	if client.portHopAttempts.Load() != 1 {
		t.Fatalf("portHopAttempts = %d, want 1", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_HopExhausted verifies that after maxPortHops
// unsuccessful hops, DialUDP returns ErrRelayUDPUnhealthy.
func TestQUICClient_DialUDP_HopExhausted(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Each iteration: connect → poison mux → DialUDP triggers hop
	for i := int32(0); i < maxPortHops; i++ {
		if _, err := client.connect(ctx); err != nil {
			t.Fatalf("connect (hop %d): %v", i, err)
		}
		client.mu.Lock()
		mux := client.udpMux
		client.mu.Unlock()
		for j := int32(0); j < relayTimeoutThreshold; j++ {
			mux.recordTimeout()
		}
	}

	// Now hops are at maxPortHops-1 from previous rounds, but the connect
	// above established a new healthy mux. We need to poison it one more time.
	// Actually, let's re-think: each DialUDP call in the loop above would do
	// the hop. Let me use a simpler approach.

	// Reset and do it properly: pre-set hop attempts to max
	client.portHopAttempts.Store(maxPortHops)

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for j := int32(0); j < relayTimeoutThreshold; j++ {
		mux.recordTimeout()
	}

	// DialUDP should return ErrRelayUDPUnhealthy (hops exhausted)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrRelayUDPUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrRelayUDPUnhealthy", err)
	}

	if client.portHopAttempts.Load() != maxPortHops {
		t.Fatalf("portHopAttempts = %d, want %d", client.portHopAttempts.Load(), maxPortHops)
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

// TestQUICClient_ConcurrentHopRace verifies that when multiple goroutines
// detect relayUDPHealthy()=false simultaneously, only one actually executes
// a hop. Without the CAS guard, N goroutines would each Add(1) + resetConnection(),
// burning N hops in a single unhealthy event.
func TestQUICClient_ConcurrentHopRace(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// 10 goroutines all try to hop simultaneously.
	// Only 1 should win; the rest should get false from tryHop().
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if client.tryHop() {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()

	if winners.Load() != 1 {
		t.Fatalf("hop winners = %d, want exactly 1", winners.Load())
	}
	if client.portHopAttempts.Load() != 1 {
		t.Fatalf("portHopAttempts = %d, want 1", client.portHopAttempts.Load())
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

// TestQUICClient_DialUDP_NoHopWithoutConfig verifies that without hop config,
// unhealthy datagrams immediately return ErrRelayUDPUnhealthy (no hop attempt).
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

	// Should immediately return ErrRelayUDPUnhealthy (no hop)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrRelayUDPUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrRelayUDPUnhealthy", err)
	}

	if client.portHopAttempts.Load() != 0 {
		t.Fatalf("portHopAttempts = %d, want 0 (no hop without config)", client.portHopAttempts.Load())
	}
}

// TestQUICClient_DialUDP_HopRecoveryAfterCooldown verifies that after hop
// exhaustion + cooldown elapsed, the hop counter resets and QUIC gets a fresh
// round of probes (OpenConnect-style periodic recovery).
func TestQUICClient_DialUDP_HopRecoveryAfterCooldown(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Establish connection
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Simulate hop exhaustion that happened 4 minutes ago (past 30s cooldown)
	client.portHopAttempts.Store(maxPortHops)
	client.portHopExhaustedAt.Store(time.Now().Add(-4 * time.Minute).UnixNano())

	// Make datagram path unhealthy
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// DialUDP should trigger recovery: reset hops → hop → reconnect → succeed
	pc, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP should succeed after recovery probe, got: %v", err)
	}
	defer pc.Close()

	// Recovery resets to 0, then hop increments to 1
	if got := client.portHopAttempts.Load(); got != 1 {
		t.Fatalf("portHopAttempts = %d, want 1 (reset + one hop)", got)
	}
	// Exhaustion timestamp cleared by recovery
	if got := client.portHopExhaustedAt.Load(); got != 0 {
		t.Fatalf("portHopExhaustedAt = %d, want 0 (cleared by recovery)", got)
	}
}

// TestQUICClient_DialUDP_HopRecoveryBlockedDuringCooldown verifies that within
// the recovery cooldown period, hops stay exhausted and ErrRelayUDPUnhealthy
// is returned (TCP-WS continues serving).
func TestQUICClient_DialUDP_HopRecoveryBlockedDuringCooldown(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Simulate hop exhaustion that just happened (well within 30s cooldown)
	client.portHopAttempts.Store(maxPortHops)
	client.portHopExhaustedAt.Store(time.Now().UnixNano())

	// Make datagram path unhealthy
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	// DialUDP should return ErrRelayUDPUnhealthy (cooldown not elapsed)
	_, err := client.DialUDP(ctx, "8.8.8.8:53")
	if !errors.Is(err, ErrRelayUDPUnhealthy) {
		t.Fatalf("DialUDP = %v, want ErrRelayUDPUnhealthy (within cooldown)", err)
	}

	// Hop counter should be unchanged
	if got := client.portHopAttempts.Load(); got != maxPortHops {
		t.Fatalf("portHopAttempts = %d, want %d (unchanged during cooldown)", got, maxPortHops)
	}
}

// TestQUICClient_DialUDP_HopRecoveryResetOnNetworkChange verifies that
// ResetConnections() clears the portHopExhaustedAt timestamp along with
// the hop counter, giving QUIC a fully fresh start on the new network.
func TestQUICClient_DialUDP_HopRecoveryResetOnNetworkChange(t *testing.T) {
	client, _, cleanup := setupQUICPairWithHop(t)
	defer cleanup()

	// Simulate a prior exhaustion
	client.portHopAttempts.Store(maxPortHops)
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
