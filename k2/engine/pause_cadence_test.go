package engine

// Final-review-2 fix wave: the retry schedule and network holds respect
// Pause (I1), hold ticks judge the network active-only (M3), a long-pending
// target slows to longPendingInterval (M4) and wakes at once when its node is
// seen answering (M8), and an iface-down tick skips Phase A (M9). All on the
// harness's fake clock.

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/kaitu-io/k2/config"
)

// racesTotal counts Phase-A races on every host.
func (hs *foHarness) racesTotal() int {
	hs.servers.mu.Lock()
	defer hs.servers.mu.Unlock()
	n := 0
	for _, c := range hs.servers.races {
		n += c
	}
	return n
}

func (hs *foHarness) setupsTotal() int {
	hs.servers.mu.Lock()
	defer hs.servers.mu.Unlock()
	n := 0
	for _, c := range hs.servers.setups {
		n += c
	}
	return n
}

// I1: a dead fleet paused for 20 min: no netcheck, no race, no cap fire;
// Wake ticks within 1s; the paused stretch is not counted toward the cap.
func TestRetrySchedule_PauseStopsTicksAndCapClock(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.peek().at.Sub(r.start) < 2*time.Minute {
		if _, exited := r.tick(); exited {
			t.Fatal("failed before the pause")
		}
	}
	hs.e.Pause()
	pausedAt := hs.clock.Now().Sub(r.start)
	races, checks, setups := hs.racesTotal(), hs.net.callCount(), hs.setupsTotal()
	for r.peek().at.Sub(r.start) < pausedAt+20*time.Minute {
		if _, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v while paused", r.elapsed())
		}
	}
	if n := hs.racesTotal(); n != races {
		t.Fatalf("races while paused = %d, want 0", n-races)
	}
	if n := hs.net.callCount(); n != checks {
		t.Fatalf("netchecks while paused = %d, want 0", n-checks)
	}
	if n := hs.setupsTotal(); n != setups {
		t.Fatalf("candidate setups while paused = %d, want 0", n-setups)
	}
	if st, le := hs.engineState(); st != StateConnected || le != nil || hs.h.fatalCount() != 0 {
		t.Fatalf("after 20 min paused: engine = %s / %v, fatal %d; want connected, no cap", st, le, hs.h.fatalCount())
	}
	// Wake 1s after the last paused tick, so the wake tick's next retry
	// differs from the timer already armed and the loop re-arms.
	hs.clock.advanceTo(hs.clock.Now().Add(time.Second))
	resumedAt := hs.clock.Now().Sub(r.start)
	hs.e.Wake()
	// The schedule resumes at once, no timer fired.
	deadline := time.Now().Add(time.Second)
	for hs.racesTotal() == races {
		if time.Now().After(deadline) {
			t.Fatal("no tick within 1s of Wake")
		}
		time.Sleep(time.Millisecond)
	}
	// Counted time: pausedAt before the pause, the rest after Wake.
	r.next = nil // the wake re-armed the schedule; take the fresh timer
	failedAt := r.runUntilCapFails(resumedAt-pausedAt, 60*time.Minute)
	counted := failedAt - (resumedAt - pausedAt)
	if counted < exhaustFailAfter || counted > exhaustFailAfter+longPendingInterval {
		t.Fatalf("failed after %v of counted (unpaused) time, want in [10m, 10m+%v]", counted, longPendingInterval)
	}
	hs.assertFailed503Once("cap after the pause")
}

// I1: a network hold while paused does no netcheck and no Phase-A; Wake
// re-judges at once.
func TestHold_PausedDoesNoIO(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()
	hs.e.Pause()
	races, checks := hs.racesTotal(), hs.net.callCount()
	for i := 0; i < 10; i++ {
		tm.fire()
		tm = hs.nextTimer()
	}
	if n := hs.racesTotal(); n != races {
		t.Fatalf("hold races while paused = %d, want 0", n-races)
	}
	if n := hs.net.callCount(); n != checks {
		t.Fatalf("hold netchecks while paused = %d, want 0", n-checks)
	}
	hs.e.Wake()
	eventually(t, "hold re-judged on Wake", func() bool { return hs.net.callCount() > checks })
	eventually(t, "hold Phase-A on Wake", func() bool { return hs.racesTotal() > races })
}

// I1: a Phase-A whose race completes after Pause must not install the fresh
// connection Pause released.
func TestPhaseA_PausedDoesNotInstall(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.servers.setAlive("a.example", true)
	hs.servers.mu.Lock()
	hs.servers.onRace = func(context.Context, string) { hs.e.Pause() }
	hs.servers.mu.Unlock()
	hs.e.mu.Lock()
	dd := hs.e.directDialer
	hs.e.mu.Unlock()
	err, quit := hs.e.phaseA(context.Background(), hs.target, dd, "wait-tick")
	if err != nil || quit {
		t.Fatalf("phaseA = %v, %v; want nil, false (the node answered)", err, quit)
	}
	if pt := hs.target.tm.PrimaryType(); pt != "" {
		t.Fatalf("a paused engine installed the re-race result (primary %q)", pt)
	}
}

