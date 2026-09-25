//go:build linux

package gateway

import (
	"net"
	"os"
	"testing"
	"time"
)

// ── TPROXY UDP reply source ──────────────────────────────────────
//
// A TPROXY'd UDP reply must leave with the ORIGINAL destination as its
// source (216.239.35.0:123, not <router>:12345). Writing through the shared
// listener socket puts the router's own address on the wire; an unconnected
// client socket still receives it, but every connect()ed UDP socket (NTP,
// QUIC, games) drops it in the kernel — observed on OpenWrt 2026-09-25.

type recordingWriter struct {
	to   net.Addr
	data []byte
}

func (w *recordingWriter) WriteTo(b []byte, addr net.Addr) (int, error) {
	w.to = addr
	w.data = append([]byte(nil), b...)
	return len(b), nil
}

type fakeReplier struct {
	askedFor *net.UDPAddr
	w        *recordingWriter
	err      error
}

func (f *fakeReplier) replyConn(dst *net.UDPAddr) (udpWriter, error) {
	f.askedFor = dst
	if f.err != nil {
		return nil, f.err
	}
	return f.w, nil
}

func loopbackUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestTProxyUDPConn_WriteTo_RepliesFromOriginalDst(t *testing.T) {
	listener := loopbackUDP(t)
	src := &net.UDPAddr{IP: net.IPv4(10, 30, 0, 191), Port: 40404}
	dst := &net.UDPAddr{IP: net.IPv4(216, 239, 35, 0), Port: 123}
	rep := &fakeReplier{w: &recordingWriter{}}

	c := newTProxyUDPConn(listener, rep, src, dst, []byte("q"))
	if _, err := c.WriteTo([]byte("reply"), nil); err != nil {
		t.Fatal(err)
	}
	if rep.askedFor == nil || rep.askedFor.String() != dst.String() {
		t.Fatalf("reply socket must be bound to the original dst %s, asked for %v", dst, rep.askedFor)
	}
	if rep.w.to == nil || rep.w.to.String() != src.String() {
		t.Fatalf("reply must go to the LAN source %s, went to %v", src, rep.w.to)
	}
	if string(rep.w.data) != "reply" {
		t.Fatalf("payload mismatch: %q", rep.w.data)
	}
}

func TestTProxyUDPConn_WriteTo_FallsBackToListenerWhenReplierFails(t *testing.T) {
	listener := loopbackUDP(t)
	client := loopbackUDP(t)
	src := client.LocalAddr().(*net.UDPAddr)
	dst := &net.UDPAddr{IP: net.IPv4(216, 239, 35, 0), Port: 123}
	rep := &fakeReplier{err: os.ErrPermission}

	c := newTProxyUDPConn(listener, rep, src, dst, []byte("q"))
	if _, err := c.WriteTo([]byte("reply"), nil); err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, from, err := client.ReadFrom(buf)
	if err != nil {
		t.Fatalf("fallback reply never arrived: %v", err)
	}
	if string(buf[:n]) != "reply" || from.String() != listener.LocalAddr().String() {
		t.Fatalf("got %q from %v", buf[:n], from)
	}
}

// ── reply socket cache ───────────────────────────────────────────

func TestReplySocketCache_ReusesPerDstAndEvictsIdle(t *testing.T) {
	binds := 0
	cache := newReplySocketCache(func(dst *net.UDPAddr) (*net.UDPConn, error) {
		binds++
		return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	})
	defer cache.close()

	a := &net.UDPAddr{IP: net.IPv4(216, 239, 35, 0), Port: 123}
	b := &net.UDPAddr{IP: net.IPv4(216, 239, 35, 0), Port: 124}

	w1, err := cache.replyConn(a)
	if err != nil {
		t.Fatal(err)
	}
	w2, _ := cache.replyConn(a)
	if w1 != w2 {
		t.Error("same dst must reuse the same socket")
	}
	if _, err := cache.replyConn(b); err != nil {
		t.Fatal(err)
	}
	if binds != 2 {
		t.Fatalf("expected 2 binds (one per dst), got %d", binds)
	}

	// Everything is idle now; a sweep with a zero idle budget must drop both.
	cache.sweep(0)
	if n := cache.len(); n != 0 {
		t.Fatalf("expected empty cache after sweep, have %d", n)
	}
	if _, err := cache.replyConn(a); err != nil {
		t.Fatal(err)
	}
	if binds != 3 {
		t.Fatalf("evicted dst must be re-bound, binds=%d", binds)
	}
	// A socket touched just now must survive a sweep with a generous budget.
	cache.sweep(time.Hour)
	if n := cache.len(); n != 1 {
		t.Fatalf("fresh socket must survive, have %d", n)
	}
}

// The real bind needs CAP_NET_ADMIN (IP_TRANSPARENT). Runs on the lab VMs as
// root; skipped elsewhere so CI stays honest instead of green-by-skip-noise.
func TestTransparentBind_RequiresRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("IP_TRANSPARENT bind needs root")
	}
	for _, dst := range []*net.UDPAddr{
		{IP: net.IPv4(203, 0, 113, 9), Port: 123},
		{IP: net.ParseIP("2001:db8::9"), Port: 123},
	} {
		c, err := transparentBind(dst)
		if err != nil {
			t.Fatalf("transparent bind %s: %v", dst, err)
		}
		if got := c.LocalAddr().String(); got != dst.String() {
			t.Errorf("bound to %s, want %s", got, dst)
		}
		c.Close()
	}
}
