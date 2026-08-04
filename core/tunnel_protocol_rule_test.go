package core

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/hkdf"

	"github.com/kaitu-io/k2-rules/krs"
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

// TestUDPSniff_ProtocolPlusHostRule_UsesSniffedSNI covers the one branch of
// RouteConnProto that a protocol-only rule never exercises: a route
// combining {protocol, domain_suffix}. The connection's destination is
// necessarily an IP literal (QUIC dials by IP, never by hostname), so this
// combined rule can only ever match through the QUIC-sniffed SNI domain
// (meta.Domain) — never through the raw dest IP. Before the fix, step 6 in
// core/tunnel.go called RouteConnProto(host, ...) with the IP, which can
// never satisfy a domain_suffix matcher: the rule silently never fired, with
// no error anywhere. After the fix, step 6 prefers meta.Domain when set.
func TestUDPSniff_ProtocolPlusHostRule_UsesSniffedSNI(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)

	ns, err := krs.BuildSet(krs.NamedSet{DomainSuffixes: []string{"example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	ruleEngine := rule.NewEngine([]rule.RouteEntry{
		{Target: directTarget, Sets: []krs.Matcher{ns}, Protocols: []string{"quic"}},
	}, proxyTarget, nil, false)

	// The destination the tunnel dials — an IP literal, standing in for the
	// QUIC server's address. It intentionally does NOT match the
	// domain_suffix criterion; only the sniffed SNI can satisfy the rule.
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()
	dest := directListener.LocalAddr().String()

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
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  directTarget,
		DefaultTarget: proxyTarget,
		Router:        NewRouter(ruleEngine),
		// SniffSNI, not SniffBT: QUIC domain extraction (tunnel.go's
		// needDomain) only runs unconditionally when sniffSNI is on — our
		// dest is IPv4, so the IPv6-rewrite fallback path doesn't apply.
		SniffSNI: true,
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	quicPkt := buildQUICInitialWithSNI(t, "example.org")

	localPC := newMockPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, dest)
		close(done)
	}()

	localPC.injectRead(quicPkt, nil)

	select {
	case got := <-serverGot:
		if !bytes.Equal(got, quicPkt) {
			t.Errorf("direct listener got %x, want %x", got, quicPkt)
		}
	case <-remotePC.writeCh:
		t.Fatal("protocol+host rule did not match — connection stayed on the proxy default; " +
			"RouteConnProto must be using the sniffed SNI domain, not the dest IP")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: neither direct listener nor proxy conn received the QUIC packet")
	}

	localPC.closeConn()
	<-done
}

// buildQUICInitialWithSNI constructs a real, decryptable QUIC v1 Initial
// packet carrying a TLS ClientHello with the given SNI — a test-fixture-only
// duplicate of sniff/quic_test.go's buildTestQUICInitial (which is
// unexported and package-private to `sniff`). Duplicated here so
// sniff.IsQUIC / sniff.SniffQUIC run FOR REAL, unmodified, inside a core
// package test — the point is to exercise the actual shipped sniffing code,
// not a stand-in for it. The salt/label constants are RFC 9001 QUIC v1
// public constants, not sniff-package internals.
func buildQUICInitialWithSNI(t *testing.T, sni string) []byte {
	t.Helper()

	clientHello := captureTLSClientHelloBytes(t, sni)

	var cryptoFrame bytes.Buffer
	cryptoFrame.WriteByte(0x06) // CRYPTO frame type
	writeQUICVarintTest(&cryptoFrame, 0)
	writeQUICVarintTest(&cryptoFrame, uint64(len(clientHello)))
	cryptoFrame.Write(clientHello)

	payload := cryptoFrame.Bytes()
	const minPayload = 1200 // QUIC Initial padding requirement
	if len(payload) < minPayload {
		payload = append(payload, make([]byte, minPayload-len(payload))...)
	}

	destConnID := make([]byte, 8)
	if _, err := rand.Read(destConnID); err != nil {
		t.Fatal(err)
	}

	var header bytes.Buffer
	header.WriteByte(0xc0) // long header + fixed bit + Initial type + 1-byte PN
	binary.Write(&header, binary.BigEndian, uint32(0x1))
	header.WriteByte(byte(len(destConnID)))
	header.Write(destConnID)
	header.WriteByte(0) // src conn ID length
	writeQUICVarintTest(&header, 0) // token length
	pktLen := 1 + len(payload) + 16 // PN(1) + payload + AEAD tag(16)
	writeQUICVarintTest(&header, uint64(pktLen))
	header.WriteByte(0) // packet number = 0

	hdrLen := header.Len()

	// RFC 9001 §5.2 initial_salt for QUIC v1.
	saltV1 := []byte{0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
		0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a}
	initialSecret := hkdf.Extract(crypto.SHA256.New, destConnID, saltV1)
	secret, err := quicHKDFExpandLabelTest(initialSecret, "client in", crypto.SHA256.Size())
	if err != nil {
		t.Fatalf("expand client in: %v", err)
	}
	key, err := quicHKDFExpandLabelTest(secret, "quic key", 16)
	if err != nil {
		t.Fatalf("expand key: %v", err)
	}
	iv, err := quicHKDFExpandLabelTest(secret, "quic iv", 12)
	if err != nil {
		t.Fatalf("expand iv: %v", err)
	}
	hpKey, err := quicHKDFExpandLabelTest(secret, "quic hp", 16)
	if err != nil {
		t.Fatalf("expand hp: %v", err)
	}

	ciph, err := quicAEADAESGCMTest(key, iv)
	if err != nil {
		t.Fatalf("AEAD: %v", err)
	}
	nonce := make([]byte, ciph.NonceSize()) // all-zero nonce for PN=0
	aad := header.Bytes()
	encrypted := ciph.Seal(nil, nonce, payload, aad)

	packet := make([]byte, 0, hdrLen+len(encrypted))
	packet = append(packet, aad...)
	packet = append(packet, encrypted...)

	sampleOffset := hdrLen + 3
	if sampleOffset+16 > len(packet) {
		t.Fatal("packet too short for header-protection sample")
	}
	sample := packet[sampleOffset : sampleOffset+16]
	block, err := aes.NewCipher(hpKey)
	if err != nil {
		t.Fatal(err)
	}
	mask := make([]byte, aes.BlockSize)
	block.Encrypt(mask, sample)

	packet[0] ^= mask[0] & 0x0f
	packet[hdrLen-1] ^= mask[1]

	return packet
}

