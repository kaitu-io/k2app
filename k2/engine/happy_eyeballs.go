package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// ipv6HeadStart is the RFC 8305 delay before starting IPv4.
// IPv6 gets this head start because it is the preferred address family.
const ipv6HeadStart = 250 * time.Millisecond

// EyeballsError is returned when both IPv6 and IPv4 probes fail.
type EyeballsError struct {
	IPv6Err error
	IPv4Err error
}

func (e *EyeballsError) Error() string {
	return fmt.Sprintf("happy eyeballs: IPv6: %v; IPv4: %v", e.IPv6Err, e.IPv4Err)
}

func (e *EyeballsError) Unwrap() []error {
	return []error{e.IPv6Err, e.IPv4Err}
}

// resolveDialAddr determines which IP address to use for wire transport.
// When both IPv6 and IPv4 are available, runs a Happy Eyeballs race.
// When only one is available, returns it directly without probing.
func resolveDialAddr(ctx context.Context, cfg wire.Config, dd wire.DirectDialer) (string, error) {
	hasIPv6 := cfg.IPv6 != ""
	hasIPv4 := cfg.IPv4 != ""

	switch {
	case hasIPv6 && hasIPv4:
		return happyEyeballs(ctx, cfg.IPv6, cfg.IPv4, cfg.Port, dd)
	case hasIPv6:
		slog.Info("engine: using IPv6 (only address)", "addr", cfg.IPv6)
		return cfg.IPv6, nil
	case hasIPv4:
		slog.Info("engine: using IPv4 (only address)", "addr", cfg.IPv4)
		return cfg.IPv4, nil
	default:
		return "", nil
	}
}

type probeResult struct {
	addr string
	conn net.Conn
	err  error
}

// happyEyeballs races IPv6 and IPv4 TCP connections per RFC 8305.
// IPv6 gets a 250ms head start. First to connect wins.
func happyEyeballs(ctx context.Context, ipv6, ipv4 string, port int, dd wire.DirectDialer) (string, error) {
	portStr := strconv.Itoa(port)
	addr6 := net.JoinHostPort(ipv6, portStr)
	addr4 := net.JoinHostPort(ipv4, portStr)

	results := make(chan probeResult, 2)
	raceCtx, raceCancel := context.WithCancel(ctx)

	var spawned, consumed int

	// Cleanup: cancel all probes, drain pending results, close stale connections.
	// Handles every exit path: winner found, both fail, ctx cancelled.
	defer func() {
		raceCancel()
		for consumed < spawned {
			if r := <-results; r.conn != nil {
				r.conn.Close()
			}
			consumed++
		}
	}()

	probe := func(addr, ip string) {
		var conn net.Conn
		var err error
		if dd != nil {
			conn, err = dd.DialContext(raceCtx, "tcp", addr)
		} else {
			var d net.Dialer
			conn, err = d.DialContext(raceCtx, "tcp", addr)
		}
		results <- probeResult{addr: ip, conn: conn, err: err}
	}

	// 1. Start IPv6 probe.
	go probe(addr6, ipv6)
	spawned++

	// 2. Wait for head start or IPv6 immediate result.
	timer := time.NewTimer(ipv6HeadStart)
	defer timer.Stop()

	var ipv6Err, ipv4Err error

	select {
	case r := <-results:
		consumed++
		if r.err == nil {
			r.conn.Close()
			slog.Info("engine: happy eyeballs: IPv6 won", "addr", ipv6)
			return r.addr, nil
		}
		ipv6Err = r.err
		slog.Debug("engine: happy eyeballs: IPv6 failed immediately", "err", r.err)
	case <-timer.C:
		// IPv6 still in progress, start IPv4 in parallel.
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// 3. Start IPv4 probe.
	go probe(addr4, ipv4)
	spawned++

	// 4. Collect remaining results.
	for consumed < spawned {
		select {
		case r := <-results:
			consumed++
			if r.err == nil {
				r.conn.Close()
				family := "IPv4"
				if r.addr == ipv6 {
					family = "IPv6"
				}
				slog.Info("engine: happy eyeballs: winner", "family", family, "addr", r.addr)
				return r.addr, nil
			}
			if r.addr == ipv6 {
				ipv6Err = r.err
			} else {
				ipv4Err = r.err
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	slog.Warn("engine: happy eyeballs: both failed", "ipv6Err", ipv6Err, "ipv4Err", ipv4Err)
	return "", &EyeballsError{IPv6Err: ipv6Err, IPv4Err: ipv4Err}
}
