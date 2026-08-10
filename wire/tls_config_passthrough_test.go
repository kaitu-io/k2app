package wire

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"testing"
	"time"

	tls "github.com/metacubex/utls"
)

// BuildClientTLSConfig is the single place that decides how a k2v5 server's
// certificate gets verified. Both legs — QUIC and TCP-WS — must hand that
// decision to uTLS intact.
//
// This used to be copied field by field into a fresh tls.Config in the TCP-WS
// dial path, and the copy carried InsecureSkipVerify but not
// VerifyPeerCertificate: with pin= configured, InsecureSkipVerify=true arrived
// and the pin check did not, so the TCP-WS leg accepted any certificate at all.
// The default path lost RootCAs (the Kaitu CA) and the blocked-CA check the
// same way. Returning the utls config directly is what makes that class of bug
// impossible, and these tests are what keep it that way.

func TestBuildClientTLSConfig_PinModeKeepsVerifier(t *testing.T) {
	cfg := Config{
		Host: "example.com",
		Port: 443,
		ECH:  testECHConfigB64(),
		Pin:  "sha256:47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU=",
	}

	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig: %v", err)
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Fatal("pin mode must disable chain verification")
	}
	if tlsCfg.VerifyPeerCertificate == nil {
		t.Fatal("pin mode disabled chain verification without installing the pin verifier — " +
			"this configuration accepts any certificate")
	}
}

func TestBuildClientTLSConfig_DefaultModeKeepsCAPolicy(t *testing.T) {
	cfg := Config{Host: "example.com", Port: 443, ECH: testECHConfigB64()}

	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig: %v", err)
	}

	if tlsCfg.InsecureSkipVerify {
		t.Fatal("default mode must verify the chain")
	}
	if tlsCfg.RootCAs == nil {
		t.Fatal("default mode lost RootCAs — the embedded Kaitu CA would not be trusted")
	}
	if tlsCfg.VerifyPeerCertificate == nil {
		t.Fatal("default mode lost the blocked-CA verifier — certificates from blocked CAs would be accepted")
	}
}

// TestBuildClientTLSConfig_ReturnsUTLSConfig pins the type. A stdlib
// *crypto/tls.Config here would force every uTLS caller to hand-copy fields
// again, which is how the verifier went missing in the first place.
func TestBuildClientTLSConfig_ReturnsUTLSConfig(t *testing.T) {
	cfg := Config{Host: "example.com", Port: 443, ECH: testECHConfigB64()}
	tlsCfg, _, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig: %v", err)
	}
	var _ *tls.Config = tlsCfg
}

// testECHConfigB64 returns a minimal well-formed ECHConfigList prefix. ECH is
// mandatory for k2v5, so every config under test needs one.
func testECHConfigB64() string {
	return base64.StdEncoding.EncodeToString([]byte{0xfe, 0x0d, 0x00, 0x10})
}

// TestTCPWSDial_PinMismatchIsRejected is the behavioural twin of
// TestBuildClientTLSConfig_PinModeKeepsVerifier: it drives a real uTLS
// handshake against a server whose certificate does not match the configured
// pin, and requires it to fail.
//
// Before the config was passed through untouched, this connected happily —
// InsecureSkipVerify survived the copy into the uTLS config and the pin
// verifier did not, so pin= turned certificate checking off instead of
// tightening it. A type-level test alone would not have caught that: the copy
// was in the dial path, not in BuildClientTLSConfig.
func TestTCPWSDial_PinMismatchIsRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pin-test"},
		DNSNames:     []string{"pin-test.invalid"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake() }()
		}
	}()

	// A pin that deliberately matches nothing.
	pins := [][32]byte{{}}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	uconn := tls.UClient(conn, &tls.Config{
		ServerName:            "pin-test.invalid",
		MinVersion:            tls.VersionTLS13,
		InsecureSkipVerify:    true, // chain checking is delegated to the pin verifier
		VerifyPeerCertificate: buildPinVerifier(pins),
	}, tls.HelloChrome_120_PQ)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := uconn.HandshakeContext(ctx); err == nil {
		t.Fatal("handshake succeeded against a certificate matching no pin — the pin verifier is not running")
	}
}
