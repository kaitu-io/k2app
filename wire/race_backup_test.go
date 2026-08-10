package wire

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestDrainQUICBackup_CapturesConnectedQUIC verifies that drainQUICBackup
// captures a connected QUIC candidate from the results channel and sets it
// as QUICBackup on the RaceResult.
func TestDrainQUICBackup_CapturesConnectedQUIC(t *testing.T) {
	quicDialer := &mockTransport{available: true}
	res := &RaceResult{
		Dialer:        &mockTransport{available: true}, // tcpws winner
		TransportType: "tcpws",
	}

	results := make(chan raceCandidate, 2)
	// Simulate a connected QUIC-443 arriving shortly after tcpws.
	results <- raceCandidate{dialer: quicDialer, transportType: "quic-443"}

	var mu sync.Mutex
	var backup *connectedQUIC

	res = drainQUICBackup(res, results, 1, &mu, &backup)

	if res.QUICBackup != quicDialer {
		t.Fatal("QUICBackup should be the connected QUIC dialer")
	}
	if res.QUICBackupType != "quic-443" {
		t.Fatalf("QUICBackupType = %q, want quic-443", res.QUICBackupType)
	}
	if backup == nil || backup.dialer != quicDialer {
		t.Fatal("shared backup pointer should reference the QUIC dialer")
	}
}

// TestDrainQUICBackup_Prefers443OverHop verifies that when both quic-443
// and quic-hop arrive, quic-443 is preferred as the backup.
func TestDrainQUICBackup_Prefers443OverHop(t *testing.T) {
	quic443 := &mockTransport{available: true}
	quicHop := &mockTransport{available: true}
	res := &RaceResult{
		Dialer:        &mockTransport{available: true},
		TransportType: "tcpws",
	}

	results := make(chan raceCandidate, 2)
	// quic-hop arrives first, then quic-443.
	results <- raceCandidate{dialer: quicHop, transportType: "quic-hop"}
	results <- raceCandidate{dialer: quic443, transportType: "quic-443"}

	var mu sync.Mutex
	var backup *connectedQUIC

	res = drainQUICBackup(res, results, 2, &mu, &backup)

	if res.QUICBackup != quic443 {
		t.Fatal("QUICBackup should prefer quic-443 over quic-hop")
	}
	if !quicHop.closed {
		t.Fatal("displaced quic-hop should be closed")
	}
}

// TestDrainQUICBackup_SkipsFailedQUIC verifies that failed QUIC candidates
// (err != nil) are not captured as backup.
func TestDrainQUICBackup_SkipsFailedQUIC(t *testing.T) {
	res := &RaceResult{
		Dialer:        &mockTransport{available: true},
		TransportType: "tcpws",
	}

	results := make(chan raceCandidate, 1)
	results <- raceCandidate{transportType: "quic-443", err: ErrNotConnected}

	var mu sync.Mutex
	var backup *connectedQUIC

	res = drainQUICBackup(res, results, 1, &mu, &backup)

	if res.QUICBackup != nil {
		t.Fatal("failed QUIC should not be captured as backup")
	}
}

// TestDrainQUICBackup_TimerExpiry verifies that drain returns promptly
// when remaining candidates don't arrive within the grace period.
func TestDrainQUICBackup_TimerExpiry(t *testing.T) {
	res := &RaceResult{
		Dialer:        &mockTransport{available: true},
		TransportType: "tcpws",
	}

	results := make(chan raceCandidate, 2) // 2 remaining but none sent

	var mu sync.Mutex
	var backup *connectedQUIC

	start := time.Now()
	res = drainQUICBackup(res, results, 2, &mu, &backup)
	elapsed := time.Since(start)

	if res.QUICBackup != nil {
		t.Fatal("no candidates sent, backup should be nil")
	}
	// Should return within ~50ms + some margin, not block for seconds.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("drain took %v, expected ~50ms", elapsed)
	}
}

