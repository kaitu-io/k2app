package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

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

// getHardwareID returns a platform-specific hardware identifier.
// Must match the Rust desktop implementation exactly.
func getHardwareID() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "kern.uuid").Output()
		if err != nil {
			return "", fmt.Errorf("sysctl kern.uuid: %w", err)
		}
		return strings.TrimSpace(string(out)), nil

	case "windows":
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			"(Get-CimInstance Win32_ComputerSystemProduct).UUID").Output()
		if err != nil {
			return "", fmt.Errorf("powershell UUID: %w", err)
		}
		id := strings.TrimSpace(string(out))
		// Reject all-F sentinel (means WMI returned no real UUID)
		if strings.ReplaceAll(strings.ReplaceAll(id, "F", ""), "-", "") == "" {
			return "", fmt.Errorf("hardware UUID is all-F sentinel: %s", id)
		}
		return id, nil

	case "linux":
		data, err := os.ReadFile("/etc/machine-id")
		if err != nil {
			return "", fmt.Errorf("read /etc/machine-id: %w", err)
		}
		return strings.TrimSpace(string(data)), nil

	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
