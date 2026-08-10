package core

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
)

// These tests drive the real HandleTCP/HandleUDP paths rather than calling
// recordDial directly. Calling the counter in isolation would still pass if a
// call site were dropped, which is the whole failure this instrumentation
// exists to catch — a counter nobody increments reads exactly like a tunnel
// where nothing is being attempted.

func TestDialOutcomes_ProxyFailureCountedThroughHandleTCP(t *testing.T) {
	dialer := newMockDialer()
	dialer.dialErr = errors.New("node cannot reach destination")
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	runTCP(t, prov, "example.com:80")

	got := tunnel.DialOutcomes()
	if got.ProxyFail != 1 || got.ProxyOK != 0 {
		t.Fatalf("failed proxy dial not counted: %+v", got)
	}
	if got.DirectOK != 0 || got.DirectFail != 0 {
		t.Fatalf("proxy failure leaked into the direct counters: %+v", got)
	}
}

func TestDialOutcomes_ProxySuccessCountedThroughHandleTCP(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	// A successful dial hands off to pipe(), so HandleTCP only returns once
	// both ends close — this case cannot use runTCP.
	clientConn, remoteConn := net.Pipe()
	dialer.tcpConns <- remoteConn

	localConn, providerConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "example.com:80")
		close(done)
	}()
	go providerConn.Write([]byte("hello"))

	buf := make([]byte, 16)
	if _, err := clientConn.Read(buf); err != nil {
		t.Fatalf("remote never received the forwarded bytes: %v", err)
	}
	providerConn.Close()
	clientConn.Close()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("HandleTCP did not return after both ends closed")
	}

	got := tunnel.DialOutcomes()
	if got.ProxyOK != 1 || got.ProxyFail != 0 {
		t.Fatalf("successful proxy dial not counted: %+v", got)
	}
}

// A direct dial failing is a different fault from a proxy dial failing — the
// #3303 bound-interface bug versus a node that cannot egress. Keeping them in
// one counter would let either hide behind the other.
func TestDialOutcomes_DirectFailureCountedSeparately(t *testing.T) {
	prov := &mockProvider{}
	ruleEngine := rule.NewEngine(nil, rule.Target(0), nil, false)

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    testOutbounds(newMockDialer()),
		DirectTarget: rule.Target(0),
		Router:       NewRouter(ruleEngine),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	// Port 1 on loopback refuses immediately — a deterministic direct failure.
	runTCP(t, prov, "127.0.0.1:1")

	got := tunnel.DialOutcomes()
	if got.DirectFail != 1 {
		t.Fatalf("failed direct dial not counted: %+v", got)
	}
	if got.ProxyOK != 0 || got.ProxyFail != 0 {
		t.Fatalf("direct failure leaked into the proxy counters: %+v", got)
	}
}

func TestDialOutcomes_UDPFailureCountedThroughHandleUDP(t *testing.T) {
	dialer := newMockDialer()
	dialer.dialErr = errors.New("node cannot reach destination")
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(newMockPacketConn(), "8.8.8.8:53")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleUDP did not return after a failed dial")
	}

	got := tunnel.DialOutcomes()
	if got.ProxyFail != 1 {
		t.Fatalf("failed UDP proxy dial not counted: %+v", got)
	}
}

func TestDialOutcomes_SubReportsTheWindowNotTheTotal(t *testing.T) {
	prev := DialOutcomes{ProxyOK: 40, ProxyFail: 2, DirectOK: 7, DirectFail: 1}
	cur := DialOutcomes{ProxyOK: 40, ProxyFail: 518, DirectOK: 9, DirectFail: 1}

	got := cur.Sub(prev)
	want := DialOutcomes{ProxyOK: 0, ProxyFail: 516, DirectOK: 2, DirectFail: 0}
	if got != want {
		t.Fatalf("Sub() = %+v, want %+v", got, want)
	}
}

// runTCP drives one connection through HandleTCP and waits for it to finish.
// The write satisfies the sniff peek immediately so the handler does not sit
// on its read deadline.
func runTCP(t *testing.T, prov *mockProvider, dest string) {
	t.Helper()

	localConn, providerConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, dest)
		close(done)
	}()
	go providerConn.Write([]byte("hello"))

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("HandleTCP did not return")
	}
	providerConn.Close()
}
