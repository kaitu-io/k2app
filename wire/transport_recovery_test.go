package wire

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
)

// mockStatsTransport implements Dialer + StatsProvider for testing WireStats fallback.
type mockStatsTransport struct {
	mockTransport
	transport string
}

func (m *mockStatsTransport) WireStats() WireStats {
	return WireStats{Transport: m.transport}
}

var _ StatsProvider = (*mockStatsTransport)(nil)

// TestTransportManager_FallbackTracking verifies that when SetFallbackActive(true)
// is called, WireStats reflects TCP-WS with Fallback flag, and DialTCP skips QUIC.
func TestTransportManager_FallbackTracking(t *testing.T) {
	var quicCalled bool
	quic := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				quicCalled = true
				return nil, errors.New("QUIC broken")
			},
		},
		transport: "quic",
	}
	tcpws := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				c, _ := net.Pipe()
				return c, nil
			},
		},
		transport: "tcpws",
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	// Before fallback: QUIC stats
	if tm.FallbackActive() {
		t.Fatal("should not be in fallback initially")
	}
	ws := tm.WireStats()
	if ws.Transport != "quic" {
		t.Fatalf("WireStats.Transport = %q, want quic", ws.Transport)
	}

	// Activate fallback via SetFallbackActive
	tm.SetFallbackActive(true)

	ws = tm.WireStats()
	if ws.Transport != "tcpws" {
		t.Fatalf("WireStats.Transport = %q, want tcpws in fallback", ws.Transport)
	}
	if !ws.WireFallback {
		t.Fatal("WireStats.WireFallback should be true")
	}

	// DialTCP should skip QUIC
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if quicCalled {
		t.Fatal("QUIC should be skipped when fallback is active")
	}
}

// TestTransportManager_QuicRecovery verifies that a bare QUIC handshake
// success (recordQuicSuccess) does NOT clear fallback on its own — recovery
// requires echo verification via evaluateSelector (root cause A fix; see
// TestProbe_RollbackRequiresEchoNotJustHandshake for the full mutation
// test). recordQuicSuccess only resets the handshake failure streak.
func TestTransportManager_QuicRecovery(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.handshakeFailStreak.Store(2)

	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(tcpws)

	// Set fallback, then a bare handshake success must NOT clear it.
	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	tm.recordQuicSuccess()
	if !tm.FallbackActive() {
		t.Fatal("fallback must stay active after a bare handshake success (echo not verified)")
	}
	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after recordQuicSuccess, want 0", got)
	}

	// Explicit clear (compat shim, used by engine's race-lost install path
	// via SetFallbackActiveWithReason) still works.
	tm.SetFallbackActive(false)
	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after SetFallbackActive(false)")
	}
}

// startHalfDeadQUICServer boots a real QUIC listener that completes the k2
// metadata handshake (so QUICClient.connect() succeeds exactly like a real
// server) but never responds to any stream opened afterward. An EchoProbe
// against it blocks until its own read deadline — simulating a black-holed
// data path where the handshake succeeds but Short Header packets are
// silently dropped (root cause A's failure mode).
func startHalfDeadQUICServer(t *testing.T) (*net.UDPAddr, func()) {
	t.Helper()
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go serveHalfDeadConn(conn)
		}
	}()

	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	cleanup := func() {
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
	return addr, cleanup
}

// serveHalfDeadConn answers exactly one stream — the metadata handshake —
// then goes silent. A further stream (e.g. an echo probe) is accepted by
// quic-go locally on the client side regardless of server action, but the
// server here never reads or writes it, so the client blocks on its own
// read deadline.
func serveHalfDeadConn(conn *quic.Conn) {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		return
	}
	br := bufio.NewReader(stream)
	frameType, _, err := ReadVarint(br)
	if err != nil || frameType != H3FrameTypeK2Proxy {
		return
	}
	streamType, _, err := ReadStreamHeader(br)
	if err != nil || streamType != StreamTypeMeta {
		return
	}
	if _, err := ReadMetadataFrame(br); err != nil {
		return
	}
	_ = WriteStreamResponse(stream, nil)
	_ = WriteMetadataResponse(stream, "")
	stream.Close()
	// Deliberately do not accept any further streams — echo probes black-hole.
}

