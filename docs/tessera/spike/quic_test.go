//go:build ignore

// NOT part of the test build. Imports github.com/apernet/quic-go, which the
// repo dropped in fc6b107 (wire migrated to the in-tree kuic fork), so this
// file no longer compiles here. Its finding is still recorded in spec.md §6
// and below: quic.ClientToken exposes no fields or constructor, so a Tessera
// QUIC client needs a patched quic-go. Kept as the evidence behind that
// claim. The other spike tests in this package do not import it and still run.

// Verification for spec.md §6 — the QUIC gap.
//
// §6 asserts two things. This file tests both, and one of them turns out to be
// wrong:
//
//  1. "§4.4 的 session_id 机制本身仍然工作" — FALSE. RFC 9001 §8.4 forbids TLS
//     middlebox compatibility mode on QUIC, so every real QUIC client sends an
//     EMPTY legacy_session_id. Writing 32 bytes there is not invisible; it is a
//     field no other QUIC client on the internet fills in.
//
//  2. "借壳转发在 QUIC 上做不到 TCP 那样干净" — also FALSE, and in the other
//     direction. On TCP the relay has to splice a byte stream after peeking the
//     ClientHello. On QUIC it is verbatim datagram forwarding: the node never
//     terminates, never decrypts, never reassembles. It is *cleaner* than TCP.
//
// So the QUIC problem is not "no relay". It is "no carrier for the
// discriminator" — and this file identifies one (the Initial packet's Token
// field) and tests that a real QUIC server tolerates a token it cannot decode,
// which is what makes relaying a credential-bearing probe safe.
//
// Evidence, not implementation. Lives under docs/ on purpose.
package spike

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	utls "github.com/metacubex/utls"
)

// ---------------------------------------------------------------------------
// QUIC long header parsing — enough of RFC 9000 §17.2 to read the Token field
// ---------------------------------------------------------------------------

func readVarint(b []byte) (val uint64, n int, ok bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	size := 1 << (b[0] >> 6)
	if len(b) < size {
		return 0, 0, false
	}
	val = uint64(b[0] & 0x3f)
	for i := 1; i < size; i++ {
		val = val<<8 | uint64(b[i])
	}
	return val, size, true
}

// initialToken returns the Token field of an Initial packet. ok is false for
// anything that is not a v1 Initial — short header packets, Handshake packets,
// version negotiation, garbage.
func initialToken(d []byte) (token []byte, ok bool) {
	if len(d) < 7 || d[0]&0x80 == 0 {
		return nil, false // not a long header
	}
	if d[0]&0x30 != 0x00 {
		return nil, false // long header type != Initial
	}
	if d[1] != 0x00 || d[2] != 0x00 || d[3] != 0x00 || d[4] != 0x01 {
		return nil, false // not QUIC v1
	}
	p := 5
	for i := 0; i < 2; i++ { // DCID then SCID
		if p >= len(d) {
			return nil, false
		}
		l := int(d[p])
		p += 1 + l
		if p > len(d) {
			return nil, false
		}
	}
	tl, n, ok := readVarint(d[p:])
	if !ok {
		return nil, false
	}
	p += n
	if uint64(len(d)-p) < tl {
		return nil, false
	}
	return d[p : p+int(tl)], true
}

// ---------------------------------------------------------------------------
// The node: one UDP port, two fates
// ---------------------------------------------------------------------------

// demuxConn is a net.PacketConn that quic-go listens on, but which only
// surfaces datagrams from flows it decided are authenticated. Everything else
// is forwarded verbatim to the front and never seen by our QUIC stack.
//
// This is the whole borrowed-shell mechanism for QUIC. Note what is absent:
// no TLS termination, no CRYPTO frame reassembly, no connection ID tracking.
// The decision is per 4-tuple, taken on the first datagram, and after that the
// node is a UDP NAT.
type demuxConn struct {
	sock  *net.UDPConn
	front *net.UDPAddr

	// classify runs once per client 4-tuple, on its first datagram. In
	// production this is spec §4.5; here it is "does the Initial carry a
	// token", which is structurally the real predicate minus the crypto
	// (tested separately in TestTokenCarriesTheCredential).
	classify func(first []byte) bool

	mu     sync.Mutex
	relays map[string]*net.UDPConn
	local  map[string]bool
	closed chan struct{}

	nRelayed atomic.Int64
	nLocal   atomic.Int64
}

