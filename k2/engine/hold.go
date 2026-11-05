package engine

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

// Hold — waiting for the user's network inside outboundReplaceLoop.
//
// When a node looks dead, the replace loop first asks judgeNet whether the
// user's own network works. If it does not (interface down / no internet /
// captive portal) the engine holds: the tunnel and TUN stay up, status reads
// reconnecting + the verdict's reason (holdErr, see buildStatusLocked), and no
// replacement candidate is spent. The hold ends when judgeNet says online.
//
// Everything here runs on the replace loop's single goroutine. e.mu sections
// only read or write fields; netcheck I/O, netCoordinator calls and status
// pushes happen outside it.

// holdBackoff is the hold's re-judge cadence without network events: 5s, 10s,
// 30s, then 30s forever (the last entry repeats). No upper bound on the hold —
// the user can always disconnect.
var holdBackoff = []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}

// realHoldTimer is the production holdTimerFn.
func realHoldTimer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// judgeNet decides whether the user's network reaches the internet, from the
// cheapest evidence first (netCoordinator interface state, passive direct
// dials within passiveFreshness, then the active netcheck), and logs one
// DIAG: netcheck line. trigger: pre-phaseB, pre-exhaust, hold-tick, net-event.
//
// If ctx is cancelled mid-check the active probes all report no response and
// the verdict reads no-internet — callers must check ctx.Err() first.
func (e *Engine) judgeNet(ctx context.Context, trigger string) netVerdict {
	e.mu.Lock()
	nc := e.netCoord
	dt := e.directTrack
	run := e.netcheck
	targets := e.netcheckTargets
	e.mu.Unlock()

	ev := netEvidence{Now: time.Now()}
	if nc != nil {
		ev.IfaceDown = !nc.isNetworkUp()
	}
	if dt != nil {
		ev.LastDirectOK = dt.LastOK()
	}
	var rs []checkResult
	start := time.Now()
	v, evidence := decideNet(ev, func() []checkResult {
		if run != nil {
			rs = run(ctx)
		}
		return rs
	})
	attrs := []any{
		"trigger", trigger,
		"result", int(v),
		"verdict", v.String(),
		"evidence", evidence,
		"checks", formatCheckResults(targets, rs),
		"ms", time.Since(start).Milliseconds(),
	}
	if v == netOnline {
		slog.Info("DIAG: netcheck", attrs...)
	} else {
		slog.Warn("DIAG: netcheck", attrs...)
	}
	return v
}

// setHold sets (non-nil) or clears (nil) the hold reason and pushes the
// resulting status. A no-op once the session is over (ctx cancelled by
// Stop/fail, or state no longer connected): Stop and fail clear holdErr
// themselves, and a late write must never land on a following session.
// Skips the push when the reason is unchanged.
func (e *Engine) setHold(ctx context.Context, ee *EngineError) {
	e.mu.Lock()
	if ctx.Err() != nil || e.state != StateConnected {
		e.mu.Unlock()
		return
	}
	if sameHold(e.holdErr, ee) {
		e.mu.Unlock()
		return
	}
	e.setHoldLocked(ee)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

func sameHold(a, b *EngineError) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Code == b.Code
}

// holdUntilOnline holds on verdict v (not netOnline) until judgeNet reports
// online (clears the hold, returns true) or ctx is cancelled (returns false;
// Stop/fail own clearing the hold then). Each wake — backoff timer or a
// network event via holdWake — re-judges; a changed reason is pushed.
//
// A network event restarts the backoff at its first step: a fresh network
// often needs a few seconds (DHCP, portal redirect) before it answers.
func (e *Engine) holdUntilOnline(ctx context.Context, target *outboundTransport, v netVerdict) bool {
	e.mu.Lock()
	name := target.name
	timerFn := e.holdTimerFn
	e.mu.Unlock()
	if timerFn == nil {
		timerFn = realHoldTimer
	}

	slog.Warn("engine: holding for the network", "outbound", name, "verdict", v.String())
	e.setHold(ctx, v.holdError())
	step := 0
	for {
		d := holdBackoff[min(step, len(holdBackoff)-1)]
		step++
		ch, stop := timerFn(d)
		trigger := "hold-tick"
		select {
		case <-ctx.Done():
			stop()
			return false
		case <-ch:
		case <-e.holdWake:
			stop()
			trigger = "net-event"
			step = 0
		}
		v = e.judgeNet(ctx, trigger)
		if ctx.Err() != nil {
			return false
		}
		if v == netOnline {
			slog.Info("engine: network back, leaving hold", "outbound", name)
			e.setHold(ctx, nil)
			return true
		}
		e.setHold(ctx, v.holdError())
	}
}

// wakeHold pokes a running hold to re-judge now. Non-blocking; a pending
// wake is enough (cap 1).
func (e *Engine) wakeHold() {
	select {
	case e.holdWake <- struct{}{}:
	default:
	}
}

// directResolve adapts a DNS query func (DirectDNSClient.Query) into the
// netcheck resolver. A and AAAA are queried in parallel; IPv4 addresses are
// returned first because netcheck dials addresses in order inside a single
// 3s budget — on a network that blackholes IPv6, AAAA first would spend the
// whole budget there and report a false no-internet.
func directResolve(query func(context.Context, *mdns.Msg) (*mdns.Msg, error)) func(context.Context, string) ([]netip.Addr, error) {
	return func(ctx context.Context, host string) ([]netip.Addr, error) {
		var v4, v6 []netip.Addr
		var err4, err6 error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); v4, err4 = lookupType(ctx, query, host, mdns.TypeA) }()
		go func() { defer wg.Done(); v6, err6 = lookupType(ctx, query, host, mdns.TypeAAAA) }()
		wg.Wait()
		addrs := append(v4, v6...)
		if len(addrs) > 0 {
			return addrs, nil
		}
		if err4 != nil {
			return nil, err4
		}
		if err6 != nil {
			return nil, err6
		}
		return nil, errors.New("netcheck: no addresses for " + host)
	}
}

func lookupType(ctx context.Context, query func(context.Context, *mdns.Msg) (*mdns.Msg, error), host string, qtype uint16) ([]netip.Addr, error) {
	m := new(mdns.Msg)
	m.SetQuestion(mdns.Fqdn(host), qtype)
	m.RecursionDesired = true
	resp, err := query(ctx, m)
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, rr := range resp.Answer {
		var ip []byte
		switch r := rr.(type) {
		case *mdns.A:
			ip = r.A
		case *mdns.AAAA:
			ip = r.AAAA
		default:
			continue
		}
		if a, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, a.Unmap())
		}
	}
	return out, nil
}
