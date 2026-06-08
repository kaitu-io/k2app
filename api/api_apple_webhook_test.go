package center

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// buildFakeJWS constructs a minimal JWS string (header.payload.sig) whose header
// contains the given x5c array of base64-encoded DER certs. The payload and
// signature are stubs — verifyAppleJWS only inspects the header.
func buildFakeJWS(t *testing.T, x5c []string) string {
	t.Helper()
	header := map[string]any{"alg": "ES256", "x5c": x5c}
	headerJSON, err := json.Marshal(header)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(headerJSON) + ".stub_payload.stub_sig"
}

// makeSelfSignedChain generates a fake root→leaf chain that does NOT root at the
// Apple Root CA G3, so verifyAppleJWS must reject it.
func makeSelfSignedChain(t *testing.T) (leafDER, rootDER []byte) {
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
		Subject:               pkix.Name{CommonName: "Fake Root CA"},
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

	leafKey := mk()
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Fake Leaf"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
	}
	leafDER, err = x509.CreateCertificate(rand.Reader, leafTmpl, rootCert, &leafKey.PublicKey, rootKey)
	require.NoError(t, err)
	return leafDER, rootDER
}

func TestVerifyAppleJWS(t *testing.T) {
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
		leafDER, _ := makeSelfSignedChain(t)
		jws := buildFakeJWS(t, []string{base64.StdEncoding.EncodeToString(leafDER)})
		err := verifyAppleJWS(jws)
		require.Error(t, err)
		require.Contains(t, err.Error(), "x5c chain has 1")
	})

	t.Run("rejects self-signed chain not rooted at Apple CA", func(t *testing.T) {
		leafDER, rootDER := makeSelfSignedChain(t)
		jws := buildFakeJWS(t, []string{
			base64.StdEncoding.EncodeToString(leafDER),
			base64.StdEncoding.EncodeToString(rootDER),
		})
		err := verifyAppleJWS(jws)
		require.Error(t, err)
		require.Contains(t, err.Error(), "chain")
	})

	t.Run("rejects JWS with malformed base64 in x5c", func(t *testing.T) {
		jws := buildFakeJWS(t, []string{"not-valid-base64!!!", "also-invalid"})
		require.Error(t, verifyAppleJWS(jws))
	})
}
