package k2p

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/kaitu-io/k2/wire"
	"github.com/sasha-s/go-deadlock"
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

// PeerAuthListener wraps a wire.Listener and verifies peer auth on the first stream.
type PeerAuthListener struct {
	inner      wire.Listener
	secretHash string
	authDone   bool
	mu         deadlock.Mutex
}

// WrapWithAuth wraps ln to require peer authentication before accepting proxy traffic.
func WrapWithAuth(ln wire.Listener, secretHash string) *PeerAuthListener {
	return &PeerAuthListener{inner: ln, secretHash: secretHash}
}

// AcceptTCP implements wire.Listener. The first accepted connection is treated as the
// auth stream: reads the auth frame, verifies the secret, then accepts the next
// connection for actual proxy traffic.
func (p *PeerAuthListener) AcceptTCP(ctx context.Context) (net.Conn, string, error) {
	conn, addr, err := p.inner.AcceptTCP(ctx)
	if err != nil {
		return nil, "", err
	}

	p.mu.Lock()
	needAuth := !p.authDone
	p.mu.Unlock()

	if needAuth {
		// First stream is the auth stream.
		secret, err := ReadAuthFrame(conn)
		if err != nil {
			conn.Close()
			return nil, "", fmt.Errorf("k2p: read auth: %w", err)
		}
		if !VerifySecret(secret, p.secretHash) {
			conn.Write([]byte{AuthRejected}) //nolint:errcheck
			conn.Close()
			return nil, "", fmt.Errorf("k2p: peer auth rejected")
		}
		conn.Write([]byte{AuthOK}) //nolint:errcheck
		conn.Close()

		p.mu.Lock()
		p.authDone = true
		p.mu.Unlock()

		// Accept the next stream (actual proxy traffic).
		return p.inner.AcceptTCP(ctx)
	}

	return conn, addr, nil
}

// ServeUDP implements wire.Listener.
func (p *PeerAuthListener) ServeUDP(ctx context.Context, handler wire.UDPHandler) error {
	return p.inner.ServeUDP(ctx, handler)
}

// Close implements wire.Listener.
func (p *PeerAuthListener) Close() error {
	return p.inner.Close()
}