// M3: behind an accept-all captive portal direct dials "succeed" (passive
// evidence fresh) while the active check reads captive. Hold ticks must
// judge active-only: no Phase-B round per tick, the hold stays 107.
func TestHold_AcceptAllCaptiveTicksActiveOnly(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkOtherResponse},
	})
	hs.e.mu.Lock()
	dt := hs.e.directTrack
	hs.e.mu.Unlock()
	dt.setLastOKForTest(time.Now())

	hs.escalate()
	hs.waitStatus("reconnecting 107", isReconnectingCode(ErrCodeCaptivePortal))
	tm := hs.nextTimer()
	setups := hs.setupsTotal()
	for i := 0; i < 5; i++ {
		dt.setLastOKForTest(time.Now()) // the portal keeps accepting direct dials
		tm.fire()
		tm = hs.nextTimer()
		if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeCaptivePortal)(s) {
			t.Fatalf("tick %d: status = %+v, want reconnecting/107", i, s)
		}
	}
	if n := hs.setupsTotal(); n != setups {
		t.Fatalf("candidate setups during the hold = %d, want 0 (a Phase-B round per tick)", n-setups)
	}
	if hs.h.sawState(StateConnected) && func() bool {
		hs.h.mu.Lock()
		defer hs.h.mu.Unlock()
		seen107 := false
		for _, s := range hs.h.statuses {
			if isReconnectingCode(ErrCodeCaptivePortal)(s) {
				seen107 = true
			} else if seen107 && s.State == StateConnected {
				return true
			}
		}
		return false
	}() {
		t.Fatal("the hold flapped 107 → connected")
	}
}

// M4: a target pending ≥ longPendingAfter ticks every longPendingInterval.
func TestRetrySchedule_LongPendingSlowsTo60s(t *testing.T) {
	if longPendingAfter != 10*time.Minute || longPendingInterval != 60*time.Second {
		t.Fatalf("longPendingAfter/Interval = %v/%v, want 10m/60s (brief M4)", longPendingAfter, longPendingInterval)
	}
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}}) // manual mode: no cap
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		at := r.peek().at.Sub(r.start)
		d := r.peek().d
		// The interval is chosen at the previous tick (at - d).
		armedAt := at - d
		switch {
		case armedAt >= longPendingAfter && d != longPendingInterval:
			t.Fatalf("timer armed at %v pending = %v, want %v", armedAt, d, longPendingInterval)
		case armedAt < longPendingAfter && d > 30*time.Second:
			t.Fatalf("timer armed at %v pending = %v, want ≤ 30s before 10 min", armedAt, d)
		}
		if _, exited := r.tick(); exited {
			t.Fatal("manual mode failed")
		}
	}
	// A net event ticks at once; the cadence stays slow afterwards.
	before := hs.racesTotal()
	hs.clock.advanceTo(hs.clock.Now().Add(time.Second)) // so the loop re-arms
	hs.e.wakeHold()
	eventually(t, "net-event tick", func() bool { return hs.racesTotal() > before })
	r.next = nil
	if d := r.peek().d; d != longPendingInterval {
		t.Fatalf("after a net event on a long-pending target: next = %v, want %v", d, longPendingInterval)
	}
}

// M8: the health monitor seeing the node answer (targetRecovered) ticks a
// scheduled target at once — status clears without waiting out the backoff.
func TestRetrySchedule_TargetRecoveredTicksAtOnce(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hm := &healthMonitor{}
	hs.e.wireHealthRecovered(hm)
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	r.tick()
	r.tick() // now on the 30s step
	hs.clock.advanceTo(hs.clock.Now().Add(holdBackoff[0]))
	hs.servers.setAlive("a.example", true)
	before := hs.servers.raceCount("a.example")
	hm.recovered(hs.target)
	eventually(t, "tick on recovered", func() bool { return hs.servers.raceCount("a.example") > before })
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
}