// TestProbe_RollbackRequiresEchoNotJustHandshake pins root cause A: a probe
// that only completes the QUIC handshake must NOT clear fallback / switch
// back to QUIC. It drives a REAL *QUICClient (the same concrete type
// production uses, so the mutation genuinely exercises quicProbeLoop's
// production code path — not a parallel mock implementation) against a
// server whose handshake succeeds but whose data path is black-holed.
//
// This test FAILS against the pre-Task-3 quicProbeLoop (which called
// recordQuicSuccess()/cleared fallback right after connect() succeeded,
// with no data-path check) and PASSES after the echo-verified rewrite
// (runProbeOnce, which requires 3 passing EchoProbe rounds before it will
// ever set quicEchoRecovered).
func TestProbe_RollbackRequiresEchoNotJustHandshake(t *testing.T) {
	addr, cleanup := startHalfDeadQUICServer(t)
	defer cleanup()

	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, NewCookieJar())
	// kuic drops a Chrome-fingerprint ClientHello with an empty/IP-literal SNI
	// (RFC 6066 §3), which would fail the handshake outright and let this test
	// pass for the wrong reason (no rollback because no handshake, rather than
	// no rollback because echo is unverified). A hostname ServerName makes the
	// handshake genuinely succeed so the echo-gate is what's under test.
	qc.SetTLSConfig(&tls.Config{ServerName: "localhost", InsecureSkipVerify: true, NextProtos: []string{"h3"}})
	defer qc.Close()

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.forceActiveForTest("tcpws")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tm.runProbeOnceForTest(ctx)

	if tm.ActiveChannel() == "quic" {
		t.Fatal("half-dead QUIC (handshake ok, echo dead) must NOT trigger rollback to QUIC")
	}
	if tm.quicEchoRecovered.Load() {
		t.Fatal("quicEchoRecovered must stay false when echo probes fail")
	}
}

// TestRollback_FiresWhenTCPWSHealthyAndQUICEchoVerified pins the fix-round-1
// defect: rollback (tcpws→QUIC) must fire on echo verification ALONE, even
// when tcpws is fully healthy. This is the mirror case of
// TestProbe_RollbackRequiresEchoNotJustHandshake — that one pins "handshake
// alone must not roll back", this one pins "a healthy backup must not
// permanently block rolling back".
//
// The bug this guards against: while tcpws serves traffic, QUIC carries no
// real traffic, so scoreQUIC.quality can never be measured and stays pinned
// at neutral (0.5) — scoreQUIC ≤ 0.5 structurally. A rollback gate of the
// shape `scoreQUIC > scoreTCPWS × k` can mathematically never fire once
// tcpws is healthy (scoreTCPWS ≈ 1.0), permanently stranding users on the
// stateful tcpws/smux path — the opposite of the design's goal (QUIC is the
// strategic, stateless home). See spec §3.2 "切回为什么不做分数比较".
//
// Mutation verification: this test FAILS if selectorStep's rollback branch
// is reverted to the old `in.scoreQUIC > in.scoreTCPWS*switchBackRatio`
// gate (verified by hand: temporarily restoring that gate makes this test
// fail with "evaluateSelector = channelStay, want channelSwitchToQUIC",
// then removing it again restores PASS — the same red/green protocol used
// for TestProbe_RollbackRequiresEchoNotJustHandshake).
func TestRollback_FiresWhenTCPWSHealthyAndQUICEchoVerified(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	now := time.Now()

	// Drive tcpws to a healthy score: several real successes + a good echo
	// RTT baseline (QualityFromEchoRTT is self-relative — a stable RTT
	// converges to ~1.0 quality).
	for i := 0; i < 5; i++ {
		tm.RecordEchoResult("tcpws", 15*time.Millisecond, nil, now)
	}
	if got := tm.ScoreTCPWS(now); got < 0.9 {
		t.Fatalf("precondition: scoreTCPWS = %v, want a healthy (~1.0) score", got)
	}

	// QUIC echo-verified (fresh handshake + 3 passing echo rounds, exactly
	// what runProbeOnce sets after a successful probe cycle) — its own score
	// is necessarily low/neutral since it carries no traffic while tcpws is
	// active; that must not matter.
	tm.quicEchoRecovered.Store(true)
	if got := tm.ScoreQUIC(now); got > 0.5 {
		t.Fatalf("precondition: scoreQUIC = %v, want it pinned at/below neutral (untraded channel)", got)
	}

	// forceActiveForTest installs a zero-value nextReviewAt, so the
	// flap-damping review window is trivially open.
	tm.forceActiveForTest("tcpws")

	if got := tm.evaluateSelector(now); got != channelSwitchToQUIC {
		t.Fatalf("evaluateSelector = %v, want channelSwitchToQUIC — echo verification alone must roll back even against a healthy tcpws", got)
	}
	if tm.ActiveChannel() != "quic" {
		t.Fatalf("ActiveChannel = %q, want quic", tm.ActiveChannel())
	}
	// The verification is one-shot: a completed rollback consumes it, so the
	// NEXT fallback episode has to earn a fresh handshake + 3 echo rounds all
	// over again (final-review I3 — see
	// TestProbe_StaleEchoResultDiscardedAfterRollback for the in-flight-probe
	// half of that invariant).
	if tm.quicEchoRecovered.Load() {
		t.Fatal("quicEchoRecovered must be consumed (false) once the rollback actually lands")
	}
}

