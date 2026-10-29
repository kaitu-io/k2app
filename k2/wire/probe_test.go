package wire

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
)

func TestComputeProbeScore_Unreachable(t *testing.T) {
	if got := (ProbeStats{Reachable: false}).Score(); got != 0 {
		t.Errorf("unreachable score = %v, want 0", got)
	}
}

func TestComputeProbeScore_EchoUnsupported(t *testing.T) {
	s := ProbeStats{Reachable: true, EchoSupported: false, AvgRttMs: 100}
	if got := s.Score(); got != -1 {
		t.Errorf("unsupported score = %v, want -1 sentinel", got)
	}
}

func TestComputeProbeScore_Excellent(t *testing.T) {
	s := ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 30, LossRate: 0, JitterMs: 10}
	got := s.Score()
	if got < 0.80 || got > 0.86 {
		t.Errorf("excellent score = %v, want ~0.83", got)
	}
}

func TestComputeProbeScore_JitterPenalty(t *testing.T) {
	low := ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 100, JitterMs: 20}
	high := ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 100, JitterMs: 300}
	if low.Score() <= high.Score() {
		t.Errorf("low-jitter score %v must exceed high-jitter score %v", low.Score(), high.Score())
	}
}

func TestComputeProbeScore_JitterClamp(t *testing.T) {
	insane := ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 100, JitterMs: 10000}
	rttScore := 1.0 / (1.0 + 100.0/150.0)
	minExpected := rttScore * 0.70 * 0.999 // allow tiny float fuzz
	if insane.Score() < minExpected {
		t.Errorf("clamped jitter score = %v, want >= %v (penalty floor 0.7)", insane.Score(), minExpected)
	}
}

func TestComputeProbeScore_FullLoss(t *testing.T) {
	s := ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 100, LossRate: 1, JitterMs: 0}
	if got := s.Score(); got != 0 {
		t.Errorf("full-loss score = %v, want 0", got)
	}
}

// probeTestEchoServer reuses the wire package's echoTestServer helper (defined
// in echo_test.go) and handles N sequential echo streams per connection.
func probeTestEchoServer(t *testing.T, rounds int) (*quic.Listener, *net.UDPAddr, func()) {
	ln, addr, cleanup := echoTestServer(t)

	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		for i := 0; i < rounds; i++ {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				return
			}
			br := bufio.NewReader(stream)
			if _, _, err := ReadVarint(br); err != nil {
				return
			}
			if _, _, err := ReadStreamHeader(br); err != nil {
				return
			}
			HandleEchoStream(stream, br)
		}
	}()

	return ln, addr, cleanup
}

// probeTestUnsupportedServer: accept stream, read header, then close with a
// stream reset (looks like an old server without echo support).
func probeTestUnsupportedServer(t *testing.T) (*quic.Listener, *net.UDPAddr, func()) {
	ln, addr, cleanup := echoTestServer(t)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		// Reset the stream — isEchoUnsupported treats this as "old server".
		stream.CancelRead(0)
		stream.CancelWrite(0)
	}()
	return ln, addr, cleanup
}

func TestProbeConnection_FiveRounds(t *testing.T) {
	_, addr, cleanupServer := probeTestEchoServer(t, 5)
	defer cleanupServer()

	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stats, err := probeConnection(ctx, conn, 5, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("probeConnection: %v", err)
	}
	if !stats.Reachable || !stats.EchoSupported {
		t.Errorf("expected reachable + supported, got %+v", stats)
	}
	if stats.LossRate != 0 {
		t.Errorf("loss = %v, want 0", stats.LossRate)
	}
	if stats.AvgRttMs <= 0 {
		t.Errorf("avgRtt = %v, want > 0", stats.AvgRttMs)
	}
	if stats.MinRttMs > stats.AvgRttMs || stats.AvgRttMs > stats.MaxRttMs {
		t.Errorf("min/avg/max ordering wrong: %v / %v / %v",
			stats.MinRttMs, stats.AvgRttMs, stats.MaxRttMs)
	}
}

func TestProbeConnection_UnsupportedServer(t *testing.T) {
	_, addr, cleanupServer := probeTestUnsupportedServer(t)
	defer cleanupServer()

	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stats, err := probeConnection(ctx, conn, 5, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("probeConnection: %v", err)
	}
	if !stats.Reachable {
		t.Error("Reachable should be true on unsupported server")
	}
	if stats.EchoSupported {
		t.Error("EchoSupported should be false")
	}
	if score := stats.Score(); score != -1 {
		t.Errorf("Score = %v, want -1 sentinel", score)
	}
}

// fakeDirectDialer: minimal DirectDialer satisfying wire.DirectDialer for
// ProbeURL tests that don't need interface binding.
type fakeDirectDialer struct{}

func (fakeDirectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}
func (fakeDirectDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}

func TestProbeURL_Unreachable(t *testing.T) {
	// Port 1 on loopback is reliably refused on all supported OSes.
	url := "k2v5://u:t@127.0.0.1:1?insecure=1"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stats, err := ProbeURL(ctx, url, fakeDirectDialer{}, 3, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if stats.Reachable {
		t.Errorf("stats.Reachable = true, want false")
	}
	if stats.MeasuredAt.IsZero() {
		t.Errorf("MeasuredAt should be set even on error")
	}
}

func TestProbeURL_BadURL(t *testing.T) {
	_, err := ProbeURL(context.Background(), "not-a-url", fakeDirectDialer{}, 3, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error for bad URL")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "scheme") {
		t.Errorf("err = %v, want parse/scheme error", err)
	}
}
