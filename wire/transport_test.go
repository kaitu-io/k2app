package wire

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	tls "github.com/metacubex/utls"
)

// mockTransport implements a minimal transport for testing TransportManager.
type mockTransport struct {
	available bool
	dialTCPFn func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error)
	dialUDPFn func(ctx context.Context, addr string) (net.PacketConn, error)
	closed    bool
}

func (m *mockTransport) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	if !m.available {
		return nil, ErrNotConnected
	}
	if m.dialTCPFn != nil {
		return m.dialTCPFn(ctx, addr, earlyData)
	}
	c, _ := net.Pipe()
	return c, nil
}

func (m *mockTransport) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	if !m.available {
		return nil, ErrNotConnected
	}
	if m.dialUDPFn != nil {
		return m.dialUDPFn(ctx, addr)
	}
	return nil, ErrUDPNotSupported
}

func (m *mockTransport) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return m.DialUDP(ctx, addr)
}

func (m *mockTransport) Reset() {}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func TestTransportManager_Create(t *testing.T) {
	tm := NewTransportManager()
	if tm == nil {
		t.Fatal("NewTransportManager returned nil")
	}
}

func TestTransportManager_DialTCP_QUICPreferred(t *testing.T) {
	quicCalled := false
	tcpwsCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !quicCalled {
		t.Error("QUIC should be preferred")
	}
	if tcpwsCalled {
		t.Error("TCP-WS should not be called when QUIC is available")
	}
}

func TestTransportManager_DialTCP_FallbackToTCPWS(t *testing.T) {
	tcpwsCalled := false

	quic := &mockTransport{available: false}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !tcpwsCalled {
		t.Error("TCP-WS should be used as fallback")
	}
}

func TestTransportManager_DialTCP_BothDown(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: false}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: false})

	_, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestTransportManager_DialTCP_NoTransports(t *testing.T) {
	tm := NewTransportManager()

	_, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestTransportManager_DialTCP_QUICFailsFallback(t *testing.T) {
	tcpwsCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			return nil, errors.New("quic stream error")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !tcpwsCalled {
		t.Error("TCP-WS should be used when QUIC fails")
	}
}

func TestTransportManager_Close(t *testing.T) {
	quic := &mockTransport{available: true}
	tcpws := &mockTransport{available: true}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	err := tm.Close()
	if err != nil {
		t.Fatal(err)
	}

	if !quic.closed {
		t.Error("QUIC should be closed")
	}
	if !tcpws.closed {
		t.Error("TCP-WS should be closed")
	}
}

func TestTransportManager_ImplementsDialer(t *testing.T) {
	// TransportManager should implement wire.Dialer at compile time
	var _ Dialer = (*TransportManager)(nil)
}

func TestMultiTransportError_Unwrap(t *testing.T) {
	inner := errors.New("connection refused")
	mte := &MultiTransportError{
		QUICErr:  errors.New("handshake failed"),
		TCPWSErr: inner,
	}
	if !errors.Is(mte, inner) {
		t.Fatal("Unwrap should return TCPWSErr")
	}
}

func TestMultiTransportError_Format(t *testing.T) {
	mte := &MultiTransportError{
		QUICErr:  errors.New("QUIC fail"),
		TCPWSErr: errors.New("TCP fail"),
	}
	got := mte.Error()
	want := "QUIC: QUIC fail; TCP-WS: TCP fail"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestTransportManager_DialUDP_BothDown(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, errors.New("QUIC fail")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, errors.New("TCP fail")
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	_, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	var mte *MultiTransportError
	if !errors.As(err, &mte) {
		t.Fatalf("expected MultiTransportError, got %T", err)
	}
}

// ==========================================================================
// Reset tests
// ==========================================================================

func TestTransportManager_Reset(t *testing.T) {
	tests := []struct {
		name  string
		quic  Dialer
		tcpws Dialer
	}{
		{
			name:  "both transports",
			quic:  NewQUICClient(Config{}, nil),
			tcpws: NewTCPWSClient(Config{}, nil),
		},
		{
			name: "only quic",
			quic: NewQUICClient(Config{}, nil),
		},
		{
			name:  "only tcpws",
			tcpws: NewTCPWSClient(Config{}, nil),
		},
		{
			name: "no transports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := NewTransportManager()
			if tt.quic != nil {
				tm.SetPrimary(tt.quic, "quic-443")
			}
			if tt.tcpws != nil {
				tm.SetTCPWS(tt.tcpws)
			}

			// Should not panic with any combination.
			tm.Reset()

			// Verify fallback state cleared.
			if tm.FallbackActive() {
				t.Error("fallbackActive should be false after reset")
			}
		})
	}
}

// ==========================================================================
// Network change recovery tests
// ==========================================================================

func TestReset_ClearsHandshakeFailStreak(t *testing.T) {
	client := NewQUICClient(Config{Host: "1.2.3.4", Port: 443}, nil)
	client.handshakeFailStreak.Store(quicConnFailThreshold)

	tm := NewTransportManager()
	tm.SetPrimary(client, "quic-443")

	tm.Reset()

	if got := client.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after Reset, want 0", got)
	}
}