// TestProbe_StaleEchoResultDiscardedAfterRollback pins final-review I3: a
// recovery probe already in flight when the rollback lands must NOT re-arm
// quicEchoRecovered afterwards.
//
// runProbeOnce spends up to ~20s in unlocked I/O (handshake + 3 echo rounds
// at 5s each). evaluateSelector can complete the rollback — and consume the
// flag with Store(false) — at any point inside that window. An unconditional
// Store at the end then writes the FINISHED episode's evidence back onto a
// selector that is already home on QUIC, so the next time QUIC falls back it
// finds a pre-armed flag and rolls straight back with no fresh verification
// at all: precisely root cause A, re-entering through the back door.
//
// The rollback is simulated deterministically from inside the last echo round
// (which is exactly when the real race lands) rather than from a competing
// goroutine, so there is nothing timing-dependent here.
func TestProbe_StaleEchoResultDiscardedAfterRollback(t *testing.T) {
	tm := NewTransportManager()
	probe := &echoingMockTransport{mockTransport: mockTransport{available: true}}
	var round int
	probe.echoFn = func(context.Context) error {
		round++
		if round == 3 {
			// evaluateSelector rolls back mid-probe and consumes the flag.
			tm.forceActiveForTest("quic")
			tm.quicEchoRecovered.Store(false)
		}
		return nil
	}
	tm.SetPrimary(probe, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.forceActiveForTest("tcpws")

	tm.runProbeOnceForTest(context.Background())

	if round != 3 {
		t.Fatalf("precondition: probe ran %d echo rounds, want 3 — the rollback never landed mid-probe", round)
	}
	if tm.quicEchoRecovered.Load() {
		t.Fatal("stale echo verification re-armed quicEchoRecovered after the rollback had already consumed it — the next fallback would roll back without fresh proof")
	}
}

// TestTransportManager_ResetClearsFallback verifies that Reset()
// clears fallback state.
func TestTransportManager_ResetClearsFallback(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	tm.Reset()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after reset")
	}
}