func newDemuxConn(t *testing.T, front *net.UDPAddr, classify func([]byte) bool) *demuxConn {
	t.Helper()
	sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	d := &demuxConn{
		sock:     sock,
		front:    front,
		classify: classify,
		relays:   map[string]*net.UDPConn{},
		local:    map[string]bool{},
		closed:   make(chan struct{}),
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func (d *demuxConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, 4096)
	for {
		n, addr, err := d.sock.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		key := addr.String()

		d.mu.Lock()
		if d.local[key] {
			d.mu.Unlock()
			return copy(p, buf[:n]), addr, nil
		}
		relay, known := d.relays[key]
		if !known {
			if d.classify(buf[:n]) {
				d.local[key] = true
				d.nLocal.Add(1)
				d.mu.Unlock()
				return copy(p, buf[:n]), addr, nil
			}
			relay, err = net.DialUDP("udp", nil, d.front)
			if err != nil {
				d.mu.Unlock()
				return 0, nil, err
			}
			d.relays[key] = relay
			d.nRelayed.Add(1)
			go d.pumpBack(relay, addr)
		}
		d.mu.Unlock()

		if _, err := relay.Write(buf[:n]); err != nil {
			continue // front went away; the flow simply stalls, as it would for a real relay
		}
	}
}

// pumpBack carries the front's datagrams back to the client, spoofing nothing:
// they leave from the node's own socket, so to the client the front's QUIC
// endpoint simply *is* the node.
func (d *demuxConn) pumpBack(relay *net.UDPConn, client *net.UDPAddr) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-d.closed:
			return
		default:
		}
		n, err := relay.Read(buf)
		if err != nil {
			return
		}
		if _, err := d.sock.WriteToUDP(buf[:n], client); err != nil {
			return
		}
	}
}

func (d *demuxConn) WriteTo(p []byte, addr net.Addr) (int, error) { return d.sock.WriteTo(p, addr) }
func (d *demuxConn) LocalAddr() net.Addr                          { return d.sock.LocalAddr() }
func (d *demuxConn) SetDeadline(t time.Time) error                { return d.sock.SetDeadline(t) }
func (d *demuxConn) SetReadDeadline(t time.Time) error            { return d.sock.SetReadDeadline(t) }
func (d *demuxConn) SetWriteDeadline(t time.Time) error           { return d.sock.SetWriteDeadline(t) }

func (d *demuxConn) Close() error {
	d.mu.Lock()
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	for _, r := range d.relays {
		r.Close()
	}
	d.relays = map[string]*net.UDPConn{}
	d.mu.Unlock()
	return d.sock.Close()
}

// ---------------------------------------------------------------------------
// Test servers
// ---------------------------------------------------------------------------

// namedCert moved to helpers_test.go so the still-compiling spike tests
// (shape_test.go) can use it without pulling in this file's apernet import.

// serveQUIC runs a QUIC endpoint that answers every stream with its own name,
// so a client can tell which endpoint actually served it.
func serveQUIC(t *testing.T, pc net.PacketConn, name string) *quic.Listener {
	t.Helper()
	ln, err := quic.Listen(pc, &tls.Config{
		Certificates: []tls.Certificate{namedCert(t, name)},
		NextProtos:   []string{"k2t-spike"},
		MinVersion:   tls.VersionTLS13,
	}, &quic.Config{MaxIdleTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go func(c *quic.Conn) {
				for {
					st, err := c.AcceptStream(context.Background())
					if err != nil {
						return
					}
					st.Write([]byte(name))
					st.Close()
				}
			}(conn)
		}
	}()
	return ln
}

func listenUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

type dialResult struct {
	certCN string
	served string
}

