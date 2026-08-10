package tlspad

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

// helper: generate a self-signed ECDSA P-256 cert, return tls.Certificate and DER length
func generateMinimalECCert(t *testing.T) (tls.Certificate, int) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
	return tlsCert, len(der)
}

// helper: generate a self-signed RSA-2048 cert
func generateMinimalRSACert(t *testing.T) (tls.Certificate, int) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
	return tlsCert, len(der)
}

func TestPadCertificateTemplate(t *testing.T) {
	// Generate a minimal EC cert to measure base size
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	// First pass: measure base DER size
	baseDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create base cert: %v", err)
	}
	baseLen := len(baseDER)
	t.Logf("base EC cert DER length: %d", baseLen)

	// Target: 4000 bytes
	target := 4000

	// Apply padding
	tmpl2 := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	PadCertificateTemplate(tmpl2, baseLen, target)

	paddedDER, err := x509.CreateCertificate(rand.Reader, tmpl2, tmpl2, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create padded cert: %v", err)
	}
	paddedLen := len(paddedDER)
	t.Logf("padded EC cert DER length: %d (target: %d)", paddedLen, target)

	diff := paddedLen - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 30 {
		t.Errorf("padded cert DER length %d not within +-30 of target %d (diff=%d)", paddedLen, target, diff)
	}
}

func TestPadCertificateTemplate_RSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	baseDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create base cert: %v", err)
	}
	baseLen := len(baseDER)
	t.Logf("base RSA cert DER length: %d", baseLen)

	target := 4000

	tmpl2 := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	PadCertificateTemplate(tmpl2, baseLen, target)

	paddedDER, err := x509.CreateCertificate(rand.Reader, tmpl2, tmpl2, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create padded cert: %v", err)
	}
	paddedLen := len(paddedDER)
	t.Logf("padded RSA cert DER length: %d (target: %d)", paddedLen, target)

	diff := paddedLen - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 30 {
		t.Errorf("padded cert DER length %d not within +-30 of target %d (diff=%d)", paddedLen, target, diff)
	}
}

func TestPadCertificateNoOp(t *testing.T) {
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	// Target <= current: should be a no-op
	PadCertificateTemplate(tmpl, 500, 300)

	if len(tmpl.ExtraExtensions) != 0 {
		t.Errorf("expected no extra extensions when target <= current, got %d", len(tmpl.ExtraExtensions))
	}

	// Target == current: also no-op
	PadCertificateTemplate(tmpl, 500, 500)

	if len(tmpl.ExtraExtensions) != 0 {
		t.Errorf("expected no extra extensions when target == current, got %d", len(tmpl.ExtraExtensions))
	}
}

func TestGeneratePaddedCert_EC(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	target := 4000
	cert, err := GeneratePaddedCert(key, target)
	if err != nil {
		t.Fatalf("GeneratePaddedCert: %v", err)
	}

	derLen := MeasureCertDERLen(&cert)
	t.Logf("GeneratePaddedCert EC DER length: %d (target: %d)", derLen, target)

	diff := derLen - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 20 {
		t.Errorf("DER length %d not within +-20 of target %d (diff=%d)", derLen, target, diff)
	}
}

func TestGeneratePaddedCert_RSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	target := 4000
	cert, err := GeneratePaddedCert(key, target)
	if err != nil {
		t.Fatalf("GeneratePaddedCert: %v", err)
	}

	derLen := MeasureCertDERLen(&cert)
	t.Logf("GeneratePaddedCert RSA DER length: %d (target: %d)", derLen, target)

	diff := derLen - target
	if diff < 0 {
		diff = -diff
	}
	if diff > 20 {
		t.Errorf("DER length %d not within +-20 of target %d (diff=%d)", derLen, target, diff)
	}
}

