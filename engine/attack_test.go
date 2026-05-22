package engine

import (
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// TestAttack_RandomConcurrentLifecycleStorm fires a randomized storm of
// lifecycle and signal events at an engine that's been forced into the
// "connected" shape, then concurrently Stop()s it. Catches:
//   - panic from nil dereference during partial teardown
//   - deadlock (5s go-deadlock timeout)
//   - goroutine leak after Stop returns (TestMain goleak)
//   - state ending anywhere other than disconnected
//
// Loop count is small but each iteration spawns ~6 attacker goroutines for
// 100ms. Total cost ~5s under -race.
func TestAttack_RandomConcurrentLifecycleStorm(t *testing.T) {
	const iterations = 20
	const attackers = 6
	const attackDuration = 100 * time.Millisecond

	for it := range iterations {
		e := newAttackEngine(t)
		h := &mockEventHandler{}
		e.SetEventHandler(h)

		var wg sync.WaitGroup
		stop := make(chan struct{})

		for a := range attackers {
			wg.Add(1)
			seed := uint64(it*100 + a)
			go func() {
				defer wg.Done()
				rng := rand.New(rand.NewPCG(seed, seed^0xdeadbeef))
				for {
					select {
					case <-stop:
						return
					default:
					}
					switch rng.IntN(7) {
					case 0:
						e.OnNetworkChanged()
					case 1:
						e.OnNetEvent(&NetEvent{Signal: SignalAvailable, Source: "attack"})
					case 2:
						e.OnNetEvent(&NetEvent{Signal: SignalUnavailable, Source: "attack"})
					case 3:
						e.OnNetEvent(&NetEvent{Signal: SignalChanged, Source: "attack"})
					case 4:
						e.Pause()
					case 5:
						e.Wake()
					case 6:
						e.onHealthCritical()
					}
				}
			}()
		}

		// Race Stop() against the storm. Random small jitter so different
		// iterations hit different points in the event mix.
		jitter := time.Duration(rand.IntN(int(attackDuration/2))) * time.Nanosecond
		time.Sleep(attackDuration/2 + jitter)

		stopDone := make(chan error, 1)
		go func() {
			stopDone <- e.Stop()
		}()
		select {
		case err := <-stopDone:
			if err != nil {
				t.Fatalf("iter %d: Stop returned error: %v", it, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iter %d: Stop did not complete in 5s — deadlock", it)
		}

		close(stop)
		wg.Wait()

		// Final invariants.
		if got := e.Status(); got != StateDisconnected {
			t.Fatalf("iter %d: post-Stop status = %q, want disconnected", it, got)
		}
		e.mu.Lock()
		hasResidual := e.cancel != nil || e.health != nil || e.netCoord != nil ||
			e.prov != nil || e.tunnel != nil || e.outboundTMs != nil
		e.mu.Unlock()
		if hasResidual {
			t.Fatalf("iter %d: residual engine fields after Stop", it)
		}
	}
}

// TestAttack_NetCoordEventStormAcrossStop fires a high-rate event stream at
// netCoord while Stop() runs. After Stop returns, no further callback should
// ever fire — even a leading/trailing debounce timer in flight must be
// neutralized.
func TestAttack_NetCoordEventStormAcrossStop(t *testing.T) {
	const totalEvents = 2000

	e := newAttackEngine(t)
	var reconnects atomic.Int64
	// Re-wire netCoord callbacks to count fires instead of touching engine.
	e.netCoord = newNetCoordinator(func() { reconnects.Add(1) }, func() {})

	stop := make(chan struct{})

	// Storm: 4 producers alternating signals.
	var wg sync.WaitGroup
	for p := range 4 {
		wg.Add(1)
		seed := uint64(p)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed, ^seed))
			for range totalEvents / 4 {
				select {
				case <-stop:
					return
				default:
				}
				var sig string
				switch rng.IntN(3) {
				case 0:
					sig = SignalAvailable
				case 1:
					sig = SignalUnavailable
				default:
					sig = SignalChanged
				}
				e.OnNetEvent(&NetEvent{Signal: sig, Source: "storm"})
			}
		}()
	}

	// Let storm warm up so trailing timers are armed.
	time.Sleep(50 * time.Millisecond)

	// Concurrent Stop().
	stopDone := make(chan struct{})
	go func() {
		_ = e.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not complete in 5s — deadlock")
	}

	close(stop)
	wg.Wait()

	// Snapshot count at end of Stop.
	postStopCount := reconnects.Load()

	// Wait one full debounce window for any in-flight trailing timer.
	time.Sleep(700 * time.Millisecond)

	finalCount := reconnects.Load()
	if finalCount != postStopCount {
		t.Fatalf("netCoord fired %d callbacks AFTER Stop returned (pre=%d, final=%d) — late-fire bug",
			finalCount-postStopCount, postStopCount, finalCount)
	}

	// Sanity: with 2000 events compressed by 500ms debounce, total fires
	// should be heavily bounded (≪ events).
	if finalCount > 100 {
		t.Fatalf("netCoord fired %d times for %d events — debounce broken", finalCount, totalEvents)
	}
}