// TestReset_ClearsChannelScoresAndMinEchoRTT pins final-review I2 (spec §6,
// "网络切换 Reset ... 清 flapCount/窗口/分数 EWMA"): Reset must clear both
// ChannelScores AND the tcpws min-echo-RTT ratchet, not just the selector
// state.
//
// The EWMAs alone would eventually decay to neutral through ChannelScore's
// staleness gate (~3×probeInterval, ~48s), which is survivable. The min-RTT
// ratchet is not: it is monotonic and never decays, so a 5ms LAN baseline
// carried across a move to an 80ms mobile path pins QualityFromEchoRTT at
// ~0.06 and silently disables the selector's quality path for the rest of the
// process lifetime.
func TestReset_ClearsChannelScoresAndMinEchoRTT(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	now := time.Now()
	// Old network: both channels measured healthy, and the tcpws ratchet
	// dragged down to a 5ms LAN minimum.
	for i := 0; i < 6; i++ {
		tm.RecordEchoResult("tcpws", 5*time.Millisecond, nil, now)
		tm.RecordEchoResult("quic", 0, nil, now)
	}
	tm.RecordQUICStats(WireStats{RTT: 20 * time.Millisecond, MinRTT: 20 * time.Millisecond}, now)
	if got := tm.ScoreTCPWS(now); got < 0.9 {
		t.Fatalf("precondition: scoreTCPWS = %v, want a healthy (~1.0) score", got)
	}
	if got := tm.ScoreQUIC(now); got < 0.9 {
		t.Fatalf("precondition: scoreQUIC = %v, want a healthy (~1.0) score", got)
	}
	if got := tm.tcpwsMinEchoRTT.Load(); got != int64(5*time.Millisecond) {
		t.Fatalf("precondition: tcpwsMinEchoRTT = %v, want 5ms", time.Duration(got))
	}

	tm.Reset()

	// Both scores are back to "no data yet" → neutral × neutral.
	const wantNeutral = scoreNeutral * scoreNeutral
	if got := tm.ScoreQUIC(now); got != wantNeutral {
		t.Fatalf("scoreQUIC = %v after Reset, want %v (old-network EWMA survived)", got, wantNeutral)
	}
	if got := tm.ScoreTCPWS(now); got != wantNeutral {
		t.Fatalf("scoreTCPWS = %v after Reset, want %v (old-network EWMA survived)", got, wantNeutral)
	}

	// New network: an 80ms echo is now the baseline, so its quality is 1.0 and
	// the first post-Reset sample lands at a clean 1.0. With the 5ms ratchet
	// still in place it would be QualityFromEchoRTT(80ms, 5ms) = 0.0625,
	// dragging the score down instead — on a link that is simply farther away,
	// not worse.
	tm.RecordEchoResult("tcpws", 80*time.Millisecond, nil, now)
	if got := tm.ScoreTCPWS(now); got < 0.99 {
		t.Fatalf("scoreTCPWS = %v after a first 80ms echo on the new path, want ~1.0 — the stale min-RTT ratchet is still crushing it", got)
	}
}

// resettableMock records whether Reset() reached the dialer.
type resettableMock struct {
	mockTransport
	resets int
}

func (m *resettableMock) Reset() { m.resets++ }

// TestResetConnections_PreservesScoreboard pins the health-reconnect variant:
// a wire-quality reconnect (engine's onHealthCritical) redials the SAME path,
// so the measurements the selector has gathered on it are still valid and
// must survive — scores, the selector state, flap history, the tcpws min-RTT
// ratchet and the fresh-sample window. Only the cached connections are
// dropped (both dialers' Reset()), plus the post-reset dial budget.
//
// Without this the health reconnect (10s cooldown) kept wiping the quality
// path's evidence faster than the backup probe could rebuild it, so a lossy
// but reachable QUIC could never be escaped (ticket #3747), and a rollback
// followed by a fresh critical would zero flapCount and oscillate at the
// base 30s window forever.
func TestResetConnections_PreservesScoreboard(t *testing.T) {
	p := &resettableMock{mockTransport: mockTransport{available: true}}
	b := &resettableMock{mockTransport: mockTransport{available: true}}
	tm := NewTransportManager()
	tm.SetPrimary(p, "quic-443")
	tm.SetTCPWS(b)

	now := time.Now()
	for i := 0; i < 4; i++ {
		tm.RecordEchoResult("tcpws", 40*time.Millisecond, nil, now)
	}
	tm.RecordQUICStats(WireStats{PacketLossRate: 0.5, RTT: 100 * time.Millisecond, MinRTT: 20 * time.Millisecond}, now)
	tm.mu.Lock()
	tm.selState.flapCount = 3
	tm.selState.aboveSince = now.Add(-5 * time.Second)
	tm.mu.Unlock()

	wantQUIC, wantTCPWS := tm.ScoreQUIC(now), tm.ScoreTCPWS(now)
	wantFresh := tm.scoreTCPWS.FreshSamples(now)
	if wantFresh < backupFreshSamplesGate {
		t.Fatalf("precondition: fresh samples = %d", wantFresh)
	}

	tm.ResetConnections()

	if p.resets != 1 || b.resets != 1 {
		t.Fatalf("both dialers must have their cached connections reset: primary=%d tcpws=%d", p.resets, b.resets)
	}
	if !tm.freshAfterReset.Load() {
		t.Fatal("first dial after a reconnect must get the extended budget")
	}
	if got := tm.ScoreQUIC(now); got != wantQUIC {
		t.Fatalf("scoreQUIC %v → %v: evidence wiped", wantQUIC, got)
	}
	if got := tm.ScoreTCPWS(now); got != wantTCPWS {
		t.Fatalf("scoreTCPWS %v → %v: evidence wiped", wantTCPWS, got)
	}
	if got := tm.scoreTCPWS.FreshSamples(now); got != wantFresh {
		t.Fatalf("fresh samples %d → %d", wantFresh, got)
	}
	if got := tm.tcpwsMinEchoRTT.Load(); got != int64(40*time.Millisecond) {
		t.Fatalf("tcpws min-RTT ratchet cleared: %v", time.Duration(got))
	}
	tm.mu.Lock()
	flap, above := tm.selState.flapCount, tm.selState.aboveSince
	tm.mu.Unlock()
	if flap != 3 {
		t.Fatalf("flapCount %d, want 3 (anti-flap history erased)", flap)
	}
	if above.IsZero() {
		t.Fatal("aboveSince cleared: the sustain clock restarted")
	}

	// Contrast: the network-change Reset still clears everything.
	tm.Reset()
	if got := tm.ScoreQUIC(now); got != scoreNeutral*scoreNeutral {
		t.Fatalf("Reset must still clear scores, got %v", got)
	}
	tm.mu.Lock()
	flap = tm.selState.flapCount
	tm.mu.Unlock()
	if flap != 0 {
		t.Fatalf("Reset must still clear flapCount, got %d", flap)
	}
}

