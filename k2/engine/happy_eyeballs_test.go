package engine

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// startTCPListener creates a TCP listener on the given address and returns it.
// The listener accepts connections in a goroutine and immediately closes them.
func startTCPListener(t *testing.T, addr string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln
}

// portFromListener extracts the port number from a listener's address.
func portFromListener(t *testing.T, ln net.Listener) int {
	t.Helper()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestResolveDialAddr_OnlyIPv4(t *testing.T) {
	cfg := wire.Config{IPv4: "1.2.3.4", Port: 443}
	addr, err := resolveDialAddr(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "1.2.3.4" {
		t.Errorf("addr = %q, want %q", addr, "1.2.3.4")
	}
}

func TestResolveDialAddr_OnlyIPv6(t *testing.T) {
	cfg := wire.Config{IPv6: "2001:db8::1", Port: 443}
	addr, err := resolveDialAddr(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "2001:db8::1" {
		t.Errorf("addr = %q, want %q", addr, "2001:db8::1")
	}
}

func TestResolveDialAddr_NoAddresses(t *testing.T) {
	cfg := wire.Config{Port: 443}
	addr, err := resolveDialAddr(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "" {
		t.Errorf("addr = %q, want empty", addr)
	}
}

func TestHappyEyeballs_IPv6Wins(t *testing.T) {
	// Both reachable on loopback. IPv6 has 250ms head start → should win.
	ln6 := startTCPListener(t, "[::1]:0")
	ln4 := startTCPListener(t, "127.0.0.1:0")
	port6 := portFromListener(t, ln6)
	port4 := portFromListener(t, ln4)

	// Use same port for both — not possible with different listeners.
	// Instead, test resolveDialAddr with actual listeners on different ports.
	// We test happyEyeballs directly.
	_ = port4

	addr, err := happyEyeballs(context.Background(), "::1", "127.0.0.1", port6, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// IPv6 should win because it gets the head start and is reachable.
	if addr != "::1" {
		t.Errorf("winner = %q, want ::1 (IPv6)", addr)
	}
}

func TestHappyEyeballs_IPv4Wins_IPv6Unreachable(t *testing.T) {
	// IPv4 reachable, IPv6 points to a port with no listener (connection refused).
	ln4 := startTCPListener(t, "127.0.0.1:0")
	port := portFromListener(t, ln4)

	// Use the same port for IPv6 — no listener on [::1]:port.
	// First check if ::1 is available.
	if ln, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("IPv6 loopback not available")
	} else {
		ln.Close()
	}

	addr, err := happyEyeballs(context.Background(), "::1", "127.0.0.1", port, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "127.0.0.1" {
		t.Errorf("winner = %q, want 127.0.0.1 (IPv4)", addr)
	}
}

func TestHappyEyeballs_BothFail(t *testing.T) {
	// Both point to ports with no listener.
	if ln, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("IPv6 loopback not available")
	} else {
		ln.Close()
	}

	// Use port 1 — almost certainly no listener.
	_, err := happyEyeballs(context.Background(), "::1", "127.0.0.1", 1, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ee *EyeballsError
	if !errors.As(err, &ee) {
		t.Fatalf("expected EyeballsError, got %T: %v", err, err)
	}
	if ee.IPv6Err == nil {
		t.Error("IPv6Err should not be nil")
	}
	if ee.IPv4Err == nil {
		t.Error("IPv4Err should not be nil")
	}
}

func TestHappyEyeballs_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := happyEyeballs(ctx, "::1", "127.0.0.1", 443, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestHappyEyeballs_IPv6ImmediateFail_NoDelay(t *testing.T) {
	// IPv6 fails immediately (connection refused), IPv4 is reachable.
	// Should NOT wait the full 250ms head start.
	if ln, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("IPv6 loopback not available")
	} else {
		ln.Close()
	}

	ln4 := startTCPListener(t, "127.0.0.1:0")
	port := portFromListener(t, ln4)

	start := time.Now()
	addr, err := happyEyeballs(context.Background(), "::1", "127.0.0.1", port, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "127.0.0.1" {
		t.Errorf("winner = %q, want 127.0.0.1", addr)
	}
	// Should complete well under 250ms (the head start), proving no delay.
	if elapsed > 200*time.Millisecond {
		t.Errorf("took %v, expected < 200ms (no head start delay on immediate IPv6 failure)", elapsed)
	}
}

func TestHappyEyeballs_WithDirectDialer(t *testing.T) {
	// Verify DirectDialer is used for probing.
	ln := startTCPListener(t, "127.0.0.1:0")
	port := portFromListener(t, ln)

	var dialerUsed bool
	dd := &mockDirectDialer{
		dialFunc: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialerUsed = true
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}

	cfg := wire.Config{IPv4: "127.0.0.1", IPv6: "::1", Port: port}
	_, err := resolveDialAddr(context.Background(), cfg, dd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dialerUsed {
		t.Error("DirectDialer was not used for probing")
	}
}

// mockDirectDialer implements wire.DirectDialer for testing.
type mockDirectDialer struct {
	dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (m *mockDirectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return m.dialFunc(ctx, network, addr)
}

func (m *mockDirectDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}
