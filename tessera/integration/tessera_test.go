// Package integration proves the whole QUIC discriminator on a real wire: two
// clients dial the same UDP port, and which server answers them is decided by
// the credential in the Initial packet's Token field.
//
// Everything here is end-to-end on purpose. The unit tests can show that a
// credential opens and that a token parses; only a real dial can show that the
// node never emits a byte of its own to a stranger.
package integration

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	utls "github.com/metacubex/utls"

	"github.com/kaitu-io/tessera/client"
	"github.com/kaitu-io/tessera/credential"
	"github.com/kaitu-io/tessera/demux"
	"github.com/kaitu-io/tessera/utlsquic"
)

const (
	frontName = "front.invalid"
	nodeName  = "node.invalid"
	// alpn is what a browser offers over QUIC. It has to be the same on both
	// paths: a node answering with an ALPN its front does not speak would
	// contradict its own cover story inside the very packet a censor decrypts.
	alpn = "h3"
)

var testShortID = [8]byte{0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4}

func namedCert(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serveQUIC answers every stream with its own name, so a client can tell which
// endpoint actually served it. hellos, if non-nil, receives each ClientHello
// the endpoint parsed.
func serveQUIC(t *testing.T, pc net.PacketConn, name string, hellos chan<- *tls.ClientHelloInfo) {
	t.Helper()
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{namedCert(t, name)},
		NextProtos:   []string{alpn},
		MinVersion:   tls.VersionTLS13,
	}
	if hellos != nil {
		tlsConf.GetConfigForClient = func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			select {
			case hellos <- chi:
			default:
			}
			return nil, nil
		}
	}
	ln, err := quic.Listen(pc, tlsConf, &quic.Config{MaxIdleTimeout: 10 * time.Second})
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

// testbed is a front, and a node borrowing that front's shell.
type testbed struct {
	nodeAddr   string
	serverPriv *ecdh.PrivateKey
	stats      *demux.Stats
	nodeHellos chan *tls.ClientHelloInfo
}

func newTestbed(t *testing.T) *testbed {
	t.Helper()

	frontSock := listenUDP(t)
	serveQUIC(t, frontSock, frontName, nil)

	serverPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	opener, err := credential.NewOpener(credential.OpenerConfig{
		PrivateKey: serverPriv,
		Front:      frontName,
		ShortIDs:   [][8]byte{testShortID},
	})
	if err != nil {
		t.Fatal(err)
	}

	nodeSock := listenUDP(t)
	dc, err := demux.New(demux.Config{
		Conn:     nodeSock,
		Front:    frontSock.LocalAddr().(*net.UDPAddr),
		Classify: demux.TokenClassifier(opener),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dc.Close() })

	hellos := make(chan *tls.ClientHelloInfo, 8)
	serveQUIC(t, dc, nodeName, hellos)

	return &testbed{
		nodeAddr:   nodeSock.LocalAddr().String(),
		serverPriv: serverPriv,
		stats:      dc.Stats(),
		nodeHellos: hellos,
	}
}

type dialResult struct {
	certCN string
	served string
}

// dial performs one QUIC exchange and reports which endpoint answered. cfg nil
// means a stock quic-go dial — what any prober on the internet can do.
func dial(t *testing.T, addr string, cfg *quic.Config) (dialResult, error) {
	t.Helper()
	var res dialResult
	tlsConf := &tls.Config{
		ServerName:         frontName, // clients always name the front
		NextProtos:         []string{alpn},
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
	if cfg == nil {
		cfg = &quic.Config{}
	}
	cfg.MaxIdleTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsConf, cfg)
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
	return res, nil
}