// captureTLSClientHelloBytes captures a real crypto/tls ClientHello for the
// given SNI by running a throwaway local TLS handshake, then strips the
// 5-byte TLS record header. Duplicate of sniff/quic_test.go's
// captureTLSClientHello.
func captureTLSClientHelloBytes(t *testing.T, serverName string) []byte {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var hello []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		if n > 0 {
			hello = make([]byte, n)
			copy(hello, buf[:n])
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	})
	_ = tlsConn.Handshake() // Expected to fail — we only need the ClientHello.
	tlsConn.Close()
	wg.Wait()

	if len(hello) == 0 {
		t.Fatal("failed to capture TLS ClientHello")
	}
	if len(hello) < 5 || hello[0] != 0x16 {
		t.Fatalf("captured data is not a TLS Handshake record: %s", hex.EncodeToString(hello[:min(10, len(hello))]))
	}
	recordLen := int(binary.BigEndian.Uint16(hello[3:5]))
	if 5+recordLen > len(hello) {
		t.Fatal("TLS record length exceeds captured data")
	}
	return hello[5 : 5+recordLen]
}

// writeQUICVarintTest writes a QUIC variable-length integer. Duplicate of
// sniff/quic_test.go's writeQuicVarint.
func writeQUICVarintTest(w *bytes.Buffer, v uint64) {
	switch {
	case v <= 63:
		w.WriteByte(byte(v))
	case v <= 16383:
		w.WriteByte(byte(v>>8) | 0x40)
		w.WriteByte(byte(v))
	case v <= 1073741823:
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(v)|0x80000000)
		w.Write(b)
	default:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, v|0xC000000000000000)
		w.Write(b)
	}
}

// quicHKDFExpandLabelTest implements TLS 1.3 HKDF-Expand-Label (RFC 8446
// §7.1) for the QUIC v1 "tls13 " label prefix. Duplicate of
// sniff/qtls.go's quicHKDFExpandLabel (unexported, sniff-internal).
func quicHKDFExpandLabelTest(secret []byte, label string, length int) ([]byte, error) {
	b := make([]byte, 3, 3+6+len(label)+1)
	binary.BigEndian.PutUint16(b, uint16(length))
	b[2] = uint8(6 + len(label))
	b = append(b, []byte("tls13 ")...)
	b = append(b, []byte(label)...)
	b = b[:3+6+len(label)+1]
	b[3+6+len(label)] = 0 // empty context
	out := make([]byte, length)
	n, err := hkdf.Expand(crypto.SHA256.New, secret, b).Read(out)
	if err != nil {
		return nil, err
	}
	if n != length {
		return nil, err
	}
	return out, nil
}

// quicAEADAESGCMTest wraps AES-GCM with the QUIC xor-nonce construction
// (RFC 9001 §5.3). Duplicate of sniff/qtls.go's quicAEADAESGCM /
// xorNonceAEAD (unexported, sniff-internal).
func quicAEADAESGCMTest(key, nonceMask []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &xorNonceAEADTest{aead: aead, nonceMask: [12]byte(nonceMask)}, nil
}

type xorNonceAEADTest struct {
	nonceMask [12]byte
	aead      cipher.AEAD
}

func (f *xorNonceAEADTest) NonceSize() int { return 8 }
func (f *xorNonceAEADTest) Overhead() int  { return f.aead.Overhead() }