func dialQUIC(t *testing.T, addr string, sni string, store quic.TokenStore) (dialResult, error) {
	t.Helper()
	var res dialResult
	tlsConf := &tls.Config{
		ServerName:         sni,
		NextProtos:         []string{"k2t-spike"},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			c, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			res.certCN = c.Subject.CommonName
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsConf, &quic.Config{
		TokenStore:     store,
		MaxIdleTimeout: 10 * time.Second,
	})
	if err != nil {
		return res, err
	}
	defer conn.CloseWithError(0, "")

	st, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return res, err
	}
	if _, err := st.Write([]byte("hi")); err != nil {
		return res, err
	}
	st.Close()
	body, err := io.ReadAll(st)
	if err != nil {
		return res, err
	}
	res.served = string(body)

	// Give the server's NEW_TOKEN a moment to land in the store before the
	// caller reuses it.
	time.Sleep(150 * time.Millisecond)
	return res, nil
}

// ---------------------------------------------------------------------------
// §6 claim 1: the TCP discriminator does not transfer
// ---------------------------------------------------------------------------

// TestQUICClientHelloHasNoSessionID is the finding that rules out reusing §4.4
// verbatim. RFC 9001 §8.4 bans TLS middlebox compatibility mode, so a QUIC
// ClientHello carries a zero-length legacy_session_id. Go's own stdlib encodes
// the rule at crypto/tls/handshake_client.go ("The session ID is not set for
// QUIC connections").
//
// Consequence: a censor who decrypts an Initial (which it can — the keys come
// from the Destination Connection ID, which is in the clear) sees 32 bytes in
// a field every other client leaves empty. That is not a hidden discriminator,
// it is a beacon.
func TestQUICClientHelloHasNoSessionID(t *testing.T) {
	qc := tls.QUICClient(&tls.QUICConfig{TLSConfig: &tls.Config{
		ServerName: frontSNI,
		NextProtos: []string{"k2t-spike"},
		MinVersion: tls.VersionTLS13,
	}})
	defer qc.Close()
	qc.SetTransportParameters([]byte{0x00, 0x00})

	if err := qc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	var hello []byte
	for {
		ev := qc.NextEvent()
		if ev.Kind == tls.QUICNoEvent {
			break
		}
		if ev.Kind == tls.QUICWriteData && ev.Level == tls.QUICEncryptionLevelInitial {
			hello = append(hello, ev.Data...)
		}
	}
	if len(hello) == 0 {
		t.Fatal("no Initial CRYPTO data produced")
	}

	parsed, err := parseClientHello(hello)
	if err != nil {
		t.Fatalf("parse QUIC ClientHello: %v", err)
	}
	t.Logf("QUIC ClientHello: %d 字节, legacy_session_id = %d 字节", len(hello), len(parsed.sessionID))

	if len(parsed.sessionID) != 0 {
		t.Fatalf("QUIC ClientHello 的 legacy_session_id 应为空 (RFC 9001 §8.4)，实测 %d 字节",
			len(parsed.sessionID))
	}

	// The TCP path for contrast — proven in feasibility_test.go, restated here
	// so the asymmetry is visible in one place.
	t.Log("TCP 路径同一字段为 32 字节（feasibility_test.go 已证），因此 §4.4 的载体在 QUIC 上不存在")
}