// TestContract_QUICBackupFlow verifies the full lifecycle when QUIC is
// installed as primary with fallback=true (simulating tcpws winning the
// race while QUIC also connected):
//
//  1. DialTCP routes through tcpws (fallback active)
//  2. QUIC recovery clears fallback
//  3. DialTCP routes through QUIC
//  4. Close() cleans up both without double-close
//
// User scenario: low-latency path where tcpws handshake beats QUIC
// echo-probe. QUIC is preserved and activated by background probe.
func TestContract_QUICBackupFlow(t *testing.T) {
	var quicCalled, tcpwsCalled bool

	quicPrimary := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				quicCalled = true
				c, _ := net.Pipe()
				return c, nil
			},
		},
		transport: "quic",
	}
	tcpwsFallback := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				tcpwsCalled = true
				c, _ := net.Pipe()
				return c, nil
			},
		},
		transport: "tcpws",
	}

	// Simulate engine setup: QUIC as primary, tcpws serving, fallback=true.
	tm := NewTransportManager()
	tm.SetPrimary(quicPrimary, "quic-443")
	tm.SetTCPWS(tcpwsFallback)
	tm.SetFallbackActiveWithReason(true, "quic-race-lost")

	// --- Phase 1: traffic routes through tcpws ---
	if !tm.FallbackActive() {
		t.Fatal("precondition: fallback should be active")
	}

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatalf("Phase 1 DialTCP: %v", err)
	}
	conn.Close()

	if quicCalled {
		t.Fatal("Phase 1: QUIC should be skipped when fallback active")
	}
	if !tcpwsCalled {
		t.Fatal("Phase 1: tcpws should handle traffic")
	}

	// WireStats should report tcpws with fallback flag.
	ws := tm.WireStats()
	if ws.Transport != "tcpws" {
		t.Fatalf("Phase 1: WireStats.Transport = %q, want tcpws", ws.Transport)
	}
	if !ws.WireFallback {
		t.Fatal("Phase 1: WireFallback should be true")
	}

	// --- Phase 2: QUIC probe succeeds, recovery ---
	quicCalled = false
	tcpwsCalled = false

	// A bare handshake success (recordQuicSuccess) must NOT clear fallback
	// on its own (root cause A) — recovery requires echo verification.
	tm.recordQuicSuccess()
	if !tm.FallbackActive() {
		t.Fatal("Phase 2: fallback must stay active after a bare handshake success (echo not verified)")
	}

	// Simulate the background probe's echo-verified recovery (runProbeOnce):
	// 3 passing echo rounds mark quicEchoRecovered, then evaluateSelector —
	// past the flap-damping review window opened by the race-lost install
	// above — applies the switch back to QUIC. Deliberately NO score
	// compensation here: Phase 1 already nudged scoreTCPWS off-neutral with
	// one real success (tcpws is "healthy-ish", not cold), and rollback is
	// echo-gated only (spec §3.2) — a score-ratio gate would be unreachable
	// against a healthy tcpws and strand users on the stateful fallback
	// forever. See TestRollback_FiresWhenTCPWSHealthyAndQUICEchoVerified for
	// the dedicated regression pinning this against a fully healthy tcpws.
	now := time.Now()
	for i := 0; i < 3; i++ {
		tm.RecordEchoResult("quic", 20*time.Millisecond, nil, now)
	}
	tm.quicEchoRecovered.Store(true)

	if got := tm.evaluateSelector(now.Add(flapWindowBase + time.Second)); got != channelSwitchToQUIC {
		t.Fatalf("evaluateSelector = %v, want channelSwitchToQUIC", got)
	}
	if tm.FallbackActive() {
		t.Fatal("Phase 2: fallback should be cleared after echo-verified QUIC recovery")
	}

	// WireStats should now report QUIC.
	ws = tm.WireStats()
	if ws.Transport != "quic" {
		t.Fatalf("Phase 2: WireStats.Transport = %q, want quic", ws.Transport)
	}
	if ws.WireFallback {
		t.Fatal("Phase 2: WireFallback should be false after recovery")
	}

	// --- Phase 3: traffic routes through QUIC ---
	conn, err = tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatalf("Phase 3 DialTCP: %v", err)
	}
	conn.Close()

	if !quicCalled {
		t.Fatal("Phase 3: QUIC should handle traffic after recovery")
	}
	if tcpwsCalled {
		t.Fatal("Phase 3: tcpws should not be called")
	}

	// --- Phase 4: Close() cleans up both ---
	if err := tm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !quicPrimary.closed {
		t.Fatal("Phase 4: QUIC primary should be closed")
	}
	if !tcpwsFallback.closed {
		t.Fatal("Phase 4: tcpws fallback should be closed")
	}
}

// TestContract_SwapTCPWS_ReturnsOld verifies that SwapTCPWS returns the
// old dialer for the caller to close, preventing leaks during re-race.
func TestContract_SwapTCPWS_ReturnsOld(t *testing.T) {
	oldTCPWS := &mockTransport{available: true}
	newTCPWS := &mockTransport{available: true}

	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(oldTCPWS)

	got := tm.SwapTCPWS(newTCPWS)

	if got != oldTCPWS {
		t.Fatal("SwapTCPWS should return the old dialer")
	}

	// Caller is responsible for closing the old one.
	got.Close()
	if !oldTCPWS.closed {
		t.Fatal("old tcpws should be closeable by caller")
	}
	if newTCPWS.closed {
		t.Fatal("new tcpws should not be closed")
	}
}
