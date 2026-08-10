// Package tlspad provides server-side TLS record padding via X.509 certificate
// extension padding. By adding a padding extension to the certificate, the
// DER-encoded certificate (and thus the encrypted TLS Certificate record)
// reaches a target size, making certificate-based fingerprinting harder.
package tlspad

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// PaddingOID is a private OID used for the certificate padding extension.
// 1.3.6.1.4.1.99999.1 — private enterprise arc, padding purpose.
var PaddingOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}

// PadCertificateTemplate adds an X.509 extension with zero-byte padding to make
// the resulting DER-encoded certificate approximately targetDERLen bytes.
// currentDERLen is the measured DER length of a certificate generated from a
// similar template without padding. Call this BEFORE x509.CreateCertificate.
// The template is modified in place.
func PadCertificateTemplate(tmpl *x509.Certificate, currentDERLen, targetDERLen int) {
	if targetDERLen <= currentDERLen {
		return
	}

	// Extension ASN.1 overhead: SEQUENCE { OID, OCTET STRING { value } }
	// OID 1.3.6.1.4.1.99999.1 encodes to ~11 bytes, ASN.1 framing ~8 bytes.
	// We use a conservative estimate; the iterative GeneratePaddedCert corrects drift.
	overhead := 20
	padLen := targetDERLen - currentDERLen - overhead
	if padLen <= 0 {
		return
	}

	tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, pkix.Extension{
		Id:    PaddingOID,
		Value: make([]byte, padLen),
	})
}

// MeasureCertDERLen returns the total DER-encoded length across all certificates
// in the chain.
func MeasureCertDERLen(cert *tls.Certificate) int {
	total := 0
	for _, raw := range cert.Certificate {
		total += len(raw)
	}
	return total
}

// GeneratePaddedCert creates a self-signed certificate with padding to match
// targetDERLen. The private key must be *ecdsa.PrivateKey or *rsa.PrivateKey.
// It uses an iterative approach (max 3 rounds) to converge on the target size
// within +/-20 bytes.
func GeneratePaddedCert(key crypto.PrivateKey, targetDERLen int) (tls.Certificate, error) {
	pub, err := publicKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}

	baseTmpl := baseCertTemplate()

	// First: generate without padding to measure base size
	baseDER, err := x509.CreateCertificate(rand.Reader, baseTmpl, baseTmpl, pub, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tlspad: create base cert: %w", err)
	}
	baseLen := len(baseDER)

	if targetDERLen <= baseLen {
		// No padding needed; target is smaller than or equal to base
		return tls.Certificate{
			Certificate: [][]byte{baseDER},
			PrivateKey:  key,
		}, nil
	}

	// Iterative padding: converge within +/-20 bytes in up to 3 rounds
	currentEstimate := baseLen
	for i := 0; i < 3; i++ {
		tmpl := baseCertTemplate()
		tmpl.SerialNumber = big.NewInt(int64(i + 2))
		PadCertificateTemplate(tmpl, currentEstimate, targetDERLen)

		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("tlspad: create padded cert (iter %d): %w", i, err)
		}
		actual := len(der)

		diff := actual - targetDERLen
		if diff < 0 {
			diff = -diff
		}
		if diff <= 20 {
			return tls.Certificate{
				Certificate: [][]byte{der},
				PrivateKey:  key,
			}, nil
		}

		// Adjust: use actual size as the new "current" estimate for next round
		// The delta between actual and target tells us how much to shift
		currentEstimate = baseLen + (actual - targetDERLen)
		if currentEstimate < baseLen {
			currentEstimate = baseLen
		}
	}

	// Final attempt with best-effort correction
	tmpl := baseCertTemplate()
	tmpl.SerialNumber = big.NewInt(99)
	PadCertificateTemplate(tmpl, currentEstimate, targetDERLen)

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tlspad: create final padded cert: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}, nil
}

func baseCertTemplate() *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "padding"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
}

func publicKey(priv crypto.PrivateKey) (crypto.PublicKey, error) {
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		return &k.PublicKey, nil
	case *rsa.PrivateKey:
		return &k.PublicKey, nil
	default:
		// Also try ed25519 or any key implementing crypto.Signer
		if signer, ok := priv.(crypto.Signer); ok {
			return signer.Public(), nil
		}
		return nil, fmt.Errorf("tlspad: unsupported key type %T", priv)
	}
}

// DefaultTargetDERLen is a sensible default target that makes EC certificates
// look similar in size to RSA-2048 certificates (~850-900 bytes DER).
const DefaultTargetDERLen = 900

// PadToMatch creates a padded self-signed cert using the given key that matches
// the DER length of the reference certificate. Useful for making EC certs the
// same encrypted record size as RSA certs (or vice versa).
func PadToMatch(key crypto.PrivateKey, referenceCert *tls.Certificate) (tls.Certificate, error) {
	target := MeasureCertDERLen(referenceCert)
	return GeneratePaddedCert(key, target)
}

// NewPaddedSelfSigned generates an ECDSA P-256 key and a padded self-signed
// certificate with the given target DER length. This is a convenience for tests
// and quick setups.
func NewPaddedSelfSigned(targetDERLen int) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("tlspad: generate key: %w", err)
	}
	return GeneratePaddedCert(key, targetDERLen)
}