// TestUTLSCoversTheQUICClientHello checks the §5 L1 row, which flags QUIC as
// having no uTLS. The censor can decrypt a QUIC Initial, so the ClientHello
// inside it is as exposed as the TCP one — and today's k2 QUIC path builds it
// with Go's stdlib, which cannot look like a browser.
//
// The capability is already in the utls version this repo depends on
// (UQUICClient). What is NOT free is wiring it into quic-go: quic-go drives
// crypto/tls.QUICConn directly, so using utls needs a fork patch — the same
// bill as the Token field in §6.4.
func TestUTLSCoversTheQUICClientHello(t *testing.T) {
	collect := func(next func() utls.QUICEvent) []byte {
		var out []byte
		for {
			ev := next()
			if ev.Kind == utls.QUICNoEvent {
				return out
			}
			if ev.Kind == utls.QUICWriteData && ev.Level == utls.QUICEncryptionLevelInitial {
				out = append(out, ev.Data...)
			}
		}
	}

	qc := utls.UQUICClient(&utls.QUICConfig{TLSConfig: &utls.Config{
		ServerName: frontSNI,
		NextProtos: []string{"h3"},
		MinVersion: utls.VersionTLS13,
	}}, utls.HelloChrome_120)
	defer qc.Close()
	qc.SetTransportParameters([]byte{0x00, 0x00})
	if err := qc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	parsed, err := parseClientHello(collect(qc.NextEvent))
	if err != nil {
		t.Fatalf("parse uTLS QUIC ClientHello: %v", err)
	}
	if len(parsed.sessionID) != 0 {
		t.Fatalf("uTLS 的 QUIC ClientHello 也必须留空 legacy_session_id，实测 %d 字节",
			len(parsed.sessionID))
	}

	var grease bool
	for g := range parsed.allShares {
		if g&0x0f0f == 0x0a0a {
			grease = true
		}
	}
	t.Logf("uTLS(Chrome_120) QUIC ClientHello: key_share 组 %d 个, 含 GREASE=%v, session_id=%d 字节",
		len(parsed.allShares), grease, len(parsed.sessionID))
	if !grease {
		t.Fatal("Chrome 预设应包含 GREASE key_share —— 说明 QUIC 路径没走上 uTLS 的浏览器 spec")
	}
}

// ---------------------------------------------------------------------------
// §6 claim 2: the relay
// ---------------------------------------------------------------------------

// TestQUICRelayGivesTheProberTheFront is the core disproof of §6's second
// claim. An unauthenticated QUIC client aimed at the node completes a full
// handshake with the FRONT's certificate and gets the front's service, without
// the node terminating anything.
func TestQUICRelayGivesTheProberTheFront(t *testing.T) {
	frontPC := listenUDP(t)
	serveQUIC(t, frontPC, "front.invalid")

	// Classifier: no token in the Initial => not one of ours => relay.
	demux := newDemuxConn(t, frontPC.LocalAddr().(*net.UDPAddr), func(first []byte) bool {
		tok, ok := initialToken(first)
		return ok && len(tok) > 0
	})
	serveQUIC(t, demux, "node.invalid")

	res, err := dialQUIC(t, demux.LocalAddr().String(), "front.invalid", nil)
	if err != nil {
		t.Fatalf("探测连接失败 —— 说明借壳转发没成立: %v", err)
	}
	t.Logf("探测者拿到: 证书 CN=%q, 服务方=%q", res.certCN, res.served)

	if res.certCN != "front.invalid" {
		t.Fatalf("探测者应拿到 front 的证书，实得 %q", res.certCN)
	}
	if res.served != "front.invalid" {
		t.Fatalf("探测者应由 front 提供服务，实得 %q", res.served)
	}
	if demux.nRelayed.Load() != 1 || demux.nLocal.Load() != 0 {
		t.Fatalf("流向不对: relayed=%d local=%d", demux.nRelayed.Load(), demux.nLocal.Load())
	}
}

// TestQUICRelaySurvivesRetry is the case a naive relay breaks on. When the
// front demands address validation it answers with a Retry carrying a new
// Source Connection ID, and the client restarts with a different Destination
// Connection ID. A relay that keyed its state on connection IDs would lose the
// flow here. Keying on the 4-tuple does not.
func TestQUICRelaySurvivesRetry(t *testing.T) {
	frontPC := listenUDP(t)
	tr := &quic.Transport{
		Conn:                frontPC,
		VerifySourceAddress: func(net.Addr) bool { return true }, // always Retry
	}
	t.Cleanup(func() { tr.Close() })

	ln, err := tr.Listen(&tls.Config{
		Certificates: []tls.Certificate{namedCert(t, "front.invalid")},
		NextProtos:   []string{"k2t-spike"},
		MinVersion:   tls.VersionTLS13,
	}, &quic.Config{MaxIdleTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go func(c *quic.Conn) {
				for {
					st, err := c.AcceptStream(context.Background())
					if err != nil {
						return
					}
					st.Write([]byte("front.invalid"))
					st.Close()
				}
			}(conn)
		}
	}()

	demux := newDemuxConn(t, frontPC.LocalAddr().(*net.UDPAddr), func([]byte) bool { return false })
	serveQUIC(t, demux, "node.invalid")

	res, err := dialQUIC(t, demux.LocalAddr().String(), "front.invalid", nil)
	if err != nil {
		t.Fatalf("Retry 路径下借壳失败: %v", err)
	}
	if res.served != "front.invalid" {
		t.Fatalf("Retry 后应仍由 front 服务，实得 %q", res.served)
	}
	t.Log("front 强制 Retry（DCID 中途改变）后借壳仍然完整")
}

