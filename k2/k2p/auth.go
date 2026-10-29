package k2p

import (
	"context"
	"crypto/sha256"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/k2/wire"
	"golang.org/x/crypto/bcrypt"
)

const authVersion = 0x01

// Auth response codes.
const (
	AuthOK       byte = 0x00
	AuthRejected byte = 0x01
)

// WriteAuthFrame writes a peer auth frame to w.
// Frame: [1 byte version=0x01][2 bytes length big-endian][N bytes secret UTF-8]
func WriteAuthFrame(w io.Writer, secret string) error {
	b := []byte(secret)
	if len(b) > 65535 {
		return fmt.Errorf("k2p: secret too long: %d bytes (max 65535)", len(b))
	}

	hdr := [3]byte{}
	hdr[0] = authVersion
	binary.BigEndian.PutUint16(hdr[1:], uint16(len(b)))

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("k2p: write auth header: %w", err)
	}
	if len(b) > 0 {
		if _, err := w.Write(b); err != nil {
			return fmt.Errorf("k2p: write auth body: %w", err)
		}
	}
	return nil
}

// ReadAuthFrame reads a peer auth frame from r and returns the secret.
func ReadAuthFrame(r io.Reader) (string, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", fmt.Errorf("k2p: read auth header: %w", err)
	}
	if hdr[0] != authVersion {
		return "", fmt.Errorf("k2p: auth version mismatch: got 0x%02x, want 0x%02x", hdr[0], authVersion)
	}
	length := binary.BigEndian.Uint16(hdr[1:])
	if length == 0 {
		return "", nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return "", fmt.Errorf("k2p: read auth body: %w", err)
	}
	return string(body), nil
}

// HashSecret returns a bcrypt hash of secret.
func HashSecret(secret string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("k2p: hash secret: %w", err)
	}
	return string(h), nil
}

// VerifySecret returns true if secret matches the bcrypt hash.
func VerifySecret(secret, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}

// verifyPeerAuth accepts the first QUIC stream, reads the auth frame, verifies it,
// and sends the response. Used by Home to authenticate Away at the raw QUIC level
// (before QUICServer takes over for k2v5 data plane).
func verifyPeerAuth(ctx context.Context, conn *quic.Conn, secretHash string) error {
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return fmt.Errorf("k2p: home: accept auth stream: %w", err)
	}
	secret, err := ReadAuthFrame(stream)
	if err != nil {
		stream.Close()
		return fmt.Errorf("k2p: home: read auth: %w", err)
	}
	if !VerifySecret(secret, secretHash) {
		stream.Write([]byte{AuthRejected}) //nolint:errcheck
		stream.Close()
		return fmt.Errorf("k2p: home: peer auth rejected")
	}
	stream.Write([]byte{AuthOK}) //nolint:errcheck
	stream.Close()
	return nil
}

// buildPinTLSConfig creates a TLS config with optional certificate pin verification.
// If cfg.Pin is empty, InsecureSkipVerify is true (self-signed Home cert accepted).
// If cfg.Pin is set, the peer certificate's SPKI hash must match one of the pins.
func buildPinTLSConfig(cfg Config) *tls.Config {
	tlsCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true, // skip CA chain (self-signed Home cert)
	}
	if cfg.Pin != "" {
		expectedPins, err := wire.ParsePins(cfg.Pin)
		if err == nil && len(expectedPins) > 0 {
			tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return verifyPins(rawCerts, expectedPins)
			}
		}
	}
	return tlsCfg
}

// verifyPins checks that at least one certificate's SPKI hash matches an expected pin.
func verifyPins(rawCerts [][]byte, expected [][32]byte) error {
	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			continue
		}
		pubDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
		if err != nil {
			continue
		}
		hash := sha256.Sum256(pubDER)
		for _, exp := range expected {
			if hash == exp {
				return nil
			}
		}
	}
	return fmt.Errorf("k2p: no certificate matched pin")
}

// doPeerAuth sends a peer auth frame on a new QUIC stream and reads the response.
// No-op if secret is empty.
func doPeerAuth(ctx context.Context, conn *quic.Conn, secret string) error {
	if secret == "" {
		return nil
	}

	authStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("k2p: open auth stream: %w", err)
	}
	if err := WriteAuthFrame(authStream, secret); err != nil {
		authStream.Close()
		return fmt.Errorf("k2p: write auth: %w", err)
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(authStream, resp); err != nil {
		authStream.Close()
		return fmt.Errorf("k2p: read auth response: %w", err)
	}
	authStream.Close()
	if resp[0] != AuthOK {
		return fmt.Errorf("k2p: peer auth rejected by Home")
	}
	return nil
}
