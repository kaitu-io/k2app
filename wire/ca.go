package wire

import (
	"crypto/x509"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed kaitu_ca.pem
var kaituCAPEM []byte

// blockedCAOrgs contains organization names of Chinese CAs to reject (pre-uppercased).
var blockedCAOrgs = []string{
	"CNNIC",
	"WOSIGN",
	"WOTRUS",
	"CFCA",
	"GDCA",
	"SHECA",
	"CHINA FINANCIAL CERTIFICATION AUTHORITY",
	"SHANGHAI ELECTRONIC CERTIFICATION AUTHORITY",
	"GUANG DONG CERTIFICATE AUTHORITY",
}

// kaituCACertPool contains only the Kaitu CA certificate, parsed once at init.
// Used by buildPinVerifier as a fallback: certs signed by Kaitu CA are always accepted.
var kaituCACertPool *x509.CertPool

func init() {
	kaituCACertPool = x509.NewCertPool()
	kaituCACertPool.AppendCertsFromPEM(kaituCAPEM)
}

// BuildDefaultCAPool returns a system CA pool augmented with the embedded Kaitu CA.
func BuildDefaultCAPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(kaituCAPEM)
	return pool
}

// IsBlockedCA checks if a certificate was issued by a blocked Chinese CA.
func IsBlockedCA(cert *x509.Certificate) bool {
	for _, org := range cert.Subject.Organization {
		upper := strings.ToUpper(org)
		for _, blocked := range blockedCAOrgs {
			if strings.Contains(upper, blocked) {
				return true
			}
		}
	}
	for _, org := range cert.Issuer.Organization {
		upper := strings.ToUpper(org)
		for _, blocked := range blockedCAOrgs {
			if strings.Contains(upper, blocked) {
				return true
			}
		}
	}
	return false
}

// buildBlockedCAVerifier returns a VerifyPeerCertificate callback that rejects
// certificates issued by blocked Chinese CAs. This runs AFTER standard TLS
// verification (CA chain validation) has passed.
func buildBlockedCAVerifier() func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		for _, chain := range verifiedChains {
			for _, cert := range chain {
				if IsBlockedCA(cert) {
					return fmt.Errorf("wire: certificate issued by blocked CA: %s", cert.Issuer.Organization)
				}
			}
		}
		return nil
	}
}