// TestTokenSelectsTheLocalPath is the other half: a client that DOES carry a
// token in its Initial is kept by the node and served locally, on the same UDP
// port that just relayed a probe.
//
// The token here is a real quic-go NEW_TOKEN, obtained from a different server
// and reused because quic-go keys its token store on tls.Config.ServerName.
// That is the closest a stock client gets to the Tessera behaviour — see
// TestTokenIsNotSettableByStockQuicGo for what it costs to do it deliberately.
func TestTokenSelectsTheLocalPath(t *testing.T) {
	// A first server, purely to mint a token the client will keep.
	issuerPC := listenUDP(t)
	serveQUIC(t, issuerPC, "front.invalid")

	store := quic.NewLRUTokenStore(4, 4)
	if _, err := dialQUIC(t, issuerPC.LocalAddr().String(), "front.invalid", store); err != nil {
		t.Fatalf("token issuer dial: %v", err)
	}

	frontPC := listenUDP(t)
	serveQUIC(t, frontPC, "front.invalid")

	var sawToken atomic.Bool
	var tokenLen atomic.Int64
	demux := newDemuxConn(t, frontPC.LocalAddr().(*net.UDPAddr), func(first []byte) bool {
		tok, ok := initialToken(first)
		if ok && len(tok) > 0 {
			sawToken.Store(true)
			tokenLen.Store(int64(len(tok)))
			return true
		}
		return false
	})
	serveQUIC(t, demux, "node.invalid")

	res, err := dialQUIC(t, demux.LocalAddr().String(), "front.invalid", store)
	if err != nil {
		t.Fatalf("带 token 的连接失败: %v", err)
	}
	if !sawToken.Load() {
		t.Skip("客户端没有复用 token（NEW_TOKEN 未落库）——本测试的前提不成立")
	}
	t.Logf("带 token 的客户端拿到: 证书 CN=%q, 服务方=%q", res.certCN, res.served)
	t.Logf("quic-go 自己签发的 NEW_TOKEN 在 Initial 里是 %d 字节；Tessera 凭据为 64 字节 —— "+
		"长度是否落在真实分布内尚未普查", tokenLen.Load())

	if res.served != "node.invalid" {
		t.Fatalf("带 token 的连接应由节点本地服务，实得 %q", res.served)
	}
	if demux.nLocal.Load() != 1 {
		t.Fatalf("应有 1 条本地流，实得 %d", demux.nLocal.Load())
	}
}

// TestFrontToleratesAnUndecodableToken is what makes relaying safe. On the
// unauthenticated path the node forwards the client's Initial verbatim —
// including whatever sits in the Token field. If a front rejected tokens it
// could not decode, a replayed or expired Tessera credential would produce a
// failure the node could not hide, and that failure would be the signal.
//
// quic-go's server explicitly does not: an undecodable token is treated "as if
// there was no token on this packet at all" (server.go, handleInitialImpl).
// This test confirms the behaviour end to end rather than by reading it.
func TestFrontToleratesAnUndecodableToken(t *testing.T) {
	issuerPC := listenUDP(t)
	serveQUIC(t, issuerPC, "front.invalid")

	store := quic.NewLRUTokenStore(4, 4)
	if _, err := dialQUIC(t, issuerPC.LocalAddr().String(), "front.invalid", store); err != nil {
		t.Fatalf("token issuer dial: %v", err)
	}

	// A second, independent server: same SNI, different token key. The client
	// will present the first server's token, which this one cannot decode.
	otherPC := listenUDP(t)
	serveQUIC(t, otherPC, "front.invalid")

	res, err := dialQUIC(t, otherPC.LocalAddr().String(), "front.invalid", store)
	if err != nil {
		t.Fatalf("front 应容忍无法解码的 token，实测握手失败: %v", err)
	}
	if res.served != "front.invalid" {
		t.Fatalf("unexpected server: %q", res.served)
	}
	t.Log("front 收到无法解码的 token 后按无 token 处理，握手正常完成")
}

