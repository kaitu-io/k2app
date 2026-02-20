package sidecar

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// SelfSignedCertConfig holds configuration for generating self-signed certificates
type SelfSignedCertConfig struct {
	// CommonName is the CN for the certificate (e.g., "kaitu-slave")
	CommonName string
	// Organization is the O for the certificate
	Organization string
	// ValidDays is the certificate validity period in days (default: 3650 = 10 years)
	ValidDays int
	// IPAddresses are IP SANs to include in the certificate
	IPAddresses []net.IP
	// DNSNames are DNS SANs to include in the certificate
	DNSNames []string
}

// GenerateSelfSignedCert generates a self-signed TLS certificate
// Returns the certificate and private key in PEM format
func GenerateSelfSignedCert(config *SelfSignedCertConfig) (*TunnelCertificate, error) {
	if config == nil {
		config = &SelfSignedCertConfig{}
	}

	// Set defaults
	if config.CommonName == "" {
		config.CommonName = "kaitu-slave"
	}
	if config.Organization == "" {
		config.Organization = "Kaitu Self-Hosted"
	}
	if config.ValidDays == 0 {
		config.ValidDays = 3650 // 10 years
	}

	// Generate ECDSA private key (P-256 curve for good security and performance)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.AddDate(0, 0, config.ValidDays)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   config.CommonName,
			Organization: []string{config.Organization},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           config.IPAddresses,
		DNSNames:              config.DNSNames,
	}

	// Add localhost and common addresses if no specific config
	if len(config.IPAddresses) == 0 && len(config.DNSNames) == 0 {
		template.IPAddresses = []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		}
		template.DNSNames = []string{"localhost"}
	}

	// Self-sign the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	log.Printf("[SelfCert] Generated self-signed certificate: CN=%s, Valid=%d days, Expires=%s",
		config.CommonName, config.ValidDays, notAfter.Format("2006-01-02"))

	return &TunnelCertificate{
		SSLCert: string(certPEM),
		SSLKey:  string(keyPEM),
	}, nil
}

// GetOrCreateSelfSignedCert loads existing certificate from certDir or generates a new one
// If certificate exists and is valid, it will be loaded
// If certificate doesn't exist or is expired, a new one will be generated and saved
// certDir: directory to store the certificate files
func GetOrCreateSelfSignedCert(certDir string, config *SelfSignedCertConfig) (*TunnelCertificate, error) {
	if certDir == "" {
		return nil, fmt.Errorf("certDir is required")
	}

	certFile := filepath.Join(certDir, "server-cert.pem")
	keyFile := filepath.Join(certDir, "server-key.pem")

	// Check if both files exist
	if fileExists(certFile) && fileExists(keyFile) {
		// Try to load existing certificate
		cert, err := loadCertificateFromFiles(certFile, keyFile)
		if err != nil {
			log.Printf("[SelfCert] Failed to load existing certificate: %v, will regenerate", err)
		} else if isCertificateValid(cert) {
			log.Printf("[SelfCert] Loaded existing self-signed certificate from %s", certDir)
			return cert, nil
		} else {
			log.Printf("[SelfCert] Existing certificate expired or invalid, will regenerate")
		}
	}

	// Generate new certificate
	log.Printf("[SelfCert] Generating new self-signed certificate...")
	cert, err := GenerateSelfSignedCert(config)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate: %w", err)
	}

	// Save to files
	if err := cert.SaveToFiles(certDir, "server-cert.pem", "server-key.pem"); err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}

	log.Printf("[SelfCert] Self-signed certificate saved to %s", certDir)
	return cert, nil
}

// loadCertificateFromFiles loads certificate from PEM files
func loadCertificateFromFiles(certFile, keyFile string) (*TunnelCertificate, error) {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file: %w", err)
	}

	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	return &TunnelCertificate{
		SSLCert: string(certData),
		SSLKey:  string(keyData),
	}, nil
}

// isCertificateValid checks if the certificate is still valid (not expired)
func isCertificateValid(cert *TunnelCertificate) bool {
	block, _ := pem.Decode([]byte(cert.SSLCert))
	if block == nil {
		return false
	}

	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	now := time.Now()
	// Certificate is valid if it hasn't expired and has at least 30 days remaining
	return now.Before(x509Cert.NotAfter) && now.After(x509Cert.NotBefore) &&
		x509Cert.NotAfter.Sub(now) > 30*24*time.Hour
}

// GetDefaultCertDir returns the default directory for storing self-signed certificates
// Uses the same directory as the executable
func GetDefaultCertDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "/etc/kaitu-slave/certs"
	}
	return filepath.Join(filepath.Dir(exePath), "certs")
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