func (f *xorNonceAEADTest) Seal(out, nonce, plaintext, additionalData []byte) []byte {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result := f.aead.Seal(out, f.nonceMask[:], plaintext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result
}

func (f *xorNonceAEADTest) Open(out, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result, err := f.aead.Open(out, f.nonceMask[:], ciphertext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result, err
}

// --- TCP side -------------------------------------------------------------
//
// The protocol re-route originally landed on the UDP path only. TCP is
// BitTorrent's primary transport, so {protocol:["bittorrent"]} — the first
// protocol rule a user is likely to write — matched nothing there, silently.
// The two tests below pin the TCP counterpart of the UDP handler's step 6.

// btHandshakePkt returns a BitTorrent peer handshake (19-byte pstr + payload),
// the fixture sniff.IsBitTorrent recognizes on a TCP peek.
func btHandshakePkt(t *testing.T) []byte {
	t.Helper()
	pkt, err := hex.DecodeString(
		"13426974546f7272656e742070726f746f636f6c" +
			"0000000000100000e21ea9569b69bab33c97851d0298bdfa89bc90922d5554313631302dea812fcd6a3563e3be40c1d1")
	if err != nil {
		t.Fatal(err)
	}
	return pkt
}

// TestTCPSniff_NoProtocolRule_KeepsBTForcedDirect pins the "absent any
// protocol rule, behavior is byte-for-byte what it was" half of the contract.
// A Router IS installed here (unlike TestClientTunnel_HandleTCP_BTForcedDirect,
// which passes Router: nil) precisely so the new RouteConnProto call is
// actually reached and proven to be a no-op when no route mentions a protocol.
func TestTCPSniff_NoProtocolRule_KeepsBTForcedDirect(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)
	// No routes at all → everything falls through to the proxy fallback, so
	// the BT sniff gate (action != directTarget) is open.
	ruleEngine := rule.NewEngine(nil, proxyTarget, nil, false)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverGot := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		out := make([]byte, n)
		copy(out, buf[:n])
		serverGot <- out
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

	localConn, providerConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, ln.Addr().String())
		close(done)
	}()

	bt := btHandshakePkt(t)
	providerConn.Write(bt)

	select {
	case got := <-serverGot:
		if len(got) < 20 || !bytes.Equal(got[:20], bt[:20]) {
			t.Errorf("direct listener got %x, want a BT handshake prefix %x", got, bt[:20])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: BitTorrent did not reach the direct listener — the no-protocol-rule default (force direct) regressed")
	}

	providerConn.Close()
	<-done

	dialer.mu.Lock()
	tcpCalls := len(dialer.tcpCalls)
	dialer.mu.Unlock()
	if tcpCalls != 0 {
		t.Errorf("DialTCP called %d times, want 0 (no protocol rule → BitTorrent still forced direct)", tcpCalls)
	}
}

// TestTCPSniff_ProtocolRuleOverridesBTDefault is the override half: an
// explicit {protocol:["bittorrent"], via: <proxy>} rule must beat the
// hardcoded "BitTorrent forced direct" default and put the flow back on the
// tunnel. Inverting the default (rather than reinforcing it) is what makes
// this test able to fail — a no-op re-route leaves the connection direct.
//
// Mutation check: deleting the protocol re-route block from handleTCP must
// turn this red.
func TestTCPSniff_ProtocolRuleOverridesBTDefault(t *testing.T) {
	const (
		directTarget = rule.Target(0)
		proxyTarget  = rule.Target(2)
	)
	// The rule cannot match at routeConn time — meta.Protocol is empty there
	// and matchMeta is fail-closed on protocol — so the connection starts on
	// the fallback (proxy), gets forced direct by the BT default, and only
	// the post-sniff re-route can bring it back.
	ruleEngine := rule.NewEngine([]rule.RouteEntry{
		{Target: proxyTarget, Protocols: []string{"bittorrent"}},
	}, proxyTarget, nil, false)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	wentDirect := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		wentDirect <- struct{}{}
	}()

	dialer := newMockDialer()
	remoteConn, clientConn := net.Pipe()
	dialer.tcpConns <- remoteConn

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

	localConn, providerConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, ln.Addr().String())
		close(done)
	}()

	bt := btHandshakePkt(t)
	go providerConn.Write(bt)

	proxyGot := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := clientConn.Read(buf)
		if err != nil {
			return
		}
		out := make([]byte, n)
		copy(out, buf[:n])
		proxyGot <- out
	}()

	select {
	case got := <-proxyGot:
		if len(got) < 20 || !bytes.Equal(got[:20], bt[:20]) {
			t.Errorf("proxy conn got %x, want a BT handshake prefix %x", got, bt[:20])
		}
	case <-wentDirect:
		t.Fatal("BitTorrent egressed DIRECT — the {protocol:[\"bittorrent\"], via: proxy} rule did not override the post-sniff default")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for BitTorrent to reach the proxy conn")
	}

	providerConn.Close()
	clientConn.Close()
	<-done

	dialer.mu.Lock()
	tcpCalls := len(dialer.tcpCalls)
	dialer.mu.Unlock()
	if tcpCalls != 1 {
		t.Errorf("DialTCP called %d times, want 1 (protocol rule routes BitTorrent to the proxy)", tcpCalls)
	}
}