func TestPaddedTLSHandshake(t *testing.T) {
	// Generate padded server cert
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	cert, err := GeneratePaddedCert(key, 4000)
	if err != nil {
		t.Fatalf("GeneratePaddedCert: %v", err)
	}

	// Server TLS config
	serverConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Client TLS config (skip verify for self-signed)
	clientConf := &tls.Config{
		InsecureSkipVerify: true,
	}

	// Create pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var serverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv := tls.Server(serverConn, serverConf)
		if err := srv.Handshake(); err != nil {
			serverErr = fmt.Errorf("server handshake: %w", err)
			return
		}
		// Write some data
		srv.Write([]byte("hello"))
		srv.Close()
	}()

	cli := tls.Client(clientConn, clientConf)
	if err := cli.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}

	buf := make([]byte, 10)
	n, _ := cli.Read(buf)
	if string(buf[:n]) != "hello" {
		t.Errorf("expected 'hello', got %q", string(buf[:n]))
	}
	cli.Close()
	wg.Wait()

	if serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
}

// recordingConn captures all written bytes
type recordingConn struct {
	net.Conn
	written []byte
	mu      sync.Mutex
}

func (c *recordingConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.written = append(c.written, b...)
	c.mu.Unlock()
	return c.Conn.Write(b)
}

func (c *recordingConn) bytesWritten() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(c.written))
	copy(cp, c.written)
	return cp
}

func TestPaddedRecordLength(t *testing.T) {
	// Compare TLS handshake record sizes: unpadded vs padded
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Unpadded cert
	unpaddedTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	unpaddedDER, err := x509.CreateCertificate(rand.Reader, unpaddedTmpl, unpaddedTmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create unpadded cert: %v", err)
	}
	unpaddedCert := tls.Certificate{
		Certificate: [][]byte{unpaddedDER},
		PrivateKey:  key,
	}

	// Padded cert
	paddedCert, err := GeneratePaddedCert(key, 4000)
	if err != nil {
		t.Fatalf("GeneratePaddedCert: %v", err)
	}

	unpaddedBytes := doHandshakeRecordCapture(t, unpaddedCert)
	paddedBytes := doHandshakeRecordCapture(t, paddedCert)

	t.Logf("unpadded server wrote %d bytes", len(unpaddedBytes))
	t.Logf("padded server wrote %d bytes", len(paddedBytes))

	// The padded server should have written significantly more bytes
	if len(paddedBytes) <= len(unpaddedBytes) {
		t.Errorf("padded server bytes (%d) should be larger than unpadded (%d)",
			len(paddedBytes), len(unpaddedBytes))
	}

	// Difference should be roughly the padding amount (~3700 bytes for EC cert ~300 -> 4000)
	diff := len(paddedBytes) - len(unpaddedBytes)
	t.Logf("byte difference: %d", diff)
	if diff < 2000 {
		t.Errorf("expected at least 2000 bytes difference, got %d", diff)
	}
}

func doHandshakeRecordCapture(t *testing.T, cert tls.Certificate) []byte {
	t.Helper()

	serverConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	clientConf := &tls.Config{
		InsecureSkipVerify: true,
	}

	clientRaw, serverRaw := net.Pipe()
	defer clientRaw.Close()

	serverRec := &recordingConn{Conn: serverRaw}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer serverRec.Close()
		srv := tls.Server(serverRec, serverConf)
		if err := srv.Handshake(); err != nil {
			return
		}
		srv.Close()
	}()

	cli := tls.Client(clientRaw, clientConf)
	if err := cli.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	cli.Close()
	wg.Wait()

	return serverRec.bytesWritten()
}

func TestStandardClientWithPaddedServer(t *testing.T) {
	// Verify a standard Go TLS client can connect to a padded-cert server over TCP
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	cert, err := GeneratePaddedCert(key, 4000)
	if err != nil {
		t.Fatalf("GeneratePaddedCert: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	var serverErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			serverErr = fmt.Errorf("accept: %w", err)
			return
		}
		defer conn.Close()
		conn.Write([]byte("padded-hello"))
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 20)
	n, _ := conn.Read(buf)
	if string(buf[:n]) != "padded-hello" {
		t.Errorf("expected 'padded-hello', got %q", string(buf[:n]))
	}

	wg.Wait()
	if serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
}
