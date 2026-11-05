package wire

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// fnDialer is a Dialer whose DialTCP/DialUDP/DialStreamUDP outcome is
// programmable; block (when non-nil) is waited on before returning.
type fnDialer struct {
	mu      sync.Mutex
	err     error
	block   chan struct{}
	entered chan struct{} // closed (once) when a blocking dial starts
	once    sync.Once
}

func (d *fnDialer) set(err error) {
	d.mu.Lock()
	d.err = err
	d.mu.Unlock()
}

func (d *fnDialer) outcome() error {
	d.mu.Lock()
	err, block := d.err, d.block
	d.mu.Unlock()
	if block != nil {
		d.once.Do(func() { close(d.entered) })
		<-block
	}
	return err
}

func (d *fnDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	if err := d.outcome(); err != nil {
		return nil, err
	}
	c1, c2 := net.Pipe()
	c2.Close()
	return c1, nil
}
func (d *fnDialer) DialUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.outcome()
}
func (d *fnDialer) DialStreamUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.outcome()
}
func (d *fnDialer) Reset()       {}
func (d *fnDialer) Close() error { return nil }

func newFailTM(err error) (*TransportManager, *fnDialer) {
	d := &fnDialer{err: err}
	tm := NewTransportManager()
	tm.SetPrimary(d, "quic-443")
	return tm, d
}

var errRefused = errors.New("wire: QUIC dial: connection refused")

func TestConnectFailures_TransportFailureRecorded(t *testing.T) {
	tm, _ := newFailTM(errRefused)
	before := time.Now()
	for i := 1; i <= 3; i++ {
		if _, err := tm.DialTCP(context.Background(), "x:80", nil); err == nil {
			t.Fatal("dial unexpectedly succeeded")
		}
		seq, last, _ := cfTuple(tm)
		if seq != uint64(i) || last.Before(before) {
			t.Fatalf("after %d failures: seq=%d last=%v", i, seq, last)
		}
	}
	// DialUDP / DialStreamUDP are user dials too.
	tm.DialUDP(context.Background(), "x:53")
	tm.DialStreamUDP(context.Background(), "x:53")
	if seq, _, _ := cfTuple(tm); seq != 5 {
		t.Fatalf("seq=%d after 3 TCP + 2 UDP failures, want 5", seq)
	}
}

func TestConnectFailures_MetadataRejectRecorded(t *testing.T) {
	// An auth rejection at metadata closes the connection before it is cached:
	// it is exactly the S8 node (402) and must count, although it is a
	// ProxyLayerError.
	rej := &ProxyLayerError{Err: fmt.Errorf("%w: %w", ErrMetadataRejected, &StreamError{Code: 402, Message: "payment"})}
	tm, _ := newFailTM(rej)
	tm.DialTCP(context.Background(), "x:80", nil)
	if seq, last, _ := cfTuple(tm); seq != 1 || last.IsZero() {
		t.Fatalf("metadata reject not recorded: seq=%d last=%v", seq, last)
	}
}

func TestConnectFailures_AnsweredClears(t *testing.T) {
	cases := map[string]error{
		"success":                  nil,
		"target refused by server": &StreamError{Code: 1, Message: "connection refused"},
		"stream rejected":          &ProxyLayerError{Err: fmt.Errorf("%w: nope", ErrStreamRejected)},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			tm, d := newFailTM(errRefused)
			tm.DialTCP(context.Background(), "x:80", nil)
			if _, last, _ := cfTuple(tm); last.IsZero() {
				t.Fatal("precondition: failure not recorded")
			}
			d.set(answer)
			tm.DialTCP(context.Background(), "x:80", nil)
			seq, last, _ := cfTuple(tm)
			if !last.IsZero() {
				t.Fatalf("answered dial (%v) did not clear the failure", answer)
			}
			if seq != 1 {
				t.Fatalf("seq=%d, want 1 (an answer is not a failure)", seq)
			}
		})
	}
}

