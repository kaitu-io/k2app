package wire

import (
	"crypto/ecdsa"
	"crypto/rsa"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerateCertPair(t *testing.T) {
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair failed: %v", err)
	}

	// RSA cert leaf should have *rsa.PublicKey
	rsaLeaf, err := x509.ParseCertificate(pair.RSA.Certificate[0])
	if err != nil {
		t.Fatalf("parse RSA cert: %v", err)
	}
	if _, ok := rsaLeaf.PublicKey.(*rsa.PublicKey); !ok {
		t.Errorf("RSA cert public key type = %T, want *rsa.PublicKey", rsaLeaf.PublicKey)
	}

	// EC cert leaf should have *ecdsa.PublicKey
	ecLeaf, err := x509.ParseCertificate(pair.EC.Certificate[0])
	if err != nil {
		t.Fatalf("parse EC cert: %v", err)
	}
	if _, ok := ecLeaf.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Errorf("EC cert public key type = %T, want *ecdsa.PublicKey", ecLeaf.PublicKey)
	}

	// Pin hashes should be non-zero
	var zeroHash [32]byte
	if pair.RSAPinHash == zeroHash {
		t.Error("RSAPinHash is zero")
	}
	if pair.ECPinHash == zeroHash {
		t.Error("ECPinHash is zero")
	}
}

func TestCertPairPinHash(t *testing.T) {
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair failed: %v", err)
	}

	// Pin hashes should be non-zero and different from each other
	var zeroHash [32]byte
	if pair.RSAPinHash == zeroHash {
		t.Error("RSAPinHash is zero")
	}
	if pair.ECPinHash == zeroHash {
		t.Error("ECPinHash is zero")
	}
	if pair.RSAPinHash == pair.ECPinHash {
		t.Error("RSAPinHash and ECPinHash should differ")
	}
}

func TestCertPairSaveLoad(t *testing.T) {
	dir := t.TempDir()

	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair failed: %v", err)
	}

	if err := SaveCertPair(dir, pair); err != nil {
		t.Fatalf("SaveCertPair failed: %v", err)
	}

	loaded, err := LoadCertPair(dir)
	if err != nil {
		t.Fatalf("LoadCertPair failed: %v", err)
	}

	// Verify DER equality for RSA cert
	if len(pair.RSA.Certificate) != len(loaded.RSA.Certificate) {
		t.Fatalf("RSA cert chain length mismatch: %d vs %d", len(pair.RSA.Certificate), len(loaded.RSA.Certificate))
	}
	for i := range pair.RSA.Certificate {
		if string(pair.RSA.Certificate[i]) != string(loaded.RSA.Certificate[i]) {
			t.Errorf("RSA cert[%d] DER mismatch", i)
		}
	}

	// Verify DER equality for EC cert
	if len(pair.EC.Certificate) != len(loaded.EC.Certificate) {
		t.Fatalf("EC cert chain length mismatch: %d vs %d", len(pair.EC.Certificate), len(loaded.EC.Certificate))
	}
	for i := range pair.EC.Certificate {
		if string(pair.EC.Certificate[i]) != string(loaded.EC.Certificate[i]) {
			t.Errorf("EC cert[%d] DER mismatch", i)
		}
	}

	// Verify pin hashes match after load
	if pair.RSAPinHash != loaded.RSAPinHash {
		t.Error("RSAPinHash mismatch after save/load")
	}
	if pair.ECPinHash != loaded.ECPinHash {
		t.Error("ECPinHash mismatch after save/load")
	}

	// Verify key file permissions (Unix only — Windows doesn't support POSIX modes)
	if runtime.GOOS != "windows" {
		for _, name := range []string{"k2s-rsa.key", "k2s-ec.key"} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("stat %s: %v", name, err)
			}
			if perm := info.Mode().Perm(); perm != 0600 {
				t.Errorf("%s permissions = %o, want 0600", name, perm)
			}
		}
	}
}

func TestEnsureCertPair_Generate(t *testing.T) {
	dir := t.TempDir()

	pair, err := EnsureCertPair(dir)
	if err != nil {
		t.Fatalf("EnsureCertPair failed: %v", err)
	}

	// Should have generated files
	for _, name := range []string{"k2s-rsa.crt", "k2s-rsa.key", "k2s-ec.crt", "k2s-ec.key"} {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			t.Errorf("file %s was not created", name)
		}
	}

	// Should be valid certs
	var zeroHash [32]byte
	if pair.RSAPinHash == zeroHash {
		t.Error("RSAPinHash is zero")
	}
	if pair.ECPinHash == zeroHash {
		t.Error("ECPinHash is zero")
	}
}