// TestTokenIsNotSettableByStockQuicGo records an implementation cost that is
// easy to miss when reading the spec: quic-go exposes no way to put arbitrary
// bytes in the Initial Token. ClientToken's payload field is unexported and
// there is no constructor, so a Tessera QUIC client needs a patch to the fork
// we already carry (github.com/apernet/quic-go).
//
// The test is a compile-time-ish assertion via reflection so it fails loudly if
// a future version adds the constructor and the note goes stale.
func TestTokenIsNotSettableByStockQuicGo(t *testing.T) {
	rt := reflect.TypeOf(quic.ClientToken{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.IsExported() {
			t.Fatalf("quic.ClientToken 现在有导出字段 %q —— §6 关于需要 patch fork 的记录已过期", f.Name)
		}
	}
	t.Log("quic.ClientToken 无导出字段、无构造函数 → Tessera QUIC 客户端需要给 apernet/quic-go 打补丁")
}

// ---------------------------------------------------------------------------
// The credential itself, offline
// ---------------------------------------------------------------------------

// TestTokenCarriesTheCredential shows the §4.4 construction survives the move
// from legacy_session_id to the Token field. The Token is cleartext and
// variable-length, so unlike session_id it can carry the client's ephemeral
// public key alongside the AEAD output — which removes the need to decrypt the
// Initial and parse the ClientHello at all. The node's whole decision is a
// varint parse, one X25519, and one AEAD open.
func TestTokenCarriesTheCredential(t *testing.T) {
	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	buildToken := func(cp *ecdh.PrivateKey, sp *ecdh.PublicKey, front string) []byte {
		shared, err := cp.ECDH(sp)
		if err != nil {
			t.Fatal(err)
		}
		key, nonce := deriveKeyNonce(shared, cp.PublicKey().Bytes())
		sealed := aeadFor(key).Seal(nil, nonce, buildCredential(1, time.Now(), [8]byte{9}), []byte(front))
		return append(append([]byte{}, cp.PublicKey().Bytes()...), sealed...)
	}

	// Node side: the whole §4.5 decision, on cleartext bytes.
	open := func(token []byte, front string) error {
		if len(token) != 32+credLen+16 {
			return errors.New("length")
		}
		cpub, err := ecdh.X25519().NewPublicKey(token[:32])
		if err != nil {
			return err
		}
		shared, err := serverPriv.ECDH(cpub)
		if err != nil {
			return err
		}
		key, nonce := deriveKeyNonce(shared, token[:32])
		_, err = aeadFor(key).Open(nil, nonce, token[32:], []byte(front))
		return err
	}

	tok := buildToken(clientPriv, serverPriv.PublicKey(), frontSNI)
	t.Logf("token = %d 字节 (32 pubkey + %d 明文 + 16 tag)", len(tok), credLen)

	if err := open(tok, frontSNI); err != nil {
		t.Fatalf("合法凭据应通过: %v", err)
	}
	if err := open(tok, "other.front.invalid"); err == nil {
		t.Fatal("换 front 后应失败（aad 绑定）")
	}

	otherPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := open(buildToken(clientPriv, otherPriv.PublicKey(), frontSNI), frontSNI); err == nil {
		t.Fatal("对着别的节点公钥生成的凭据应失败")
	}

	// A stock client's token is just bytes; it must not accidentally open.
	if err := open(make([]byte, 32+credLen+16), frontSNI); err == nil {
		t.Fatal("全零 token 不应通过")
	}
}
