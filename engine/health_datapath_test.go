package engine

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// timeoutDialer is an echoStubDialer whose DialTCP is an unanswered request:
// the stream opened, the header went out, the 5s response read expired. The
// echo still passes — the ticket #3757 shape (handshake + echoes fine, every
// real request dead).
type timeoutDialer struct {
	echoStubDialer
}

func (*timeoutDialer) DialTCP(ctx context.Context, addr string, ed []byte) (net.Conn, error) {
	return nil, &wire.ProxyLayerError{Err: errors.New("wire: read response: deadline exceeded"), ResponseTimeout: true}
}

// The mobile backup-probe gate has a second key besides loss-critical: the
// quic data path in distress (three unanswered requests). Loss can sit at
// 0.000 for the whole episode when the sender is app-limited, so the
// loss-only gate left every such user stuck on a QUIC that answered nothing.
func TestBackupProbe_MobilePowerSavingProbesWhenDataPathDistressed(t *testing.T) {
	primary := &timeoutDialer{}
	primary.setFn(func(ctx context.Context) error { return nil })
	tm := wire.NewTransportManager()
	tm.SetPrimary(primary, "quic")
	ot := &outboundTransport{tm: tm, name: "t-quic-datapath-ps"}
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	tm.SetTCPWS(backup)

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	hm.mobilePowerSaving = true
	st := &backupLoopState{}
	now := time.Now()

	// Healthy, no timeouts yet: gate closed, wake cadence still short.
	if wake := hm.probeBackupOnce(ot, st, now); wake > backupProbeIntervalDegraded {
		t.Fatalf("gate closed: wake cadence %v, want ≤ %v", wake, backupProbeIntervalDegraded)
	}
	if calls := backup.calls.Load(); calls != 0 {
		t.Fatalf("healthy and answering must not probe on mobile, got %d calls", calls)
	}

	// Two unanswered requests: still closed (below the threshold).
	for i := 0; i < 2; i++ {
		if _, err := tm.DialTCP(context.Background(), "api.example.com:443", nil); err == nil {
			t.Fatal("precondition: the dial must fail")
		}
	}
	hm.probeBackupOnce(ot, st, now)
	if calls := backup.calls.Load(); calls != 0 {
		t.Fatalf("two timeouts must not open the gate, got %d calls", calls)
	}

	// Third: the gate opens with health still HEALTHY, at the degraded
	// cadence, and the sample feeds scoreTCPWS.
	if _, err := tm.DialTCP(context.Background(), "api.example.com:443", nil); err == nil {
		t.Fatal("precondition: the dial must fail")
	}
	hm.mu.Lock()
	health := hm.health
	hm.mu.Unlock()
	if health != HealthHealthy {
		t.Fatalf("precondition: health = %s, want healthy (the point is that loss never moved)", health)
	}
	next := hm.probeBackupOnce(ot, st, now)
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("distress must open the mobile gate, got %d calls", calls)
	}
	if next != backupProbeIntervalDegraded || st.nextProbeAt.Sub(now) != backupProbeIntervalDegraded {
		t.Fatalf("cadence: wake %v probe %v, want %v", next, st.nextProbeAt.Sub(now), backupProbeIntervalDegraded)
	}
	if got := tm.ScoreTCPWS(now); got <= 0.5 {
		t.Fatalf("backup sample must have fed scoreTCPWS, got %v", got)
	}

	// The gate closes again once the streak is stale — an idle user does not
	// keep the radio busy on three timeouts from an hour ago.
	later := now.Add(2 * backupProbeIntervalDegraded)
	stale := later.Add(61 * time.Second)
	st2 := &backupLoopState{}
	hm.probeBackupOnce(ot, st2, stale)
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("stale streak must close the gate, got %d calls", calls)
	}
}

// Desktop never gates probing, but its cadence upgrade was score-driven
// only: with loss at 0 the score stays ≥ 0.7 and the backup is probed every
// 60s, so the data-path fast path's two fresh samples took a minute to
// exist. Distress must upgrade the cadence too.
func TestBackupProbe_DesktopCadenceUpgradesOnDataPathDistress(t *testing.T) {
	primary := &timeoutDialer{}
	primary.setFn(func(ctx context.Context) error { return nil })
	tm := wire.NewTransportManager()
	tm.SetPrimary(primary, "quic")
	ot := &outboundTransport{tm: tm, name: "t-quic-datapath-desktop"}
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	tm.SetTCPWS(backup)

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	st := &backupLoopState{}
	now := time.Now()

	// Score healthy (quality primed at 1.0): 60s probe cadence.
	tm.RecordQUICStats(wire.WireStats{PacketLossRate: 0, RTT: 50 * time.Millisecond, MinRTT: 50 * time.Millisecond}, now)
	tm.RecordEchoResult("quic", 50*time.Millisecond, nil, now)
	hm.probeBackupOnce(ot, st, now)
	if st.nextProbeAt.Sub(now) != backupProbeIntervalHealthy {
		t.Fatalf("control: probe cadence %v, want %v", st.nextProbeAt.Sub(now), backupProbeIntervalHealthy)
	}

	for i := 0; i < 3; i++ {
		tm.DialTCP(context.Background(), "api.example.com:443", nil)
	}
	st = &backupLoopState{}
	hm.probeBackupOnce(ot, st, now)
	if st.nextProbeAt.Sub(now) != backupProbeIntervalDegraded {
		t.Fatalf("distressed: probe cadence %v, want %v", st.nextProbeAt.Sub(now), backupProbeIntervalDegraded)
	}
}
