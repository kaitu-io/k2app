package core

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"

	"github.com/kaitu-io/k2/rule"
)

// Regression guard for support ticket #3051: Telegram / KakaoTalk exchange
// text fine but voice and video calls never connect.
//
// The user runs the smart (bypass) preset with region cn — Center's [Auto]
// session records report "Rule: cn" for every one of their sessions from
// 2026-05-18 through 2026-06-23, on both Android and macOS. webapp emits that
// preset as:
//
//	[{match:{region:"cn"}, via:"direct"}, {match:{all:true}, via:"k2v5://…"}]
//
// which engine.go turns into a rule.Engine with disableSniff=false, and from
// that derives SniffBT = !ruleEngine.DisableSniff() = true (engine.go:436).
//
// These tests exercise that exact chain: config shape → SniffBT → routing of a
// foreign destination → what HandleUDP actually does with a VoIP first packet.
// They build the real rule engine from the user's config, which the
// Router:nil-based TestClientTunnel_HandleUDP_STUNKeepsProxyRoute cannot.

// newCNBypassEngine builds the rule engine for the smart/bypass + region:cn
// preset. The region:cn bundle is stood in for by its CIDR content — the
// bundle's only role here is deciding cn/non-cn, and the routing shape
// (one host-matching direct route, then match:all → proxy as fallback) is
// what buildRouteEntries produces for the user's config.
func newCNBypassEngine(t *testing.T, directTarget, proxyTarget rule.Target) *rule.Engine {
	t.Helper()
	cnSet, err := krs.BuildSet(krs.NamedSet{
		Name:  "cn",
		CIDRs: []string{"223.5.5.0/24", "114.114.114.0/24"},
	})
	if err != nil {
		t.Fatalf("build cn set: %v", err)
	}
	entries := []rule.RouteEntry{{Target: directTarget, Sets: []krs.Matcher{cnSet}}}
	// fallback = proxy (the match:all route); allProxy=false because a
	// preceding route means buildRouteEntries never takes the all-proxy branch.
	return rule.NewEngine(entries, proxyTarget, nil, false)
}

// stunBindingRequest returns a STUN Binding Request — the first packet both
// Telegram and KakaoTalk put on the wire when setting up a call (ICE gathering).
func stunBindingRequest() []byte {
	p := make([]byte, 20)
	p[0], p[1] = 0x00, 0x01 // Binding Request
	p[2], p[3] = 0x00, 0x00 // body length 0
	p[4], p[5], p[6], p[7] = 0x21, 0x12, 0xA4, 0x42 // magic cookie
	return p
}