func TestTransportManager_SetFallbackActive(t *testing.T) {
	tm := NewTransportManager()

	if tm.FallbackActive() {
		t.Fatal("should not be in fallback initially")
	}

	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active after SetFallbackActive(true)")
	}

	tm.SetFallbackActive(false)
	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after SetFallbackActive(false)")
	}
}

func TestTransportManager_FallbackSkipsQUIC(t *testing.T) {
	var quicCalled bool
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)
	tm.SetFallbackActive(true)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if quicCalled {
		t.Fatal("QUIC should be skipped when fallbackActive=true")
	}
}

func TestTransportManager_QUICFailNoFallback(t *testing.T) {
	// QUIC fails via mockTransport (not QUICClient) → fallbackActive should NOT
	// be set because handshakeFailStreak check only applies to *QUICClient.
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			return nil, errors.New("QUIC broken")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	// Multiple QUIC failures — should NOT auto-activate fallback.
	for range 10 {
		conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}

	if tm.FallbackActive() {
		t.Fatal("fallbackActive should NOT be set by per-request QUIC failures (non-QUICClient)")
	}
}

func TestTransportManager_ConsecConnFailsFallback(t *testing.T) {
	// Use real QUICClient (closed=true to force DialTCP failure) with
	// handshakeFailStreak pre-set to simulate accumulated handshake failures.
	qc := NewQUICClient(Config{}, nil)
	qc.closed = true // force connect() to return ErrNotConnected

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

	// Below threshold: no fallback.
	qc.handshakeFailStreak.Store(quicConnFailThreshold - 1)
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if tm.FallbackActive() {
		t.Fatal("fallbackActive should NOT be set below threshold")
	}

	// At threshold: fallback activated.
	qc.handshakeFailStreak.Store(quicConnFailThreshold)
	conn, err = tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if !tm.FallbackActive() {
		t.Fatal("fallbackActive should be set after handshakeFailStreak >= threshold")
	}

	// Next DialTCP should skip QUIC entirely (fallbackActive=true).
	conn, err = tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestTransportManager_ConsecConnFailsReset(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)

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

	// 2 failures, then recordQuicSuccess resets counter.
	qc.handshakeFailStreak.Store(2)
	tm.recordQuicSuccess()
	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after success, want 0", got)
	}

	// 2 more failures: still below threshold (3), no fallback.
	qc.handshakeFailStreak.Store(2)
	if tm.FallbackActive() {
		t.Fatal("fallbackActive should not be set with only 2 failures")
	}
}

func TestQuicContext_ExtendedBudgetAfterReset(t *testing.T) {
	tm := NewTransportManager()
	tm.Reset() // sets freshAfterReset

	// First call after reset: should get extended budget (~8s).
	ctx := context.Background()
	qCtx, cancel := tm.quicContext(ctx, true)
	defer cancel()
	deadline, ok := qCtx.Deadline()
	if !ok {
		t.Fatal("expected deadline on context")
	}
	budget := time.Until(deadline)
	if budget < 7*time.Second || budget > 9*time.Second {
		t.Fatalf("post-reset budget = %v, want ~8s", budget)
	}

	// Second call: should be back to normal (~5s).
	qCtx2, cancel2 := tm.quicContext(ctx, true)
	defer cancel2()
	deadline2, ok := qCtx2.Deadline()
	if !ok {
		t.Fatal("expected deadline on context")
	}
	budget2 := time.Until(deadline2)
	if budget2 < 4*time.Second || budget2 > 6*time.Second {
		t.Fatalf("normal budget = %v, want ~5s", budget2)
	}
}