// TestAttack_HealthMonitorStartStopChurn rapidly starts and stops the health
// monitor with concurrent resetCounters() calls. Catches:
//   - goroutine leak from sample/echo loops not exiting
//   - panic from accessing fields cleared by stop
//   - go-deadlock 5s timeout from lock ordering bugs
func TestAttack_HealthMonitorStartStopChurn(t *testing.T) {
	const cycles = 30

	for c := range cycles {
		tm := mockTransportManager()
		tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

		ot := &outboundTransport{tm: tm, name: "attack"}
		var reconnectCount atomic.Int64
		var hm *healthMonitor
		hm = newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
			reconnectCount.Add(1)
			// Simulate the engine reconnect path that touches hm.mu.
			hm.resetCounters()
		}, time.Now())

		// Force critical so first sample fires onReconnect (the exact path
		// that originally self-deadlocked).
		hm.mu.Lock()
		hm.health = HealthCritical
		hm.smoothWireLoss = 0.5
		hm.ewmaInitialized = true
		hm.mu.Unlock()

		hm.start()

		// Concurrent resetCounters spam.
		var wg sync.WaitGroup
		stop := make(chan struct{})
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					hm.resetCounters()
				}
			}()
		}

		// Short churn — keep total test time bounded.
		time.Sleep(50 * time.Millisecond)

		stopDone := make(chan struct{})
		go func() {
			hm.stop()
			close(stopDone)
		}()
		select {
		case <-stopDone:
		case <-time.After(5 * time.Second):
			t.Fatalf("cycle %d: hm.stop did not complete in 5s — deadlock", c)
		}

		close(stop)
		wg.Wait()
	}
}

// TestAttack_PauseWakeAlternation hammers Pause/Wake/Pause/... while a
// background goroutine spams OnNetworkChanged and onHealthCritical. After
// the storm, engine state must be stable (connected or disconnected — never
// some half-state).
func TestAttack_PauseWakeAlternation(t *testing.T) {
	e := newAttackEngine(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Hammer Pause/Wake.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			e.Pause()
			e.Wake()
		}
	}()

	// Hammer reconnect triggers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			e.OnNetworkChanged()
			e.onHealthCritical()
		}
	}()

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	// After the storm, paused must be either true or false (not garbage),
	// state must be StateConnected (we never called Stop), no panic happened.
	e.mu.Lock()
	state := e.state
	paused := e.paused
	e.mu.Unlock()
	if state != StateConnected {
		t.Fatalf("state = %q, want connected", state)
	}
	_ = paused // either is valid — whichever Pause/Wake ran last

	// Clean Stop.
	if err := e.Stop(); err != nil {
		t.Fatalf("clean Stop after storm: %v", err)
	}

	// Goroutine sanity (sleep gives any orphans a chance to settle).
	time.Sleep(50 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > 50 {
		// 50 is a generous bound — the engine itself should leave <10
		// after Stop. Anything higher signals a real leak.
		t.Logf("runtime.NumGoroutine = %d after Stop (warning, not failure)", n)
	}
}

// newAttackEngine constructs an engine forced into the "connected" shape:
// state=StateConnected, primaryTM/outboundTMs set, slowProvider as prov,
// tunnel allocated, netCoord wired with no-op callbacks, healthMonitor
// running with no-op reconnect. Production Start() does much more, but for
// concurrency attacks this exercise the same code paths.
func newAttackEngine(t *testing.T) *Engine {
	t.Helper()
	e := New()
	tm := mockTransportManager()
	ot := &outboundTransport{tm: tm, name: "attack"}
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.prov = &slowProvider{closeDelay: 5 * time.Millisecond}
	e.tunnel = tunnel
	e.primaryTM = ot
	e.outboundTMs = []*outboundTransport{ot}
	// reRaceChan + outboundReplaceLoop are NOT started here — those need
	// a real reRaceCtx which complicates Stop pathing for attack tests.
	// onHealthCritical/Reset paths don't touch reRaceChan so this is fine.
	e.mu.Unlock()

	// healthMonitor with no-op reconnect (so storm calls to onHealthCritical
	// don't trigger reentrancy into engine.reconnect during teardown).
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		// No-op: avoids cascading reconnect storms during Stop.
		_ = errors.New("noop")
	}, time.Now())
	hm.start()
	e.mu.Lock()
	e.health = hm
	e.netCoord = newNetCoordinator(func() {}, func() {})
	e.mu.Unlock()

	t.Cleanup(func() {
		// Defensive Stop in case test failed mid-flight.
		_ = e.Stop()
	})
	return e
}

var _ = wire.NewTransportManager // keep import even if unused above
