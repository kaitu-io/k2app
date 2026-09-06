package k2cc

import (
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic/congestion"
)

// Snapshot() is called at 1 Hz by the health sampler. Before the fix,
// mi.Throughput() inside it set IsAppLimited=true on the LIVE MI whenever the
// in-progress MI had sent less than 50% of target so far — i.e. always, early
// in an MI — and the flag is sticky, so the observer flipped the controller
// into the app-limited anchor path.
func TestSnapshot_DoesNotMarkLiveMIAppLimited(t *testing.T) {
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	mi := NewMonitorInterval(7, 150_000_000)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.BytesSent = 150_000 // >= minSnapshotBytes, far below 50% of 150M×1s/8
	mi.BytesAcked = 150_000
	sender.state.CurrentMI = mi

	for range 3 {
		_ = sender.Snapshot()
	}
	if mi.IsAppLimited {
		t.Fatal("Snapshot() mutated the live MI's IsAppLimited — the observer must be pure")
	}
}

func TestMonitorInterval_AppLimitedIsExplicit(t *testing.T) {
	mi := NewMonitorInterval(1, 100_000_000)
	mi.StartTime = time.Now().Add(-100 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 100_000 // expected 1.25 MB → 8% → app-limited
	if !mi.appLimited() {
		t.Fatal("8% of expected bytes must be app-limited")
	}
	mi.BytesSent = 1_000_000 // 80%
	if mi.appLimited() {
		t.Fatal("80% of expected bytes must not be app-limited")
	}
	_ = mi.Throughput()
	if mi.IsAppLimited {
		t.Fatal("Throughput() must not write IsAppLimited")
	}
}

// Run with -race. Before the fix OnCongestionEventEx wrote the ackRate tracker
// outside p.mu while Snapshot()/Summary() read it under RLock.
func TestObservers_RaceFreeAgainstControlPath(t *testing.T) {
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 20 * time.Millisecond})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = sender.Snapshot()
			// _ = sender.Summary() // enabled once Summary() lands (next commit)
		}
	}()

	start := time.Now()
	var pn congestion.PacketNumber
	for time.Since(start) < 50*time.Millisecond {
		now := congestion.Time(time.Since(start))
		acked := make([]congestion.AckedPacketInfo, 0, 20)
		for range 20 {
			sender.OnPacketSent(now, 0, pn, 1400, true)
			acked = append(acked, congestion.AckedPacketInfo{PacketNumber: pn, BytesAcked: 1400})
			pn++
		}
		for _, a := range acked {
			sender.OnPacketAcked(a.PacketNumber, a.BytesAcked, 0, now)
		}
		sender.OnCongestionEventEx(0, now, acked, nil)
	}
	close(stop)
	wg.Wait()
}