func TestConnectFailures_NoOpinionCases(t *testing.T) {
	cases := map[string]error{
		"response timeout on live conn": readResponseError(fmt.Errorf("read: %w", errTimeout{})),
		"not connected":                 ErrNotConnected,
		"relay unhealthy":               ErrDatagramRelayUnhealthy,
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			tm, d := newFailTM(errRefused)
			tm.DialTCP(context.Background(), "x:80", nil)
			seq0, last0, _ := cfTuple(tm)
			d.set(e)
			tm.DialTCP(context.Background(), "x:80", nil)
			seq, last, _ := cfTuple(tm)
			if seq != seq0 || !last.Equal(last0) {
				t.Fatalf("%v changed evidence: seq %d→%d last %v→%v", e, seq0, seq, last0, last)
			}
		})
	}
	t.Run("caller ctx cancelled", func(t *testing.T) {
		tm, _ := newFailTM(errRefused)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tm.DialTCP(ctx, "x:80", nil)
		if seq, last, _ := cfTuple(tm); seq != 0 || !last.IsZero() {
			t.Fatalf("a dial the caller abandoned was recorded: seq=%d", seq)
		}
	})
}

type errTimeout struct{}

func (errTimeout) Error() string   { return "i/o timeout" }
func (errTimeout) Timeout() bool   { return true }
func (errTimeout) Temporary() bool { return true }

func TestConnectFailures_ClearedByResetsAndReplace(t *testing.T) {
	clears := map[string]func(tm *TransportManager){
		"Reset":            func(tm *TransportManager) { tm.Reset() },
		"ResetConnections": func(tm *TransportManager) { tm.ResetConnections() },
		"ReplacePrimary":   func(tm *TransportManager) { tm.ReplacePrimary(&fnDialer{err: errRefused}, "quic-443") },
	}
	for name, clear := range clears {
		t.Run(name, func(t *testing.T) {
			tm, _ := newFailTM(errRefused)
			tm.DialTCP(context.Background(), "x:80", nil)
			clear(tm)
			if _, last, _ := cfTuple(tm); !last.IsZero() {
				t.Fatalf("%s left the pre-%s failure unresolved", name, name)
			}
		})
		t.Run(name+"/straddling dial not recorded", func(t *testing.T) {
			tm, d := newFailTM(errRefused)
			d.block = make(chan struct{})
			d.entered = make(chan struct{})
			done := make(chan struct{})
			go func() {
				tm.DialTCP(context.Background(), "x:80", nil)
				close(done)
			}()
			<-d.entered // the dial has read connGen and is in the dialer
			clear(tm)
			close(d.block)
			<-done
			if seq, last, _ := cfTuple(tm); seq != 0 || !last.IsZero() {
				t.Fatalf("a dial started before %s recorded evidence after it: seq=%d", name, seq)
			}
		})
	}
}

func TestConnectFailures_TCPWSFallbackSuccessClears(t *testing.T) {
	// QUIC primary refuses, TCP-WS answers: the node is alive.
	tm, _ := newFailTM(errRefused)
	tm.DialTCP(context.Background(), "x:80", nil) // no tcpws yet: failure
	tm.SetTCPWS(&fnDialer{})
	if _, err := tm.DialTCP(context.Background(), "x:80", nil); err != nil {
		t.Fatalf("tcpws fallback dial failed: %v", err)
	}
	if _, last, _ := cfTuple(tm); !last.IsZero() {
		t.Fatal("tcpws success did not clear the failure")
	}
}

