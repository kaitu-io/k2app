package wire

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kaitu-io/kuic"
)

// quicResetKeyFileName is the QUIC stateless-reset key in cert_dir.
const quicResetKeyFileName = "quic-reset.key"

// EnsureStatelessResetKey loads dir/quic-reset.key or generates and persists a
// fresh one.
//
// The key must survive k2s restarts. Every connection ID the server issues
// carries a reset token derived from this key; a restarted process holding the
// SAME key answers a packet for a connection it no longer has with a stateless
// reset the client recognises, and the client drops the connection at once
// instead of waiting out MaxIdleTimeout. It covers the exits that send no
// CONNECTION_CLOSE (SIGKILL, crash, OOM). It only works for connections opened
// while the key already existed, so it takes effect from the second restart
// after first deployment.
//
// A missing or malformed file is replaced rather than refused: losing the key
// costs one restart's fast-fail, never a boot. Written once at provisioning,
// like the ECH key, so a plain WriteFile is enough.
func EnsureStatelessResetKey(dir string) (*quic.StatelessResetKey, error) {
	path := filepath.Join(dir, quicResetKeyFileName)
	var key quic.StatelessResetKey
	data, err := os.ReadFile(path)
	if err == nil && len(data) == len(key) {
		copy(key[:], data)
		return &key, nil
	}
	if err == nil {
		slog.Warn("wire: QUIC reset key malformed, regenerating", "path", path, "len", len(data))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("wire: QUIC reset key read: %w", err)
	}
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("wire: QUIC reset key gen: %w", err)
	}
	if err := os.WriteFile(path, key[:], 0o600); err != nil {
		return nil, fmt.Errorf("wire: QUIC reset key write: %w", err)
	}
	return &key, nil
}
