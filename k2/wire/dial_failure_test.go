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
		seq, last := tm.ConnectFailures()
		if seq != uint64(i) || last.Before(before) {
			t.Fatalf("after %d failures: seq=%d last=%v", i, seq, last)
		}
	}
	// DialUDP / DialStreamUDP are user dials too.
	tm.DialUDP(context.Background(), "x:53")
	tm.DialStreamUDP(context.Background(), "x:53")
	if seq, _ := tm.ConnectFailures(); seq != 5 {
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
	if seq, last := tm.ConnectFailures(); seq != 1 || last.IsZero() {
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
			if _, last := tm.ConnectFailures(); last.IsZero() {
				t.Fatal("precondition: failure not recorded")
			}
			d.set(answer)
			tm.DialTCP(context.Background(), "x:80", nil)
			seq, last := tm.ConnectFailures()
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
			seq0, last0 := tm.ConnectFailures()
			d.set(e)
			tm.DialTCP(context.Background(), "x:80", nil)
			seq, last := tm.ConnectFailures()
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
		if seq, last := tm.ConnectFailures(); seq != 0 || !last.IsZero() {
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
			if _, last := tm.ConnectFailures(); !last.IsZero() {
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
			if seq, last := tm.ConnectFailures(); seq != 0 || !last.IsZero() {
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
	if _, last := tm.ConnectFailures(); !last.IsZero() {
		t.Fatal("tcpws success did not clear the failure")
	}
}
