package server

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// BitTorrent is refused on the node. A torrent client in the swarm exposes the
// node's egress IP to copyright monitors; each resulting notice is a strike
// against the hosting account (2026-11-07: a Vultr Tokyo node was suspended
// over one). Clients already route BT direct in smart mode (core/tunnel.go),
// so only global-mode and old clients ever send it here.
//
// Detection is first-bytes only: the TCP handshake on the uplink, and on UDP
// the first packet a session sends to each new destination (uTP SYN, DHT,
// UDP tracker). Encrypted BT (MSE/PE) is not detected — accepted gap.

var errBTBlocked = errors.New("bittorrent blocked")

// btProtocolHeader is the fixed 20-byte prefix of a BT handshake (BEP 3).
const btProtocolHeader = "\x13BitTorrent protocol"

// btSniffReader wraps the uplink of a TCP pipe. Its first Read decides:
// anything whose first byte isn't 0x13 passes straight through with no extra
// wait; a stream that starts like the BT header is read until 20 bytes (or a
// mismatch) are in hand, then either released untouched or refused.
type btSniffReader struct {
	r       io.Reader
	decided bool
	blocked bool
}

func (s *btSniffReader) Read(p []byte) (int, error) {
	if s.decided {
		return s.r.Read(p)
	}
	n, err := s.r.Read(p)
	for n > 0 && n < len(btProtocolHeader) && n < len(p) && err == nil &&
		string(p[:n]) == btProtocolHeader[:n] {
		var m int
		m, err = s.r.Read(p[n:])
		n += m
	}
	if n == 0 {
		return 0, err
	}
	s.decided = true
	if n >= len(btProtocolHeader) && string(p[:len(btProtocolHeader)]) == btProtocolHeader {
		s.blocked = true
		return 0, errBTBlocked
	}
	return n, err
}

// btBlockLogEvery bounds DIAG: bt-blocked to one line per device per window;
// a torrent client opens thousands of connections a minute.
const btBlockLogEvery = 10 * time.Minute

// btBlockLogMax caps the per-device table; on overflow it is reset (worst
// case: a few extra log lines).
const btBlockLogMax = 4096

type btBlockStat struct {
	last       time.Time
	suppressed int
}

// btBlockLog rate-limits DIAG: bt-blocked per device. Zero value is ready.
type btBlockLog struct {
	mu deadlock.Mutex
	m  map[DeviceKey]*btBlockStat
}

// note records one block for k at now and reports whether to log it, with the
// number of blocks suppressed since the device's last logged line.
func (l *btBlockLog) note(k DeviceKey, now time.Time) (emit bool, suppressed int) {
	k.Session = "" // one window per device, across its sessions
	l.mu.Lock()
	if l.m == nil || len(l.m) >= btBlockLogMax {
		l.m = make(map[DeviceKey]*btBlockStat)
	}
	st := l.m[k]
	if st == nil {
		l.m[k] = &btBlockStat{last: now}
		l.mu.Unlock()
		return true, 0
	}
	if now.Sub(st.last) < btBlockLogEvery {
		st.suppressed++
		l.mu.Unlock()
		return false, 0
	}
	suppressed = st.suppressed
	st.last, st.suppressed = now, 0
	l.mu.Unlock()
	return true, suppressed
}

// reportBTBlocked logs a refused BT flow, rate-limited per device. log should
// already carry the client's remote address.
func (h *ProxyHandler) reportBTBlocked(log *slog.Logger, k DeviceKey, transport, addr string) {
	emit, suppressed := h.btLog.note(k, time.Now())
	if !emit {
		return
	}
	log.Info("DIAG: bt-blocked", "udid", k.UDID, "transport", transport,
		"dest", addr, "suppressed", suppressed)
}