// ==========================================================================
// Handshake tests
// ==========================================================================

func TestTransportManager_Handshake_BothFail(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.closed = true // forces connect() to return ErrNotConnected

	tc := NewTCPWSClient(Config{}, nil)
	tc.closed = true // forces connect() to return ErrNotConnected

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(tc)

	err := tm.Handshake(context.Background())
	if err == nil {
		t.Fatal("expected error when both transports fail")
	}
	var mte *MultiTransportError
	if !errors.As(err, &mte) {
		t.Fatalf("expected MultiTransportError, got %T: %v", err, err)
	}
}

func TestTransportManager_Handshake_FallbackSkipsQUIC(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	// Don't set closed — if QUIC were attempted, connect() would try a real
	// handshake. But fallbackActive skips it entirely.

	tc := NewTCPWSClient(Config{}, nil)
	tc.closed = true // TCP-WS will fail, proving it was attempted

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(tc)
	tm.SetFallbackActive(true)

	err := tm.Handshake(context.Background())
	if err == nil {
		t.Fatal("expected error from TCP-WS failure")
	}
	// Should be a plain TCP-WS error (no MultiTransportError, since QUIC was skipped).
	var mte *MultiTransportError
	if errors.As(err, &mte) {
		t.Fatal("should not be MultiTransportError when QUIC is skipped")
	}
	// Verify QUIC handshake streak was NOT incremented (it was skipped).
	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("QUIC ConsecConnFails = %d, want 0 (QUIC should be skipped)", got)
	}
}

func TestTransportManager_Handshake_NoTransports(t *testing.T) {
	tm := NewTransportManager()

	err := tm.Handshake(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestTransportManager_Handshake_ConsecFailsFallback(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.closed = true // forces connect() to fail

	// Pre-set streak to threshold so the Handshake call triggers fallback.
	qc.handshakeFailStreak.Store(quicConnFailThreshold)

	tc := NewTCPWSClient(Config{}, nil)
	tc.closed = true // TCP-WS also fails, but we only care about fallbackActive

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(tc)

	err := tm.Handshake(context.Background())
	if err == nil {
		t.Fatal("expected error when both transports fail")
	}

	// connect() increments the streak further, so it's above threshold.
	// Handshake should have activated fallback.
	if !tm.FallbackActive() {
		t.Fatal("fallbackActive should be set after ConsecConnFails >= threshold")
	}
}

// TestLegacySelector_EnvGate pins legacySelectorEnabled()'s env contract:
// K2_CHANNEL_SELECTOR_LEGACY=1 is the only truthy value.
func TestLegacySelector_EnvGate(t *testing.T) {
	t.Setenv("K2_CHANNEL_SELECTOR_LEGACY", "1")
	if !legacySelectorEnabled() {
		t.Fatal("expected legacy mode when env=1")
	}
	t.Setenv("K2_CHANNEL_SELECTOR_LEGACY", "")
	if legacySelectorEnabled() {
		t.Fatal("expected new mode when env unset")
	}
}

// TestLegacySelector_RollbackOnHandshakeOnly is the mirror image of
// TestProbe_RollbackRequiresEchoNotJustHandshake (transport_recovery_test.go):
// that test pins that the NEW echo-gated probe must NOT roll back on a bare
// handshake success against a half-dead QUIC server (handshake ok, echo
// dead). This test drives the SAME half-dead server through a LEGACY
// TransportManager and asserts the opposite — legacy mode restores the old
// root-cause-A behavior where a bare handshake success alone is enough, so
// the rollback valve genuinely reverts the fix. It also asserts a
// non-legacy TM against the same server stays on tcpws, so the two modes are
// contrasted side by side in one test.
func TestLegacySelector_RollbackOnHandshakeOnly(t *testing.T) {
	addr, cleanup := startHalfDeadQUICServer(t)
	defer cleanup()

	t.Setenv("K2_CHANNEL_SELECTOR_LEGACY", "1")
	qcLegacy := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, NewCookieJar())
	// kuic refuses a Chrome-fingerprint ClientHello whose SNI is empty or an IP
	// literal (RFC 6066 §3), so the dial needs a hostname ServerName to complete
	// the handshake — the very thing this legacy test needs to succeed. The
	// server is InsecureSkipVerify'd, so the name need not match its cert.
	qcLegacy.SetTLSConfig(&tls.Config{ServerName: "localhost", InsecureSkipVerify: true, NextProtos: []string{"h3"}})
	defer qcLegacy.Close()

	tmLegacy := NewTransportManager()
	if !tmLegacy.legacy {
		t.Fatal("precondition: tmLegacy.legacy must be true (env set before construction)")
	}
	tmLegacy.SetPrimary(qcLegacy, "quic-443")
	tmLegacy.SetTCPWS(&mockTransport{available: true})
	tmLegacy.forceActiveForTest("tcpws")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tmLegacy.runProbeOnceForTest(ctx)

	if got := tmLegacy.ActiveChannel(); got != "quic" {
		t.Fatalf("legacy TM ActiveChannel = %q, want quic — bare handshake success must roll back immediately in legacy mode", got)
	}

	// Contrast: a non-legacy TM against the same half-dead server must stay
	// on tcpws (this is TestProbe_RollbackRequiresEchoNotJustHandshake's
	// assertion, repeated here so the two modes are visibly side by side).
	t.Setenv("K2_CHANNEL_SELECTOR_LEGACY", "")
	qcNew := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, NewCookieJar())
	qcNew.SetTLSConfig(&tls.Config{ServerName: "localhost", InsecureSkipVerify: true, NextProtos: []string{"h3"}})
	defer qcNew.Close()

	tmNew := NewTransportManager()
	if tmNew.legacy {
		t.Fatal("precondition: tmNew.legacy must be false (env unset before construction)")
	}
	tmNew.SetPrimary(qcNew, "quic-443")
	tmNew.SetTCPWS(&mockTransport{available: true})
	tmNew.forceActiveForTest("tcpws")
	tmNew.runProbeOnceForTest(ctx)

	if got := tmNew.ActiveChannel(); got != "tcpws" {
		t.Fatalf("non-legacy TM ActiveChannel = %q, want tcpws — half-dead QUIC (handshake ok, echo dead) must NOT roll back", got)
	}
}