// M8: a recovered signal is a tick, not a re-race signal: with the node
// still failing, signals in a burst never run more than one tick per
// holdBackoff[0], and a recovered signal for a target not on the schedule
// does nothing.
func TestRetrySchedule_TargetRecoveredIsRateLimited(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hm := &healthMonitor{}
	hs.e.wireHealthRecovered(hm)

	// Not scheduled: ignored.
	hm.recovered(hs.target)
	time.Sleep(20 * time.Millisecond)
	if n := hs.racesTotal(); n != 0 {
		t.Fatalf("recovered on an unscheduled target raced %d times", n)
	}

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	before := hs.racesTotal()
	for i := 0; i < 20; i++ {
		hm.recovered(hs.target) // fake clock does not move: inside the gap
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := hs.racesTotal(); n != before {
		t.Fatalf("recovered burst within %v of the last attempt raced %d times, want 0", holdBackoff[0], n-before)
	}
}

// M9: an iface-down (101) tick skips Phase A — a dial cannot succeed — but
// still re-judges; hold and schedule alike.
func TestHold_IfaceDownTickSkipsPhaseA(t *testing.T) {
	t.Run("network hold", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB}},
			net:       []checkResult{checkNoResponse},
		})
		hs.escalate()
		hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
		tm := hs.nextTimer()
		hs.e.mu.Lock()
		nc := hs.e.netCoord
		hs.e.mu.Unlock()
		nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
		hs.drainWake()
		races := hs.racesTotal()
		for i := 0; i < 3; i++ {
			tm.fire()
			tm = hs.nextTimer()
		}
		hs.waitStatus("reconnecting 101", isReconnectingCode(ErrCodeNetworkUnavailable))
		if n := hs.racesTotal(); n != races {
			t.Fatalf("Phase-A on iface-down hold ticks = %d, want 0", n-races)
		}
	})
	t.Run("retry schedule", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
		hs.escalate()
		hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
		r := hs.startTicks()
		hs.e.mu.Lock()
		nc := hs.e.netCoord
		hs.e.mu.Unlock()
		nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
		hs.drainWake()
		races := hs.racesTotal()
		for i := 0; i < 3; i++ {
			r.tick()
		}
		if n := hs.racesTotal(); n != races {
			t.Fatalf("Phase-A on iface-down schedule ticks = %d, want 0", n-races)
		}
		if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeNetworkUnavailable)(s) {
			t.Fatalf("status = %+v, want reconnecting/101", s)
		}
	})
}

// drainWake drops a holdWake token left by a test's own net event.
func (hs *foHarness) drainWake() {
	select {
	case <-hs.e.holdWake:
	default:
	}
}

// M5: A answers at once, AAAA never: directResolve returns A without waiting.
func TestDirectResolve_StallingAAAADoesNotDelayA(t *testing.T) {
	query := func(ctx context.Context, m *mdns.Msg) (*mdns.Msg, error) {
		q := m.Question[0]
		if q.Qtype == mdns.TypeAAAA {
			<-ctx.Done() // a resolver that drops AAAA queries
			return nil, ctx.Err()
		}
		resp := new(mdns.Msg)
		resp.SetReply(m)
		resp.Answer = []mdns.RR{&mdns.A{Hdr: mdns.RR_Header{Name: q.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET}, A: net.ParseIP("203.0.113.7")}}
		return resp, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	addrs, err := directResolve(query)(ctx, "check.example")
	if err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("directResolve took %v with A answered at once, want no wait for AAAA", el)
	}
	if len(addrs) != 1 || addrs[0] != netip.MustParseAddr("203.0.113.7") {
		t.Fatalf("addrs = %v, want [203.0.113.7]", addrs)
	}

	// A empty → AAAA is used.
	v6only := func(_ context.Context, m *mdns.Msg) (*mdns.Msg, error) {
		q := m.Question[0]
		resp := new(mdns.Msg)
		resp.SetReply(m)
		if q.Qtype == mdns.TypeAAAA {
			resp.Answer = []mdns.RR{&mdns.AAAA{Hdr: mdns.RR_Header{Name: q.Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET}, AAAA: net.ParseIP("2001:db8::7")}}
		}
		return resp, nil
	}
	addrs, err = directResolve(v6only)(context.Background(), "check.example")
	if err != nil || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("2001:db8::7") {
		t.Fatalf("A empty: addrs = %v, %v; want [2001:db8::7]", addrs, err)
	}
}

// M6: the netcheck resolves with ClientConfig.NetcheckDNS when set, else the
// direct resolvers; DNS.Direct itself is never touched by it.
func TestNetcheckDNSServers(t *testing.T) {
	c := &config.ClientConfig{}
	c.DNS.Direct = []string{"114.114.114.114:53"}
	if got := netcheckDNSServers(c); len(got) != 1 || got[0] != "114.114.114.114:53" {
		t.Fatalf("no netcheck_dns: %v, want the direct resolvers", got)
	}
	c.NetcheckDNS = []string{"1.1.1.1:53", "8.8.8.8:53"}
	if got := netcheckDNSServers(c); len(got) != 2 || got[0] != "1.1.1.1:53" {
		t.Fatalf("netcheck_dns set: %v, want it", got)
	}
	if c.DNS.Direct[0] != "114.114.114.114:53" {
		t.Fatal("DNS.Direct changed")
	}
}
