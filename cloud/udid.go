package cloud

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// UDID returns a stable device identifier as a 64-char hex string.
// It is derived from the platform's machine ID using SHA-256.
func UDID() string {
	id := platformID()
	if id == "" {
		// Fallback: use hostname if platform ID unavailable.
		// This is less stable but better than empty.
		id = fallbackID()
	}
	hash := sha256.Sum256([]byte(id + ":k2"))
	return hex.EncodeToString(hash[:])
}

// fallbackID returns the hostname as a last-resort identifier.
func fallbackID() string {
	name, _ := os.Hostname()
	return name
}
