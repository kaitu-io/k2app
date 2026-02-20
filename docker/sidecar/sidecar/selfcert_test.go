package sidecar

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	// Test with default config
	cert, err := GenerateSelfSignedCert(nil)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	if cert.SSLCert == "" {
		t.Error("SSLCert is empty")
	}
	if cert.SSLKey == "" {
		t.Error("SSLKey is empty")
	}

	// Verify the certificate can be loaded
	_, err = tls.X509KeyPair([]byte(cert.SSLCert), []byte(cert.SSLKey))
	if err != nil {
		t.Fatalf("Failed to parse generated certificate: %v", err)
	}

	// Parse and verify certificate details
	block, _ := pem.Decode([]byte(cert.SSLCert))
	if block == nil {
		t.Fatal("Failed to decode PEM block")
	}

	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse X509 certificate: %v", err)
	}

	// Check defaults
	if x509Cert.Subject.CommonName != "kaitu-slave" {
		t.Errorf("Expected CN 'kaitu-slave', got '%s'", x509Cert.Subject.CommonName)
	}
	if len(x509Cert.Subject.Organization) != 1 || x509Cert.Subject.Organization[0] != "Kaitu Self-Hosted" {
		t.Errorf("Expected Organization 'Kaitu Self-Hosted', got %v", x509Cert.Subject.Organization)
	}

	// Check validity period (should be ~10 years)
	expectedDuration := 3650 * 24 * time.Hour
	actualDuration := x509Cert.NotAfter.Sub(x509Cert.NotBefore)
	if actualDuration < expectedDuration-24*time.Hour || actualDuration > expectedDuration+24*time.Hour {
		t.Errorf("Certificate validity period incorrect: expected ~%v, got %v", expectedDuration, actualDuration)
	}

	// Check default SANs (localhost)
	hasLocalhost := false
	for _, ip := range x509Cert.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasLocalhost = true
			break
		}
	}
	if !hasLocalhost {
		t.Error("Certificate should have 127.0.0.1 in IP SANs")
	}
}

func TestGenerateSelfSignedCertCustomConfig(t *testing.T) {
	config := &SelfSignedCertConfig{
		CommonName:   "test-server",
		Organization: "Test Org",
		ValidDays:    30,
		IPAddresses:  []net.IP{net.ParseIP("10.0.0.1")},
		DNSNames:     []string{"test.local"},
	}

	cert, err := GenerateSelfSignedCert(config)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Parse and verify
	block, _ := pem.Decode([]byte(cert.SSLCert))
	x509Cert, _ := x509.ParseCertificate(block.Bytes)

	if x509Cert.Subject.CommonName != "test-server" {
		t.Errorf("Expected CN 'test-server', got '%s'", x509Cert.Subject.CommonName)
	}

	// Check custom IP
	hasIP := false
	for _, ip := range x509Cert.IPAddresses {
		if ip.Equal(net.ParseIP("10.0.0.1")) {
			hasIP = true
			break
		}
	}
	if !hasIP {
		t.Error("Certificate should have 10.0.0.1 in IP SANs")
	}

	// Check custom DNS
	hasDNS := false
	for _, name := range x509Cert.DNSNames {
		if name == "test.local" {
			hasDNS = true
			break
		}
	}
	if !hasDNS {
		t.Error("Certificate should have test.local in DNS SANs")
	}
}

func TestGetOrCreateSelfSignedCert(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "selfcert-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// First call should generate new certificate
	cert1, err := GetOrCreateSelfSignedCert(tempDir, nil)
	if err != nil {
		t.Fatalf("Failed to get/create certificate: %v", err)
	}

	// Verify files were created
	certFile := filepath.Join(tempDir, "server-cert.pem")
	keyFile := filepath.Join(tempDir, "server-key.pem")

	if !fileExists(certFile) {
		t.Error("Certificate file was not created")
	}
	if !fileExists(keyFile) {
		t.Error("Key file was not created")
	}

	// Second call should load existing certificate
	cert2, err := GetOrCreateSelfSignedCert(tempDir, nil)
	if err != nil {
		t.Fatalf("Failed to load existing certificate: %v", err)
	}

	// Should be the same certificate
	if cert1.SSLCert != cert2.SSLCert {
		t.Error("Certificate should be the same on second call (loaded from disk)")
	}
	if cert1.SSLKey != cert2.SSLKey {
		t.Error("Private key should be the same on second call (loaded from disk)")
	}
}

func TestBuildTLSConfig(t *testing.T) {
	cert, err := GenerateSelfSignedCert(nil)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	tlsConfig, err := cert.BuildTLSConfig()
	if err != nil {
		t.Fatalf("Failed to build TLS config: %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("TLS config is nil")
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(tlsConfig.Certificates))
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected MinVersion TLS 1.2, got %d", tlsConfig.MinVersion)
	}
}

func TestIsCertificateValid(t *testing.T) {
	// Generate a valid certificate
	cert, err := GenerateSelfSignedCert(&SelfSignedCertConfig{
		ValidDays: 365, // 1 year
	})
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	if !isCertificateValid(cert) {
		t.Error("Newly generated certificate should be valid")
	}

	// Generate a certificate that expires soon (but still valid)
	shortCert, err := GenerateSelfSignedCert(&SelfSignedCertConfig{
		ValidDays: 10, // Only 10 days - less than 30 day threshold
	})
	if err != nil {
		t.Fatalf("Failed to generate short-lived certificate: %v", err)
	}

	// Should be invalid because it has less than 30 days remaining
	if isCertificateValid(shortCert) {
		t.Error("Certificate expiring in less than 30 days should be considered invalid")
	}
}

func TestGetDefaultCertDir(t *testing.T) {
	dir := GetDefaultCertDir()
	if dir == "" {
		t.Error("Default cert dir should not be empty")
	}
	// Should end with "certs"
	if filepath.Base(dir) != "certs" {
		t.Errorf("Default cert dir should end with 'certs', got '%s'", dir)
	}
}
