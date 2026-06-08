package center

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// makeTestChain builds root→intermediate→leaf ECDSA chain.
// Returns the root as PEM (trust anchor for injection), leaf private key (for
// signing test JWS), and the DER bytes of leaf and intermediate (for x5c).
func makeTestChain(t *testing.T) (rootPEM []byte, leafKey *ecdsa.PrivateKey, leafDER, intDER []byte) {
	t.Helper()
	mk := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		return k
	}
	now := time.Now()

	rootKey := mk()
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	rootCert, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)
	rootPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})

	intKey := mk()
	intTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Intermediate CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	intDER, err = x509.CreateCertificate(rand.Reader, intTmpl, rootCert, &intKey.PublicKey, rootKey)
	require.NoError(t, err)
	intCert, err := x509.ParseCertificate(intDER)
	require.NoError(t, err)

	leafKey = mk()
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Leaf"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
	}
	leafDER, err = x509.CreateCertificate(rand.Reader, leafTmpl, intCert, &leafKey.PublicKey, intKey)
	require.NoError(t, err)
	return rootPEM, leafKey, leafDER, intDER
}

// signJWS builds a properly signed ES256 JWS using RFC 7518 §3.4 raw R||S encoding.
func signJWS(t *testing.T, leafKey *ecdsa.PrivateKey, leafDER, intDER []byte, claimsJSON string) string {
	t.Helper()
	header := map[string]any{
		"alg": "ES256",
		"x5c": []string{
			base64.StdEncoding.EncodeToString(leafDER),
			base64.StdEncoding.EncodeToString(intDER),
		},
	}
	headerJSON, err := json.Marshal(header)
	require.NoError(t, err)
	headerEnc := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadEnc := base64.RawURLEncoding.EncodeToString([]byte(claimsJSON))
	signingInput := headerEnc + "." + payloadEnc

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, leafKey, digest[:])
	require.NoError(t, err)

	// RFC 7518 §3.4: raw R||S, each padded to the curve's order size (32 bytes for P-256).
	sigBytes := make([]byte, 64)
	r.FillBytes(sigBytes[:32])
	s.FillBytes(sigBytes[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sigBytes)
}

func TestVerifyAppleJWSWithRoot(t *testing.T) {
	rootPEM, leafKey, leafDER, intDER := makeTestChain(t)

	t.Run("valid chain and signature passes", func(t *testing.T) {
		jws := signJWS(t, leafKey, leafDER, intDER, `{"test":"ok"}`)
		require.NoError(t, verifyAppleJWSWithRoot(jws, rootPEM))
	})

	t.Run("tampered payload fails signature", func(t *testing.T) {
		jws := signJWS(t, leafKey, leafDER, intDER, `{"originalTransactionId":"real"}`)
		// Replace payload with forged content.
		parts := splitJWS(jws)
		forgedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"originalTransactionId":"forged"}`))
		tampered := parts[0] + "." + forgedPayload + "." + parts[2]
		err := verifyAppleJWSWithRoot(tampered, rootPEM)
		require.Error(t, err)
		require.Contains(t, err.Error(), "signature verification failed")
	})

	t.Run("wrong algorithm rejected before cert check", func(t *testing.T) {
		headerJSON, _ := json.Marshal(map[string]any{
			"alg": "HS256",
			"x5c": []string{
				base64.StdEncoding.EncodeToString(leafDER),
				base64.StdEncoding.EncodeToString(intDER),
			},
		})
		jws := base64.RawURLEncoding.EncodeToString(headerJSON) + ".payload.sig"
		err := verifyAppleJWSWithRoot(jws, rootPEM)
		require.Error(t, err)
		require.Contains(t, err.Error(), "ES256")
	})

	t.Run("wrong root CA rejects valid Apple-looking chain", func(t *testing.T) {
		jws := signJWS(t, leafKey, leafDER, intDER, `{"test":"ok"}`)
		// Use a different root as the trust anchor.
		wrongRootPEM, _, _, _ := makeTestChain(t)
		err := verifyAppleJWSWithRoot(jws, wrongRootPEM)
		require.Error(t, err)
		require.Contains(t, err.Error(), "chain")
	})
}

func TestVerifyAppleJWS_Structural(t *testing.T) {
	t.Run("rejects empty payload", func(t *testing.T) {
		require.Error(t, verifyAppleJWS(""))
	})

	t.Run("rejects payload with fewer than 3 parts", func(t *testing.T) {
		require.Error(t, verifyAppleJWS("only.two"))
	})

	t.Run("rejects JWS with no x5c field", func(t *testing.T) {
		headerJSON, _ := json.Marshal(map[string]any{"alg": "ES256"})
		jws := base64.RawURLEncoding.EncodeToString(headerJSON) + ".payload.sig"
		err := verifyAppleJWS(jws)
		require.Error(t, err)
		require.Contains(t, err.Error(), "x5c chain has 0")
	})

	t.Run("rejects JWS with only one x5c cert", func(t *testing.T) {
		_, _, leafDER, _ := makeTestChain(t)
		headerJSON, _ := json.Marshal(map[string]any{
			"alg": "ES256",
			"x5c": []string{base64.StdEncoding.EncodeToString(leafDER)},
		})
		jws := base64.RawURLEncoding.EncodeToString(headerJSON) + ".payload.sig"
		err := verifyAppleJWS(jws)
		require.Error(t, err)
		require.Contains(t, err.Error(), "x5c chain has 1")
	})

	t.Run("rejects JWS with malformed base64 in x5c", func(t *testing.T) {
		headerJSON, _ := json.Marshal(map[string]any{
			"alg": "ES256",
			"x5c": []string{"not-valid-base64!!!", "also-invalid"},
		})
		jws := base64.RawURLEncoding.EncodeToString(headerJSON) + ".payload.sig"
		require.Error(t, verifyAppleJWS(jws))
	})
}

// splitJWS splits a JWS into its 3 parts.
func splitJWS(jws string) [3]string {
	var parts [3]string
	idx := 0
	start := 0
	for i, c := range jws {
		if c == '.' {
			parts[idx] = jws[start:i]
			idx++
			start = i + 1
			if idx == 2 {
				parts[2] = jws[start:]
				break
			}
		}
	}
	return parts
}