// TestBackupFresh_AgesOut pins the windowed gate end-to-end through the real
// TransportManager: tcpws samples recorded long ago must not satisfy the
// quality path's backupFreshSamplesGate today.
func TestBackupFresh_AgesOut(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.SetChannelProbeInterval("tcpws", 5*time.Second) // window 15s

	base := time.Unix(1000, 0)
	tm.RecordEchoResult("tcpws", 10*time.Millisecond, nil, base)
	tm.RecordEchoResult("tcpws", 10*time.Millisecond, nil, base.Add(time.Second))
	// QUIC collapses: ratio holds from here on.
	tm.RecordQUICStats(WireStats{PacketLossRate: 0.6, RTT: 100 * time.Millisecond, MinRTT: 20 * time.Millisecond}, base)

	// Within the window and past the sustain: switches.
	tm.evaluateSelector(base.Add(2 * time.Second))
	if d := tm.evaluateSelector(base.Add(13 * time.Second)); d != channelSwitchToTCPWS {
		t.Fatalf("fresh samples + sustain → want switch, got %v", d)
	}

	// Same history, but evaluated only after the samples aged out (>15s):
	// the ratio still "holds" on the stale (neutral) tcpws score, yet with no
	// fresh sample the gate must refuse.
	tm2 := NewTransportManager()
	tm2.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm2.SetTCPWS(&mockTransport{available: true})
	tm2.SetChannelProbeInterval("tcpws", 5*time.Second)
	tm2.RecordEchoResult("tcpws", 10*time.Millisecond, nil, base)
	tm2.RecordEchoResult("tcpws", 10*time.Millisecond, nil, base.Add(time.Second))
	tm2.RecordQUICStats(WireStats{PacketLossRate: 0.6, RTT: 100 * time.Millisecond, MinRTT: 20 * time.Millisecond}, base.Add(30*time.Second))
	tm2.evaluateSelector(base.Add(30 * time.Second))
	if d := tm2.evaluateSelector(base.Add(41 * time.Second)); d != channelStay {
		t.Fatalf("stale samples → want stay, got %v (monotonic counter behavior)", d)
	}
}

// TestReset_ClearsConsecConnFails verifies that Reset()
// clears the consecutive connection failure counter.
func TestReset_ClearsConsecConnFails(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.handshakeFailStreak.Store(2)

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")

	tm.Reset()

	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after Reset, want 0", got)
	}
}

// TestTransportManager_RelayUDPUnhealthyNotFallback verifies that
// ErrDatagramRelayUnhealthy does not trigger transport-level fallback.
func TestTransportManager_RelayUDPUnhealthyNotFallback(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, ErrDatagramRelayUnhealthy
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	conn.Close()

	if tm.FallbackActive() {
		t.Fatal("ErrDatagramRelayUnhealthy should not set fallbackActive")
	}
}
