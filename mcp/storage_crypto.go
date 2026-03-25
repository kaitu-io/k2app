package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/denisbrodbeck/machineid"
	"golang.org/x/crypto/hkdf"
)

const (
	encPrefix = "ENC1:"
	hkdfSalt  = "kaitu-desktop-storage-v1"
	hkdfInfo  = "aes-256-gcm-key"
)

// deriveKey uses HKDF-SHA256 to derive a 32-byte AES-256 key from a hardware ID.
// Salt and info strings match the Rust desktop implementation exactly.
func deriveKey(hardwareID string) [32]byte {
	hkdfReader := hkdf.New(sha256.New, []byte(hardwareID), []byte(hkdfSalt), []byte(hkdfInfo))
	var key [32]byte
	if _, err := io.ReadFull(hkdfReader, key[:]); err != nil {
		panic("hkdf: " + err.Error()) // should never fail with valid inputs
	}
	return key
}

// decryptValue strips the ENC1: prefix, base64 decodes, splits nonce(12) || ciphertext+tag,
// and decrypts with AES-256-GCM.
func decryptValue(encrypted string, key [32]byte) (string, error) {
	if !isEncrypted(encrypted) {
		return "", fmt.Errorf("value is not encrypted (missing %s prefix)", encPrefix)
	}

	raw, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	if len(raw) < 12+16 {
		return "", fmt.Errorf("ciphertext too short: %d bytes", len(raw))
	}

	nonce := raw[:12]
	ciphertext := raw[12:]

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("gcm decrypt: %w", err)
	}

	return string(plaintext), nil
}

// isEncrypted checks whether a value has the ENC1: prefix.
func isEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}

// getHardwareID returns a platform-specific hardware identifier via
// denisbrodbeck/machineid (same sources as Rust machine-uid crate):
//   - macOS: IOPlatformUUID via ioreg
//   - Windows: Registry HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid
//   - Linux: /var/lib/dbus/machine-id → /etc/machine-id
func getHardwareID() (string, error) {
	return machineid.ID()
}
