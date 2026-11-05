package core

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kaitu-io/k2/wire"
)

// blockingFailDialer blocks every dial until release is closed, then fails
// with a transport (non-proxy-layer) error.
type blockingFailDialer struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingFailDialer() *blockingFailDialer {
	return &blockingFailDialer{entered: make(chan struct{}), release: make(chan struct{})}
}

func (d *blockingFailDialer) wait() error {
	d.once.Do(func() { close(d.entered) })
	<-d.release
	return errors.New("wire: open stream: timeout: no recent network activity")
}
func (d *blockingFailDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	return nil, d.wait()
}
func (d *blockingFailDialer) DialUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.wait()
}
func (d *blockingFailDialer) DialStreamUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.wait()
}
func (d *blockingFailDialer) Reset()       {}
func (d *blockingFailDialer) Close() error { return nil }

type countingReporter struct{ reports, clears atomic.Int64 }

func (r *countingReporter) ReportWireError(error) { r.reports.Add(1) }
func (r *countingReporter) ClearWireError()       { r.clears.Add(1) }

// PVE S1: a proxy dial still in flight on the old node when the engine swaps
// the outbound to another server fails seconds later; that error belongs to
// a superseded transport and must not become the session's lastError.
func TestWireOutbound_SupersededTransportErrorNotReported(t *testing.T) {
	for _, kind := range []string{"tcp", "udp"} {
		t.Run(kind, func(t *testing.T) {
			for _, swap := range []bool{true, false} {
				old := newBlockingFailDialer()
				sw := wire.NewSwappableDialer(old)
				rep := &countingReporter{}
				ob := NewWireOutbound("k2v5", sw, rep)

				done := make(chan error, 1)
				go func() {
					var err error
					if kind == "tcp" {
						_, err = ob.DialTCP(context.Background(), "example.com:443", nil)
					} else {
						_, _, err = ob.DialUDP(context.Background(), "example.com:53")
					}
					done <- err
				}()
				<-old.entered
				if swap {
					sw.Swap(newBlockingFailDialer()) // Phase B: another server
				}
				close(old.release)
				if err := <-done; err == nil {
					t.Fatal("dial unexpectedly succeeded")
				}
				got := rep.reports.Load()
				if swap && got != 0 {
					t.Fatalf("superseded transport's error reported %d times", got)
				}
				if !swap && got != 1 {
					t.Fatalf("control: current transport's error reported %d times, want 1", got)
				}
			}
		})
	}
}