// TestVoIP_Regression3051_SmartModeCN_STUNRelayedThroughProxy is the end-to-end
// regression guard for #3051, built from the user's real config rather than a
// Router:nil stub. It exercises the whole chain — cn-bypass config → SniffBT=true
// → a foreign VoIP destination routed to PROXY → HandleUDP with a STUN first
// packet — and asserts the packet is relayed through the tunnel, NOT sent out
// the local interface.
//
// Before the fix this reproduced the bug: the STUN packet egressed DIRECT (to a
// GFW-blocked foreign reflector), so calls never connected while text worked.
func TestVoIP_Regression3051_SmartModeCN_STUNRelayedThroughProxy(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)

	// A direct listener standing in for the foreign VoIP reflector. Anything
	// arriving here bypassed the tunnel — which is the #3051 failure.
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	wentDirect := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		if _, _, err := directListener.ReadFrom(buf); err == nil {
			wentDirect <- struct{}{}
		}
	}()

	dest := directListener.LocalAddr().String()
	host, _, _ := net.SplitHostPort(dest)

	ruleEngine := newCNBypassEngine(t, directTarget, proxyTarget)

	// Link 1 — the user's config shape produces SniffBT=true (engine.go:436).
	if ruleEngine.DisableSniff() {
		t.Fatal("bypass+region:cn must not disable sniffing")
	}
	sniffBT := !ruleEngine.DisableSniff()
	if !sniffBT {
		t.Fatal("SniffBT must be true for the smart preset")
	}

	// Link 2 — the rules route this foreign destination to PROXY.
	if got := ruleEngine.MatchConn(host, rule.ConnMeta{Network: "udp"}); got != proxyTarget {
		t.Fatalf("routing precondition: dest %s → target %d, want proxy (%d)", host, got, proxyTarget)
	}

	dialer := newMockDialer()
	// Pre-load the proxy-side conn so DialUDP returns and the relay runs.
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  directTarget,
		DefaultTarget: proxyTarget,
		Router:        NewRouter(ruleEngine),
		SniffBT:       sniffBT,
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	localPC := newMockPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, dest)
		close(done)
	}()

	stun := stunBindingRequest()
	localPC.injectRead(stun, nil)

	// Link 3 — the STUN packet must be relayed through the proxy conn.
	select {
	case got := <-remotePC.writeCh:
		if !bytes.Equal(got.data, stun) {
			t.Fatalf("proxy conn got %x, want %x", got.data, stun)
		}
	case <-wentDirect:
		t.Fatal("STUN egressed DIRECT under cn-bypass — #3051 regression")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: STUN never reached the proxy conn")
	}

	localPC.closeConn()
	remotePC.closeConn()
	<-done

	dialer.mu.Lock()
	udpCalls := len(dialer.udpCalls)
	dialer.mu.Unlock()
	if udpCalls != 1 {
		t.Fatalf("DialUDP called %d times, want 1 (STUN relayed through tunnel)", udpCalls)
	}
}

// TestVoIP_Repro3051_GlobalMode_STUNGoesThroughProxy is the counterfactual for
// the user's 2026-07-10 follow-up ("全局模式下也用不了"). Global mode makes the
// engine all-proxy, so engine.go derives SniffBT=false and the override cannot
// fire. If this passes, global mode does relay call setup through the tunnel and
// the user's global-mode failure has a different cause.
func TestVoIP_Repro3051_GlobalMode_STUNGoesThroughProxy(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)

	// A direct listener that must NEVER receive the packet in global mode.
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	wentDirect := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		if _, _, err := directListener.ReadFrom(buf); err == nil {
			wentDirect <- struct{}{}
		}
	}()

	// Global mode: single match:all → proxy route ⇒ disableSniff=true, no entries.
	ruleEngine := rule.NewEngine(nil, proxyTarget, nil, true)
	if !ruleEngine.DisableSniff() {
		t.Fatal("global mode must disable sniffing")
	}
	sniffBT := !ruleEngine.DisableSniff() // engine.go:436 ⇒ false

	dialer := newMockDialer()
	// Pre-load the proxy-side UDP conn so DialUDP returns and pipePacket can run;
	// what the tunnel writes into it is the "went through proxy" evidence.
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  directTarget,
		DefaultTarget: proxyTarget,
		Router:        NewRouter(ruleEngine),
		SniffBT:       sniffBT,
		// BlockQUIC left false — webapp never sets block_quic (CLIENT_CONFIG_DEFAULTS).
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	localPC := newMockPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, directListener.LocalAddr().String())
		close(done)
	}()

	stun := stunBindingRequest()
	localPC.injectRead(stun, nil)

	// The STUN packet must be relayed into the proxy conn, not the direct listener.
	select {
	case got := <-remotePC.writeCh:
		if !bytes.Equal(got.data, stun) {
			t.Fatalf("proxy conn got %x, want %x", got.data, stun)
		}
	case <-wentDirect:
		t.Fatal("global mode sent STUN direct — SniffBT should be off here")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: STUN never reached the proxy conn in global mode")
	}

	dialer.mu.Lock()
	udpCalls := len(dialer.udpCalls)
	dialer.mu.Unlock()
	if udpCalls != 1 {
		t.Fatalf("DialUDP called %d times, want 1 (STUN relayed through tunnel)", udpCalls)
	}

	localPC.closeConn()
	remotePC.closeConn()
	<-done
}
