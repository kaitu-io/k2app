package wire

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CertPair holds RSA + EC self-signed certificates for protocol camouflage.
type CertPair struct {
	RSA        tls.Certificate
	EC         tls.Certificate
	RSAPinHash [32]byte // sha256(PKIX-encoded RSA public key)
	ECPinHash  [32]byte // sha256(PKIX-encoded EC public key)
}

// GenerateCertPair creates RSA-2048 + ECDSA P-256 self-signed certificates.
// Both certs have 10-year validity and minimal subject.
func GenerateCertPair() (*CertPair, error) {
	rsaCert, rsaPin, err := generateSelfSigned(func() (crypto.Signer, error) {
		return rsa.GenerateKey(rand.Reader, 2048)
	})
	if err != nil {
		return nil, fmt.Errorf("wire: generate RSA cert: %w", err)
	}

	ecCert, ecPin, err := generateSelfSigned(func() (crypto.Signer, error) {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	})
	if err != nil {
		return nil, fmt.Errorf("wire: generate EC cert: %w", err)
	}

	return &CertPair{
		RSA:        rsaCert,
		EC:         ecCert,
		RSAPinHash: rsaPin,
		ECPinHash:  ecPin,
	}, nil
}

// generateSelfSigned creates a self-signed certificate using the key from genKey.
func generateSelfSigned(genKey func() (crypto.Signer, error)) (tls.Certificate, [32]byte, error) {
	key, err := genKey()
	if err != nil {
		return tls.Certificate{}, [32]byte{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"k2"}},
		NotBefore:    now,
		NotAfter:     now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("create certificate: %w", err)
	}

	// Compute pin hash: sha256(PKIX-encoded public key)
	pkixPub, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("marshal PKIX public key: %w", err)
	}
	pinHash := sha256.Sum256(pkixPub)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return tlsCert, pinHash, nil
}

// SaveCertPair writes both keypairs to dir as PEM files:
// k2s-rsa.crt, k2s-rsa.key, k2s-ec.crt, k2s-ec.key
func SaveCertPair(dir string, pair *CertPair) error {
	if err := saveCertAndKey(dir, "k2s-rsa", pair.RSA); err != nil {
		return fmt.Errorf("wire: save RSA cert pair: %w", err)
	}
	if err := saveCertAndKey(dir, "k2s-ec", pair.EC); err != nil {
		return fmt.Errorf("wire: save EC cert pair: %w", err)
	}
	return nil
}

// saveCertAndKey writes a single cert+key as PEM files.
func saveCertAndKey(dir, prefix string, cert tls.Certificate) error {
	// Write certificate
	certPath := filepath.Join(dir, prefix+".crt")
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Certificate[0],
	})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write %s: %w", certPath, err)
	}

	// Write private key
	keyPath := filepath.Join(dir, prefix+".key")
	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write %s: %w", keyPath, err)
	}

	return nil
}

// LoadCertPair loads both keypairs from dir.
func LoadCertPair(dir string) (*CertPair, error) {
	rsaCert, rsaPin, err := loadCertAndKey(dir, "k2s-rsa")
	if err != nil {
		return nil, fmt.Errorf("wire: load RSA cert pair: %w", err)
	}

	ecCert, ecPin, err := loadCertAndKey(dir, "k2s-ec")
	if err != nil {
		return nil, fmt.Errorf("wire: load EC cert pair: %w", err)
	}

	return &CertPair{
		RSA:        rsaCert,
		EC:         ecCert,
		RSAPinHash: rsaPin,
		ECPinHash:  ecPin,
	}, nil
}

// loadCertAndKey loads a single cert+key from PEM files and computes the pin hash.
func loadCertAndKey(dir, prefix string) (tls.Certificate, [32]byte, error) {
	certPath := filepath.Join(dir, prefix+".crt")
	keyPath := filepath.Join(dir, prefix+".key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("load %s: %w", prefix, err)
	}

	// Parse the leaf cert to get the public key for pin hashing
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("parse %s cert: %w", prefix, err)
	}

	pkixPub, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		return tls.Certificate{}, [32]byte{}, fmt.Errorf("marshal %s public key: %w", prefix, err)
	}
	pinHash := sha256.Sum256(pkixPub)

	return cert, pinHash, nil
}

// EnsureCertPair loads from dir if files exist, otherwise generates and saves.
func EnsureCertPair(dir string) (*CertPair, error) {
	// Check if all four files exist
	files := []string{"k2s-rsa.crt", "k2s-rsa.key", "k2s-ec.crt", "k2s-ec.key"}
	allExist := true
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			allExist = false
			break
		}
	}

	if allExist {
		return LoadCertPair(dir)
	}

	pair, err := GenerateCertPair()
	if err != nil {
		return nil, err
	}

	if err := SaveCertPair(dir, pair); err != nil {
		return nil, err
	}

	return pair, nil
}

// FormatPins returns the pin URL parameter value for both public keys.
// Format: "sha256:<rsa_urlsafe_b64>,sha256:<ec_urlsafe_b64>"
func (cp *CertPair) FormatPins() string {
	rsaB64 := base64.URLEncoding.EncodeToString(cp.RSAPinHash[:])
	ecB64 := base64.URLEncoding.EncodeToString(cp.ECPinHash[:])
	return fmt.Sprintf("sha256:%s,sha256:%s", rsaB64, ecB64)
}

// TargetProfile describes a remote site's TLS certificate characteristics.
type TargetProfile struct {
	KeyAlgo      string // "rsa" or "ec"
	CertChainLen int    // total DER bytes of all certs in chain
}

// ProbeTarget connects to host:443, completes TLS handshake, returns profile.
func ProbeTarget(host string) (*TargetProfile, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host+":443", &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, fmt.Errorf("wire: probe %s: %w", host, err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("wire: probe %s: no peer certificates", host)
	}

	// Classify leaf cert key algorithm
	var keyAlgo string
	switch state.PeerCertificates[0].PublicKey.(type) {
	case *rsa.PublicKey:
		keyAlgo = "rsa"
	case *ecdsa.PublicKey:
		keyAlgo = "ec"
	default:
		keyAlgo = "unknown"
	}

	// Sum raw DER bytes of all certs in chain
	var chainLen int
	for _, cert := range state.PeerCertificates {
		chainLen += len(cert.Raw)
	}

	return &TargetProfile{
		KeyAlgo:      keyAlgo,
		CertChainLen: chainLen,
	}, nil
}