func tesseraConfig(t *testing.T, tb *testbed) *quic.Config {
	t.Helper()
	cfg, err := client.Config(tb.serverPriv.PublicKey(), frontName, testShortID, utls.HelloChrome_120)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestTheCredentialDecidesWhoIsServed is the whole point of the discriminator.
// Two clients, one address, one difference: 64 bytes in the Initial's Token
// field. The prober arm is not optional — without it, "our client reached the
// node" would be equally true of a node that serves everybody.
func TestTheCredentialDecidesWhoIsServed(t *testing.T) {
	tb := newTestbed(t)

	ours, err := dial(t, tb.nodeAddr, tesseraConfig(t, tb))
	if err != nil {
		t.Fatalf("Tessera 客户端应连上节点: %v", err)
	}
	if ours.served != nodeName || ours.certCN != nodeName {
		t.Errorf("我们的客户端被 %q 服务、证书 CN=%q，期望都是 %q", ours.served, ours.certCN, nodeName)
	}

	prober, err := dial(t, tb.nodeAddr, nil)
	if err != nil {
		t.Fatalf("探测者应完成一次正常握手（对着 front）: %v", err)
	}
	if prober.served != frontName || prober.certCN != frontName {
		t.Errorf("探测者被 %q 服务、证书 CN=%q，期望都是 %q —— 节点泄露了自己", prober.served, prober.certCN, frontName)
	}

	if got := tb.stats.Authenticated.Load(); got != 1 {
		t.Errorf("认证路径流数 = %d，期望 1", got)
	}
	if got := tb.stats.Relayed.Load(); got != 1 {
		t.Errorf("借壳路径流数 = %d，期望 1", got)
	}
}

// TestTheProberNeverSeesNodeMaterial states the property in the form the threat
// model cares about: an active prober's entire exchange is with the front. The
// certificate is the front's real certificate, not a forgery the node minted,
// because the node terminated nothing.
func TestTheProberNeverSeesNodeMaterial(t *testing.T) {
	tb := newTestbed(t)

	for i := range 3 {
		res, err := dial(t, tb.nodeAddr, nil)
		if err != nil {
			t.Fatalf("第 %d 次探测握手失败: %v", i+1, err)
		}
		if res.certCN == nodeName {
			t.Fatalf("第 %d 次探测拿到了节点自己的证书", i+1)
		}
		if res.certCN != frontName {
			t.Fatalf("第 %d 次探测拿到 CN=%q，期望 front 的真实证书", i+1, res.certCN)
		}
	}
	if got := tb.stats.Authenticated.Load(); got != 0 {
		t.Errorf("探测者不应触发认证路径，得到 %d 条", got)
	}
	// The node's QUIC stack must not have parsed a single ClientHello: a
	// prober's packets never reach it.
	select {
	case chi := <-tb.nodeHellos:
		t.Fatalf("节点的 QUIC 栈解析了探测者的 ClientHello（SNI=%q）—— 数据报本不该到达它", chi.ServerName)
	default:
	}
}

// TestOurClientWearsABrowserFingerprint checks the other half of the dial. The
// token gets us in; uTLS decides whether the handshake that gets us in looks
// like anything a browser would send. A stdlib control arm is required, because
// "GREASE is present" proves nothing unless something without it also ran.
func TestOurClientWearsABrowserFingerprint(t *testing.T) {
	tb := newTestbed(t)

	if _, err := dial(t, tb.nodeAddr, tesseraConfig(t, tb)); err != nil {
		t.Fatalf("Tessera 拨号失败: %v", err)
	}
	tessera := recvHello(t, tb.nodeHellos)

	// Control arm: same node, same credential, stdlib TLS.
	control := &quic.Config{TokenStore: &client.TokenStore{
		ServerPub: tb.serverPriv.PublicKey(), Front: frontName, ShortID: testShortID,
	}}
	if _, err := dial(t, tb.nodeAddr, control); err != nil {
		t.Fatalf("对照组拨号失败: %v", err)
	}
	stdlib := recvHello(t, tb.nodeHellos)

	t.Logf("uTLS: %d 个 cipher suite，GREASE %d 个；stdlib: %d 个 cipher suite，GREASE %d 个",
		len(tessera.CipherSuites), countGREASE(tessera.CipherSuites),
		len(stdlib.CipherSuites), countGREASE(stdlib.CipherSuites))

	if countGREASE(tessera.CipherSuites) == 0 {
		t.Error("uTLS 臂没有 GREASE cipher suite —— 浏览器一定会发")
	}
	if countGREASE(stdlib.CipherSuites) != 0 {
		t.Error("对照组出现了 GREASE —— 对照失效，这个测试证明不了任何东西")
	}
	if len(tessera.CipherSuites) <= len(stdlib.CipherSuites) {
		t.Errorf("uTLS 臂 %d 个 cipher suite，未多于 stdlib 的 %d 个", len(tessera.CipherSuites), len(stdlib.CipherSuites))
	}
}

func recvHello(t *testing.T, ch <-chan *tls.ClientHelloInfo) *tls.ClientHelloInfo {
	t.Helper()
	select {
	case chi := <-ch:
		return chi
	case <-time.After(5 * time.Second):
		t.Fatal("节点没有解析到 ClientHello")
		return nil
	}
}

// countGREASE counts reserved values of the form 0x?A?A with both bytes equal
// (RFC 8701).
func countGREASE(vals []uint16) int {
	n := 0
	for _, v := range vals {
		if v&0x0f0f == 0x0a0a && byte(v>>8) == byte(v) {
			n++
		}
	}
	return n
}

// fixedTokenStore replays one credential forever, which is what an on-path
// attacker who captured a token can do.
type fixedTokenStore struct{ token []byte }

func (s *fixedTokenStore) Pop(string) *quic.ClientToken {
	return quic.NewClientToken(s.token, client.InitialRTT)
}
func (s *fixedTokenStore) Put(string, *quic.ClientToken) {}

// TestReplayedCredentialIsRelayed closes the loop on the replay cache over a
// real wire: the first use of a credential reaches the node, the second is
// handed to the front like any stranger.
//
// This is what stops an on-path attacker from confirming a node by capturing
// one client's credential and replaying it to see whether this address treats
// it specially.
func TestReplayedCredentialIsRelayed(t *testing.T) {
	tb := newTestbed(t)

	token, err := credential.SealToken(tb.serverPriv.PublicKey(), frontName, credential.Credential{
		Version:   credential.Version1,
		Timestamp: time.Now(),
		ShortID:   testShortID,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fixedTokenStore{token: token}

	first, err := dial(t, tb.nodeAddr, &quic.Config{
		ClientTLSConnFactory: utlsquic.Factory(utls.HelloChrome_120),
		TokenStore:           store,
	})
	if err != nil {
		t.Fatalf("首次使用应连上节点: %v", err)
	}
	if first.served != nodeName {
		t.Fatalf("首次被 %q 服务，期望 %q", first.served, nodeName)
	}

	second, err := dial(t, tb.nodeAddr, &quic.Config{
		ClientTLSConnFactory: utlsquic.Factory(utls.HelloChrome_120),
		TokenStore:           store,
	})
	if err != nil {
		t.Fatalf("重放应完成一次正常握手（对着 front）: %v", err)
	}
	if second.served != frontName {
		t.Errorf("重放被 %q 服务，期望被转给 front %q —— 重放缓存没有生效", second.served, frontName)
	}
}

// TestServerTokensAreDiscarded pins a trap in the carrier. The node's QUIC
// server issues NEW_TOKEN frames like any server; a client that stored one
// would offer it on the next dial in place of a credential, and be relayed to
// the front. The connection would fail for a reason nothing in the code says
// out loud, so the discard is asserted rather than assumed.
func TestServerTokensAreDiscarded(t *testing.T) {
	tb := newTestbed(t)
	store := &client.TokenStore{
		ServerPub: tb.serverPriv.PublicKey(), Front: frontName, ShortID: testShortID,
	}

	for i := range 3 {
		res, err := dial(t, tb.nodeAddr, &quic.Config{
			ClientTLSConnFactory: utlsquic.Factory(utls.HelloChrome_120),
			TokenStore:           store,
		})
		if err != nil {
			t.Fatalf("第 %d 次拨号失败: %v", i+1, err)
		}
		if res.served != nodeName {
			t.Fatalf("第 %d 次被 %q 服务，期望 %q —— 服务端 NEW_TOKEN 顶掉了凭据", i+1, res.served, nodeName)
		}
		// Give the server's NEW_TOKEN time to arrive before the next dial.
		time.Sleep(150 * time.Millisecond)
	}
}

// TestMigratedFlowIsRelayed pins a known limitation rather than a property.
//
// The decision is per 4-tuple and needs an Initial packet to make. A datagram
// arriving on an unknown 4-tuple without one — which is exactly what an
// authenticated connection looks like after a NAT rebinding — is classified as
// a stranger and relayed. Fixing it means teaching the node to recognise its
// own connection IDs in short header packets; when that lands, this test should
// be changed deliberately, not discovered by surprise.
func TestMigratedFlowIsRelayed(t *testing.T) {
	tb := newTestbed(t)

	if _, err := dial(t, tb.nodeAddr, tesseraConfig(t, tb)); err != nil {
		t.Fatalf("Tessera 拨号失败: %v", err)
	}
	before := tb.stats.Relayed.Load()

	// A short header packet from a fresh source port: the shape a migrated
	// connection's first datagram has.
	sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	nodeAddr, err := net.ResolveUDPAddr("udp", tb.nodeAddr)
	if err != nil {
		t.Fatal(err)
	}
	shortHeader := append([]byte{0x40}, make([]byte, 64)...) // fixed bit set, long-header bit clear
	if _, err := sock.WriteToUDP(shortHeader, nodeAddr); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for tb.stats.Relayed.Load() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tb.stats.Relayed.Load() == before {
		t.Fatal("短头包未被转给 front —— 迁移限制的形态变了，请复核 demux 的文档与本测试")
	}
}
