package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
)

// Task 3 (protocol-dimension rules): the sniffer now consults the rule
// engine after the first UDP packet is sniffed, letting an explicit
// {protocol: [...]} rule override the hardcoded post-sniff defaults —
// BitTorrent forced direct, STUN/DTLS kept on the tunnel (ticket #3051).
//
// TestUDPSniff_NoProtocolRule_KeepsLegacyBehavior pins that, absent any
// protocol rule, behavior is byte-for-byte identical to the pre-Task-3
// #3051 fix: STUN stays on the proxy, BitTorrent is forced direct.
//
// TestUDPSniff_ProtocolRuleOverridesDefault pins that an explicit
// {protocol:["stun"], via: direct} rule wins over the STUN default,
// sending the traffic direct instead of through the proxy.

// stunBindingRequestPkt returns a minimal STUN Binding Request first packet
// (same shape as core/voip_stun_regression_test.go's stunBindingRequest).
func stunBindingRequestPkt() []byte {
	p := make([]byte, 20)
	p[0], p[1] = 0x00, 0x01 // Binding Request
	p[2], p[3] = 0x00, 0x00 // body length 0
	p[4], p[5], p[6], p[7] = 0x21, 0x12, 0xA4, 0x42 // magic cookie
	return p
}

// utpTrackerConnectPkt returns a minimal BitTorrent UDP Tracker connect
// request — the simplest fixture sniff.IsUDPTracker recognizes.
func utpTrackerConnectPkt() []byte {
	p := make([]byte, 16)
	binary.BigEndian.PutUint64(p[:8], 0x41727101980) // magic constant
	binary.BigEndian.PutUint32(p[8:12], 0)            // action = connect
	binary.BigEndian.PutUint32(p[12:16], 0x12345678)  // transaction id
	return p
}

func TestUDPSniff_NoProtocolRule_KeepsLegacyBehavior(t *testing.T) {
	// Router backed by an engine with NO protocol routes: everything falls
	// through to the proxy fallback, same shape as the pre-existing #3051
	// regression tests (newCNBypassEngine / TestClientTunnel_HandleUDP_STUNKeepsProxyRoute).
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)
	ruleEngine := rule.NewEngine(nil, proxyTarget, nil, false)

	t.Run("STUN_stays_proxy", func(t *testing.T) {
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

		dialer := newMockDialer()
		remotePC := newMockPacketConn()
		dialer.udpConns <- remotePC

		prov := &mockProvider{}
		tunnel := NewClientTunnel(ClientTunnelConfig{
			Provider:      prov,
			Outbounds:     testOutbounds(dialer),
			DirectTarget:  directTarget,
			DefaultTarget: proxyTarget,
			Router:        NewRouter(ruleEngine),
			SniffBT:       true,
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

		stun := stunBindingRequestPkt()
		localPC.injectRead(stun, nil)

		select {
		case got := <-remotePC.writeCh:
			if !bytes.Equal(got.data, stun) {
				t.Errorf("proxy conn got %x, want %x", got.data, stun)
			}
		case <-wentDirect:
			t.Fatal("STUN egressed DIRECT with no protocol rule — legacy behavior broken")
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for STUN to reach the proxy conn")
		}

		localPC.closeConn()
		remotePC.closeConn()
		<-done

		dialer.mu.Lock()
		udpCalls := len(dialer.udpCalls)
		dialer.mu.Unlock()
		if udpCalls != 1 {
			t.Errorf("DialUDP called %d times, want 1 (STUN relayed through proxy)", udpCalls)
		}
	})

	t.Run("BitTorrent_forced_direct", func(t *testing.T) {
		directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer directListener.Close()

		serverGot := make(chan []byte, 1)
		go func() {
			buf := make([]byte, 4096)
			n, _, err := directListener.ReadFrom(buf)
			if err != nil {
				return
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			serverGot <- pkt
		}()

		dialer := newMockDialer()
		prov := &mockProvider{}
		tunnel := NewClientTunnel(ClientTunnelConfig{
			Provider:      prov,
			Outbounds:     testOutbounds(dialer),
			DirectTarget:  directTarget,
			DefaultTarget: proxyTarget,
			Router:        NewRouter(ruleEngine),
			SniffBT:       true,
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

		bt := utpTrackerConnectPkt()
		localPC.injectRead(bt, nil)

		select {
		case got := <-serverGot:
			if !bytes.Equal(got, bt) {
				t.Errorf("direct listener got %x, want %x", got, bt)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for direct listener to receive BitTorrent packet")
		}

		localPC.closeConn()
		<-done

		dialer.mu.Lock()
		udpCalls := len(dialer.udpCalls)
		dialer.mu.Unlock()
		if udpCalls != 0 {
			t.Errorf("DialUDP called %d times, want 0 (BitTorrent should force direct)", udpCalls)
		}
	})
}

func TestUDPSniff_ProtocolRuleOverridesDefault(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)
	// An explicit protocol rule: STUN → direct. No host criteria, so it
	// matches any destination once the protocol is known post-sniff.
	ruleEngine := rule.NewEngine([]rule.RouteEntry{
		{Target: directTarget, Protocols: []string{"stun"}},
	}, proxyTarget, nil, false)

	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	serverGot := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _, err := directListener.ReadFrom(buf)
		if err != nil {
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		serverGot <- pkt
	}()

	dialer := newMockDialer()
	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  directTarget,
		DefaultTarget: proxyTarget,
		Router:        NewRouter(ruleEngine),
		SniffBT:       true,
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

	stun := stunBindingRequestPkt()
	localPC.injectRead(stun, nil)

	select {
	case got := <-serverGot:
		if !bytes.Equal(got, stun) {
			t.Errorf("direct listener got %x, want %x", got, stun)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: protocol rule did not override STUN's default proxy route")
	}

	localPC.closeConn()
	<-done

	dialer.mu.Lock()
	udpCalls := len(dialer.udpCalls)
	dialer.mu.Unlock()
	if udpCalls != 0 {
		t.Errorf("DialUDP called %d times, want 0 (protocol rule sends STUN direct)", udpCalls)
	}
}