// Review C1: k2s answers the code-less legacy "unauthorized" when it cannot
// reach Center (undecidable verdict under enforce). That is not this node
// being dead — and engine maps it to a terminal 401 — so it must record
// nothing, neither failure nor answer. Coded rejects (402 subscription, 503
// node auth fault, 401/403 Center verdict) are node dial failures.
func TestConnectFailures_OnlyCodedMetadataRejectsCount(t *testing.T) {
	codeLess := &ProxyLayerError{Err: fmt.Errorf("%w: %w", ErrMetadataRejected,
		fmt.Errorf("%w: unauthorized", ErrStreamRejected))}
	t.Run("code-less records nothing", func(t *testing.T) {
		tm, _ := newFailTM(codeLess)
		for i := 0; i < 5; i++ {
			tm.DialTCP(context.Background(), "x:80", nil)
		}
		if seq, last, _ := cfTuple(tm); seq != 0 || !last.IsZero() {
			t.Fatalf("code-less reject recorded: seq=%d last=%v", seq, last)
		}
	})
	t.Run("code-less does not clear an earlier failure", func(t *testing.T) {
		tm, d := newFailTM(errRefused)
		tm.DialTCP(context.Background(), "x:80", nil)
		d.set(codeLess)
		tm.DialTCP(context.Background(), "x:80", nil)
		if seq, last, _ := cfTuple(tm); seq != 1 || last.IsZero() {
			t.Fatalf("code-less reject changed evidence: seq=%d last=%v", seq, last)
		}
	})
	for _, code := range []int{401, 402, 403, metadataRejectCodeNodeFault} {
		t.Run(fmt.Sprintf("coded %d records", code), func(t *testing.T) {
			rej := &ProxyLayerError{Err: fmt.Errorf("%w: %w", ErrMetadataRejected, &StreamError{Code: code, Message: "x"})}
			tm, _ := newFailTM(rej)
			tm.DialTCP(context.Background(), "x:80", nil)
			if seq, last, _ := cfTuple(tm); seq != 1 || last.IsZero() {
				t.Fatalf("coded %d reject not recorded: seq=%d", code, seq)
			}
		})
	}
}

// Review I1 support: every clear moves the generation reported to engine.
func TestConnectFailures_GenMovesOnEveryClear(t *testing.T) {
	tm, _ := newFailTM(errRefused)
	_, _, g0 := cfTuple(tm)
	tm.DialTCP(context.Background(), "x:80", nil)
	if _, _, g := cfTuple(tm); g != g0 {
		t.Fatalf("a dial moved gen %d→%d", g0, g)
	}
	tm.ResetConnections()
	_, _, g1 := cfTuple(tm)
	tm.Reset()
	_, _, g2 := cfTuple(tm)
	tm.ReplacePrimary(&fnDialer{err: errRefused}, "quic-443")
	_, _, g3 := cfTuple(tm)
	if !(g0 < g1 && g1 < g2 && g2 < g3) {
		t.Fatalf("gen did not move on each clear: %d %d %d %d", g0, g1, g2, g3)
	}
}

func cfTuple(tm *TransportManager) (uint64, time.Time, uint64) {
	cf := tm.ConnectFailures()
	return cf.Seq, cf.Last, cf.Gen
}

// First marks the start of the current unresolved run of failures: it stays
// put while failures continue, and resets on an answer or a clear.
func TestConnectFailures_FirstBoundsTheUnresolvedRun(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	now := base
	tm, d := newFailTM(errRefused)
	tm.SetConnFailClock(func() time.Time { return now })
	tm.DialTCP(context.Background(), "x:80", nil)
	now = base.Add(5 * time.Second)
	tm.DialTCP(context.Background(), "x:80", nil)
	cf := tm.ConnectFailures()
	if !cf.First.Equal(base) || !cf.Last.Equal(now) {
		t.Fatalf("run: first=%v last=%v, want first=%v last=%v", cf.First, cf.Last, base, now)
	}
	d.set(nil) // answered
	tm.DialTCP(context.Background(), "x:80", nil)
	if cf := tm.ConnectFailures(); !cf.First.IsZero() || !cf.Last.IsZero() {
		t.Fatalf("answer did not end the run: %+v", cf)
	}
	d.set(errRefused)
	now = base.Add(9 * time.Second)
	tm.DialTCP(context.Background(), "x:80", nil)
	if cf := tm.ConnectFailures(); !cf.First.Equal(now) {
		t.Fatalf("new run first=%v, want %v", cf.First, now)
	}
	tm.Reset()
	if cf := tm.ConnectFailures(); !cf.First.IsZero() {
		t.Fatalf("Reset did not end the run: %+v", cf)
	}
}
