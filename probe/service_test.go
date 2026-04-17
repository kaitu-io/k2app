package probe

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

type staticSource struct {
	urls []string
	dd   wire.DirectDialer
}

func (s staticSource) CandidateURLs() []string       { return s.urls }
func (s staticSource) DirectDialer() wire.DirectDialer { return s.dd }

// fakeDirectDialer is a sentinel used only to prove the Source's dialer
// threads through cycle → prober. It never actually dials.
type fakeDirectDialer struct{ id string }

func (f *fakeDirectDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}
func (f *fakeDirectDialer) ListenPacket(string, string) (net.PacketConn, error) {
	return nil, nil
}

// fakeProber counts invocations and observes peak concurrency.
type fakeProber struct {
	total       atomic.Int64
	inflight    atomic.Int64
	maxInflight atomic.Int64
	delay       time.Duration
	mu          sync.Mutex
	seen        []string
	seenDD      []wire.DirectDialer
}

func (f *fakeProber) ProbeURL(ctx context.Context, url string, dd wire.DirectDialer, _ int, _ time.Duration) (wire.ProbeStats, error) {
	cur := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		old := f.maxInflight.Load()
		if cur <= old || f.maxInflight.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(f.delay)
	f.mu.Lock()
	f.seen = append(f.seen, url)
	f.seenDD = append(f.seenDD, dd)
	f.mu.Unlock()
	f.total.Add(1)
	return wire.ProbeStats{
		Reachable: true, EchoSupported: true,
		AvgRttMs: 20, JitterMs: 5, MeasuredAt: time.Now(),
	}, nil
}

func TestService_RunsFirstCycleImmediately(t *testing.T) {
	reg := NewRegistry(time.Minute)
	src := staticSource{urls: []string{"k2v5://a", "k2v5://b", "k2v5://c"}}
	fp := &fakeProber{delay: 10 * time.Millisecond}

	svc := NewService(Config{
		Registry:      reg,
		Source:        src,
		Cadence:       time.Second, // long — but first cycle is immediate
		MaxConcurrent: 2,
		Rounds:        4,
		RoundInterval: 1 * time.Millisecond,
		Prober:        fp.ProbeURL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)

	// First cycle fires immediately and takes ~ ceil(3/2)*10ms = 20ms.
	time.Sleep(80 * time.Millisecond)
	cancel()
	svc.Close()

	if got := fp.total.Load(); got != 3 {
		t.Errorf("probes = %d, want 3 (one per URL)", got)
	}
	if got := fp.maxInflight.Load(); got > 2 {
		t.Errorf("peak inflight = %d, exceeded MaxConcurrent=2", got)
	}
	for _, u := range src.urls {
		if _, ok := reg.Score(u); !ok {
			t.Errorf("registry missing %s", u)
		}
	}
}

func TestService_ClosePreventsFurtherProbes(t *testing.T) {
	reg := NewRegistry(time.Minute)
	src := staticSource{urls: []string{"k2v5://a"}}
	fp := &fakeProber{}

	svc := NewService(Config{
		Registry: reg, Source: src,
		Cadence: 10 * time.Millisecond, MaxConcurrent: 1,
		Rounds: 1, RoundInterval: 1 * time.Millisecond,
		Prober: fp.ProbeURL,
	})
	svc.Start(context.Background())
	time.Sleep(35 * time.Millisecond)
	svc.Close()
	before := fp.total.Load()
	time.Sleep(50 * time.Millisecond)
	if after := fp.total.Load(); after != before {
		t.Errorf("probes ran after Close: %d → %d", before, after)
	}
}

func TestService_EmptySourceNoOp(t *testing.T) {
	reg := NewRegistry(time.Minute)
	fp := &fakeProber{}
	svc := NewService(Config{
		Registry: reg, Source: staticSource{},
		Cadence: 10 * time.Millisecond, MaxConcurrent: 1,
		Rounds: 1, RoundInterval: 1 * time.Millisecond,
		Prober: fp.ProbeURL,
	})
	svc.Start(context.Background())
	time.Sleep(50 * time.Millisecond)
	svc.Close()
	if fp.total.Load() != 0 {
		t.Errorf("expected 0 probes for empty source, got %d", fp.total.Load())
	}
}

func TestService_PassesSourceDirectDialerToProber(t *testing.T) {
	reg := NewRegistry(time.Minute)
	dd := &fakeDirectDialer{id: "source-owned"}
	src := staticSource{urls: []string{"k2v5://a", "k2v5://b"}, dd: dd}
	fp := &fakeProber{delay: 5 * time.Millisecond}

	svc := NewService(Config{
		Registry:      reg,
		Source:        src,
		Cadence:       time.Hour, // never tick — only first cycle runs
		MaxConcurrent: 2,
		Rounds:        1,
		RoundInterval: 1 * time.Millisecond,
		Prober:        fp.ProbeURL,
	})
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	time.Sleep(60 * time.Millisecond)
	cancel()
	svc.Close()

	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.seenDD) != 2 {
		t.Fatalf("prober called %d times, want 2", len(fp.seenDD))
	}
	for i, got := range fp.seenDD {
		if got != dd {
			t.Errorf("call[%d] dialer = %v, want source-owned %v", i, got, dd)
		}
	}
}

func TestService_CloseWithoutStart_NoBlock(t *testing.T) {
	svc := NewService(Config{
		Registry: NewRegistry(time.Minute),
		Source:   staticSource{},
		Prober:   (&fakeProber{}).ProbeURL,
	})
	// Close without Start must return quickly (not wait 5s).
	start := time.Now()
	svc.Close()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Close without Start took %v, expected < 500ms", elapsed)
	}
}