// TestLegacySelector_QualityPathDisabled drives the quality-path switch-away
// condition (tcpws sustained-better than quic, past the sustain window, with
// fresh backup samples — exactly what TestSelector_QualityPathNeedsSustainAndFreshBackup
// in transport_selector_test.go proves DOES switch away in non-legacy mode)
// against a legacy TransportManager and asserts it does NOT switch: in
// legacy mode only consecConnFails can move the selector away from quic.
func TestLegacySelector_QualityPathDisabled(t *testing.T) {
	t.Setenv("K2_CHANNEL_SELECTOR_LEGACY", "1")
	tm := NewTransportManager()
	if !tm.legacy {
		t.Fatal("precondition: tm.legacy must be true (env set before construction)")
	}
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	base := time.Now()

	// Drive tcpws to a healthy score with fresh samples (backupFreshSamplesGate+).
	for i := 0; i < 5; i++ {
		tm.RecordEchoResult("tcpws", 15*time.Millisecond, nil, base)
	}
	if got := tm.ScoreTCPWS(base); got < 0.9 {
		t.Fatalf("precondition: scoreTCPWS = %v, want a healthy (~1.0) score", got)
	}

	// Drive quic to a low score so scoreTCPWS > scoreQUIC*switchAwayRatio holds.
	tm.RecordQUICStats(WireStats{PacketLossRate: 0.5, RTT: 500 * time.Millisecond, MinRTT: 20 * time.Millisecond}, base)
	if got := tm.ScoreQUIC(base); got*switchAwayRatio >= tm.ScoreTCPWS(base) {
		t.Fatalf("precondition: scoreQUIC=%v scoreTCPWS=%v does not satisfy the quality-path ratio", got, tm.ScoreTCPWS(base))
	}

	// Advance past the sustain window — in non-legacy mode this alone would
	// be enough to switch away (see TestSelector_QualityPathNeedsSustainAndFreshBackup).
	after := base.Add(qualitySustainWindow + time.Second)
	if d := tm.evaluateSelector(base); d != channelStay {
		t.Fatalf("evaluateSelector(base) = %v, want channelStay", d)
	}
	if d := tm.evaluateSelector(after); d != channelStay {
		t.Fatalf("legacy evaluateSelector must NOT switch away on the quality path alone, got %v", d)
	}
	if got := tm.ActiveChannel(); got != "quic" {
		t.Fatalf("ActiveChannel = %q, want quic — quality path must stay disabled in legacy mode", got)
	}
}