func TestEnsureCertPair_Load(t *testing.T) {
	dir := t.TempDir()

	// First call generates
	pair1, err := EnsureCertPair(dir)
	if err != nil {
		t.Fatalf("EnsureCertPair (first) failed: %v", err)
	}

	// Second call loads without regenerating
	pair2, err := EnsureCertPair(dir)
	if err != nil {
		t.Fatalf("EnsureCertPair (second) failed: %v", err)
	}

	// Certs should be identical (loaded, not regenerated)
	if string(pair1.RSA.Certificate[0]) != string(pair2.RSA.Certificate[0]) {
		t.Error("RSA cert changed between Ensure calls")
	}
	if string(pair1.EC.Certificate[0]) != string(pair2.EC.Certificate[0]) {
		t.Error("EC cert changed between Ensure calls")
	}
}

func TestFormatPins(t *testing.T) {
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair failed: %v", err)
	}

	pins := pair.FormatPins()

	// Format: "sha256:<rsa_urlsafe_b64>,sha256:<ec_urlsafe_b64>"
	if !strings.HasPrefix(pins, "sha256:") {
		t.Errorf("pins should start with sha256:, got %q", pins)
	}
	parts := strings.SplitN(pins, ",", 2)
	if len(parts) != 2 {
		t.Fatalf("pins should have 2 parts separated by comma, got %q", pins)
	}
	if !strings.HasPrefix(parts[0], "sha256:") {
		t.Errorf("first part should start with sha256:, got %q", parts[0])
	}
	if !strings.HasPrefix(parts[1], "sha256:") {
		t.Errorf("second part should start with sha256:, got %q", parts[1])
	}

	// Hashes should be non-empty after prefix
	rsaHash := strings.TrimPrefix(parts[0], "sha256:")
	ecHash := strings.TrimPrefix(parts[1], "sha256:")
	if rsaHash == "" {
		t.Error("RSA pin hash is empty")
	}
	if ecHash == "" {
		t.Error("EC pin hash is empty")
	}
	if rsaHash == ecHash {
		t.Error("RSA and EC pin hashes should differ")
	}
}

func TestProbeTarget_Cloudflare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	profile, err := ProbeTarget("www.cloudflare.com")
	if err != nil {
		t.Fatalf("ProbeTarget failed: %v", err)
	}

	if profile.KeyAlgo != "ec" {
		t.Errorf("KeyAlgo = %q, want %q (Cloudflare uses EC)", profile.KeyAlgo, "ec")
	}
	if profile.CertChainLen <= 0 {
		t.Errorf("CertChainLen = %d, want > 0", profile.CertChainLen)
	}
}

func TestCertPairTLSHandshake(t *testing.T) {
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair failed: %v", err)
	}

	// Test RSA cert handshake
	t.Run("RSA", func(t *testing.T) {
		testTLSHandshake(t, pair.RSA)
	})

	// Test EC cert handshake
	t.Run("EC", func(t *testing.T) {
		testTLSHandshake(t, pair.EC)
	})
}

func testTLSHandshake(t *testing.T, cert tls.Certificate) {
	t.Helper()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	// Parse the cert to extract its public key type for verification
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	var expectKeyType string
	switch leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		expectKeyType = "rsa"
	case *ecdsa.PublicKey:
		expectKeyType = "ec"
	}

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- fmt.Errorf("accept: %w", err)
			return
		}
		defer conn.Close()
		// Force handshake
		if tlsConn, ok := conn.(*tls.Conn); ok {
			if err := tlsConn.Handshake(); err != nil {
				errCh <- fmt.Errorf("server handshake: %w", err)
				return
			}
		}
		errCh <- nil
	}()

	// Client connects
	clientConn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer clientConn.Close()

	// Verify handshake succeeded on server side
	if err := <-errCh; err != nil {
		t.Fatalf("server error: %v", err)
	}

	// Verify the peer cert key type matches
	state := clientConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("no peer certificates")
	}
	switch state.PeerCertificates[0].PublicKey.(type) {
	case *rsa.PublicKey:
		if expectKeyType != "rsa" {
			t.Errorf("expected %s key, got RSA", expectKeyType)
		}
	case *ecdsa.PublicKey:
		if expectKeyType != "ec" {
			t.Errorf("expected %s key, got EC", expectKeyType)
		}
	default:
		t.Errorf("unexpected key type: %T", state.PeerCertificates[0].PublicKey)
	}

}
