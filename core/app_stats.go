package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

// AppRouteStat is one process's routing outcomes within a reporting window.
type AppRouteStat struct {
	Process string
	Direct  int64 // connections sent out the direct outbound
	Proxy   int64 // connections sent through a wire outbound
	Reject  int64 // connections dropped by a reject route
	Fail    int64 // of Direct+Proxy, dials that failed

	// LastFail is the most recent failed dial ("direct/1.2.3.4:443"), kept so
	// the summary names one concrete destination to chase.
	LastFail string
}

// appStatsCap bounds the per-window table. Past it, new process names are
// folded into one overflow row instead of growing the map: the summary is a
// diagnostic, and a machine running thousands of distinct short-lived
// processes must not turn it into a memory leak.
const appStatsCap = 256

const appStatsOverflow = "(other)"

// appStats tallies routing outcomes per attributed process. It answers the
// question per-connection logs answer at debug level — "where did THIS app's
// traffic go, and did it get there" — at info level, for the price of one
// line per heartbeat (engine's "DIAG: app-routes").
//
// Only attributed connections are counted, so the table stays empty (and the
// line silent) unless a loaded rule asked for process attribution.
type appStats struct {
	mu deadlock.Mutex
	m  map[string]*AppRouteStat
}

func (s *appStats) row(process string) *AppRouteStat {
	if s.m == nil {
		s.m = make(map[string]*AppRouteStat)
	}
	r := s.m[process]
	if r == nil {
		if len(s.m) >= appStatsCap {
			process = appStatsOverflow
			if r = s.m[process]; r != nil {
				return r
			}
		}
		r = &AppRouteStat{Process: process}
		s.m[process] = r
	}
	return r
}

// record counts one routed connection. outbound == nil is a reject route;
// err is the dial result for the other two.
func (s *appStats) record(process string, outbound Outbound, dest string, err error) {
	if process == "" {
		return
	}
	var d AppRouteStat // this connection's contribution
	switch {
	case outbound == nil:
		d.Reject = 1
	case isDirectOutbound(outbound):
		d.Direct = 1
		d.LastFail = "direct/" + dest
	default:
		d.Proxy = 1
		d.LastFail = "proxy/" + dest
	}
	if err != nil {
		d.Fail = 1
	} else {
		d.LastFail = ""
	}

	s.mu.Lock()
	r := s.row(process)
	r.Direct += d.Direct
	r.Proxy += d.Proxy
	r.Reject += d.Reject
	r.Fail += d.Fail
	if d.LastFail != "" {
		r.LastFail = d.LastFail
	}
	s.mu.Unlock()
}

func isDirectOutbound(ob Outbound) bool {
	_, direct := ob.(*directOutbound)
	return direct
}

// take returns the window's rows, busiest first, and starts a new window.
func (s *appStats) take() []AppRouteStat {
	s.mu.Lock()
	m := s.m
	s.m = nil
	s.mu.Unlock()

	out := make([]AppRouteStat, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].total(), out[j].total()
		if ti != tj {
			return ti > tj
		}
		return out[i].Process < out[j].Process
	})
	return out
}

func (r AppRouteStat) total() int64 { return r.Direct + r.Proxy + r.Reject }

// FormatAppRouteStats renders up to max rows as one log value:
//
//	Doubao Browser Helper: direct=41 proxy=0 fail=0; curl: direct=0 proxy=3 fail=1 lastFail=proxy/1.2.3.4:443
//
// reject is printed only when non-zero. Rows past max are summarized as
// "+N more".
func FormatAppRouteStats(rows []AppRouteStat, max int) string {
	var b strings.Builder
	for i, r := range rows {
		if i == max {
			fmt.Fprintf(&b, "; +%d more", len(rows)-max)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: direct=%d proxy=%d", r.Process, r.Direct, r.Proxy)
		if r.Reject > 0 {
			fmt.Fprintf(&b, " reject=%d", r.Reject)
		}
		fmt.Fprintf(&b, " fail=%d", r.Fail)
		if r.LastFail != "" {
			fmt.Fprintf(&b, " lastFail=%s", r.LastFail)
		}
	}
	return b.String()
}

// TakeAppRouteStats returns per-process routing outcomes since the previous
// call (busiest first) and resets the window.
func (t *ClientTunnel) TakeAppRouteStats() []AppRouteStat {
	return t.appStats.take()
}
