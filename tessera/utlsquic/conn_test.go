package utlsquic_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	quic "github.com/apernet/quic-go"
	utls "github.com/metacubex/utls"

	"github.com/kaitu-io/tessera/utlsquic"
)

const (
	testSNI  = "front.example"
	testALPN = "h3"
)

// isGREASE reports whether v is one of the reserved GREASE values (RFC 8701),
// which have the form 0x?A?A with both bytes equal.
func isGREASE(v uint16) bool { return v&0x0f0f == 0x0a0a && byte(v>>8) == byte(v) }

// newServer starts a QUIC listener that records the ClientHello of each
// incoming connection, and returns its address plus a channel of those hellos.
func newServer(t *testing.T) (string, <-chan *tls.ClientHelloInfo, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: testSNI},
		DNSNames:              []string{testSNI},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	hellos := make(chan *tls.ClientHelloInfo, 4)
	conf := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}},
		NextProtos:   []string{testALPN},
		MinVersion:   tls.VersionTLS13,
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			hellos <- chi
			return nil, nil
		},
	}

	ln, err := quic.ListenAddr("127.0.0.1:0", conf, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			defer c.CloseWithError(0, "")
		}
	}()
	return ln.Addr().String(), hellos, pool
}

// dial completes one handshake against addr. A nil factory selects the standard
// library, which is the control arm of the fingerprint comparison.
func dial(t *testing.T, addr string, pool *x509.CertPool, f quic.ClientTLSConnFactory) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr,
		&tls.Config{ServerName: testSNI, NextProtos: []string{testALPN}, RootCAs: pool, MinVersion: tls.VersionTLS13},
		&quic.Config{ClientTLSConnFactory: f},
	)
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	conn.CloseWithError(0, "")
}

func recvHello(t *testing.T, hellos <-chan *tls.ClientHelloInfo) *tls.ClientHelloInfo {
	t.Helper()
	select {
	case chi := <-hellos:
		return chi
	case <-time.After(10 * time.Second):
		t.Fatal("server never saw a ClientHello")
		return nil
	}
}

// TestUTLSFingerprintSurvivesTheWire is the end-to-end claim: with the seam
// patch in place, a real quic-go connection carries a uTLS-built, browser-shaped
// ClientHello all the way to the server.
//
// Asserting on the bytes the SERVER parsed (rather than on uTLS's output in
// isolation) is what makes this end-to-end: it proves the hello was not rebuilt
// or renegotiated by quic-go somewhere along the way. The stdlib arm is the
// control -- without it, "GREASE present" would not prove the factory did
// anything, since a passing test with no baseline cannot tell the two engines
// apart.
func TestUTLSFingerprintSurvivesTheWire(t *testing.T) {
	addr, hellos, pool := newServer(t)

	dial(t, addr, pool, nil)
	stdlib := recvHello(t, hellos)

	dial(t, addr, pool, utlsquic.Factory(utls.HelloChrome_120))
	chrome := recvHello(t, hellos)

	countGREASE := func(vs []uint16) int {
		n := 0
		for _, v := range vs {
			if isGREASE(v) {
				n++
			}
		}
		return n
	}
	greaseCurves := func(cs []tls.CurveID) int {
		vs := make([]uint16, len(cs))
		for i, c := range cs {
			vs[i] = uint16(c)
		}
		return countGREASE(vs)
	}

	if n := countGREASE(stdlib.CipherSuites); n != 0 {
		t.Fatalf("control arm polluted: stdlib sent %d GREASE cipher suites", n)
	}
	if n := countGREASE(chrome.CipherSuites); n == 0 {
		t.Fatalf("no GREASE cipher suite arrived; the factory did not take effect (suites=%v)",
			chrome.CipherSuites)
	}
	if n := greaseCurves(chrome.SupportedCurves); n == 0 {
		t.Fatalf("no GREASE group arrived (curves=%v)", chrome.SupportedCurves)
	}
	if len(chrome.CipherSuites) == len(stdlib.CipherSuites) {
		t.Logf("warning: cipher list lengths coincide (%d) -- weak differential",
			len(chrome.CipherSuites))
	}
	t.Logf("stdlib: %d suites, 0 GREASE | chrome: %d suites, %d GREASE suites, %d GREASE groups",
		len(stdlib.CipherSuites), len(chrome.CipherSuites),
		countGREASE(chrome.CipherSuites), greaseCurves(chrome.SupportedCurves))
}

// TestRootCAsAreCarriedAcross guards the field copy in translateConfig. utls has
// its own Config type, so every field crosses by hand; dropping RootCAs would
// not fail to compile, it would fail to verify -- and with InsecureSkipVerify
// absent, that shows up as a handshake error rather than as silent acceptance.
// A successful handshake against a private CA is the proof it was carried.
func TestRootCAsAreCarriedAcross(t *testing.T) {
	addr, hellos, pool := newServer(t)
	dial(t, addr, pool, utlsquic.Factory(utls.HelloChrome_120))
	recvHello(t, hellos)
}

// TestUnsupportedConfigIsRejected pins the choice to fail loudly. Each of these
// fields would otherwise be silently overridden or dropped, leaving a caller
// believing a setting is in force when it is not.
func TestUnsupportedConfigIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		conf *tls.Config
	}{
		{"CipherSuites", &tls.Config{CipherSuites: []uint16{tls.TLS_AES_128_GCM_SHA256}}},
		{"CurvePreferences", &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519}}},
		{"VerifyConnection", &tls.Config{VerifyConnection: func(tls.ConnectionState) error { return nil }}},
		{"ClientSessionCache", &tls.Config{ClientSessionCache: tls.NewLRUClientSessionCache(1)}},
		{"Certificates", &tls.Config{Certificates: []tls.Certificate{{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := utlsquic.New(&tls.QUICConfig{TLSConfig: tc.conf}, utls.HelloChrome_120)
			if err := c.Start(context.Background()); err == nil {
				t.Fatalf("%s was accepted; it should be rejected rather than silently ignored", tc.name)
			}
		})
	}
}

// TestNilConfigDoesNotPanic covers the deferred-error path: the factory has no
// way to return an error, so a bad config must surface at Start.
func TestNilConfigDoesNotPanic(t *testing.T) {
	if err := utlsquic.New(nil, utls.HelloChrome_120).Start(context.Background()); err == nil {
		t.Fatal("nil config accepted")
	}
}
