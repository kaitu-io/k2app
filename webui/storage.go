//go:build linux

package webui

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sasha-s/go-deadlock"
	"golang.org/x/crypto/hkdf"
)

const (
	encPrefix = "ENC1:"
	hkdfSalt  = "kaitu-gateway-storage-v1"
	hkdfInfo  = "aes-256-gcm-key"
)

// storageFile is the on-disk JSON structure.
type storageFile struct {
	Items map[string]string `json:"items"`
}

// Storage is a thread-safe, encrypted key-value store persisted to disk.
// Values are encrypted with AES-256-GCM; the key is derived from a hardware
// identifier via HKDF-SHA256. Plaintext values (no ENC1: prefix) are read
// as-is for backward compatibility.
//
// The HKDF salt is "kaitu-gateway-storage-v1" regardless of which Kaitu
// binary hosts the store, so k2r and k2 on the same machine see identical
// ciphertexts and can share a storage file if needed.
type Storage struct {
	mu   deadlock.RWMutex
	path string
	key  [32]byte
	data storageFile
}

// deriveKey derives a 32-byte AES key from hardwareID using HKDF-SHA256.
// Panics only if the stdlib HKDF implementation fails, which is a
// programming error (it cannot fail for 32 bytes with SHA-256).
func deriveKey(hardwareID string) [32]byte {
	r := hkdf.New(sha256.New, []byte(hardwareID), []byte(hkdfSalt), []byte(hkdfInfo))
	var key [32]byte
	if _, err := io.ReadFull(r, key[:]); err != nil {
		panic(fmt.Sprintf("webui.deriveKey: %v", err))
	}
	return key
}

// getMachineID reads the Linux machine-id from the canonical locations.
// Returns an empty string if both canonical paths are unreadable.
func getMachineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// encryptValue encrypts plaintext with AES-256-GCM and returns
// "ENC1:" + base64(nonce || ciphertext || tag).
func encryptValue(plaintext string, key [32]byte) string {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(fmt.Sprintf("webui.encryptValue: NewCipher: %v", err))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(fmt.Sprintf("webui.encryptValue: NewGCM: %v", err))
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(fmt.Sprintf("webui.encryptValue: read nonce: %v", err))
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext)
}

// decryptValue decrypts a value produced by encryptValue.
// If the value does not have the ENC1: prefix it is returned as-is
// (backward compatibility with plaintext storage files).
func decryptValue(encrypted string, key [32]byte) (string, error) {
	if !strings.HasPrefix(encrypted, encPrefix) {
		return encrypted, nil
	}

	raw, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("webui.decryptValue: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("webui.decryptValue: NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("webui.decryptValue: NewGCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("webui.decryptValue: ciphertext too short")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("webui.decryptValue: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// isEncrypted reports whether value was produced by encryptValue.
func isEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}

// newStorage creates a Storage at path using key, loading any existing data.
// Internal constructor — callers use NewStorage or NewStorageWithKey.
func newStorage(path string, key [32]byte) *Storage {
	s := &Storage{
		path: path,
		key:  key,
		data: storageFile{Items: make(map[string]string)},
	}
	s.load()
	return s
}

// NewStorage creates a Storage at path. The encryption key is derived from
// the Linux machine-id via HKDF-SHA256. Existing data on disk is loaded.
func NewStorage(path string) *Storage {
	return newStorage(path, deriveKey(getMachineID()))
}

// NewStorageWithKey creates a Storage at path using the given hardwareID for
// key derivation. Intended for tests that need a deterministic key.
func NewStorageWithKey(path, hardwareID string) *Storage {
	return newStorage(path, deriveKey(hardwareID))
}

// load reads the storage file from disk. Errors (missing file, bad JSON) are
// silently ignored — the store starts empty.
func (s *Storage) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var sf storageFile
	if json.Unmarshal(data, &sf) != nil {
		return
	}
	if sf.Items == nil {
		sf.Items = make(map[string]string)
	}
	s.data = sf
}

// save persists the current state to disk atomically (write-to-tmp + rename).
// Holds RLock for the marshal step to prevent concurrent map iteration + write.
func (s *Storage) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("webui.Storage save: mkdir: %w", err)
	}

	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("webui.Storage save: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("webui.Storage save: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("webui.Storage save: rename: %w", err)
	}
	return nil
}

// Get returns the decrypted value for key, or an error if the key is absent
// or decryption fails.
func (s *Storage) Get(key string) (string, error) {
	s.mu.RLock()
	raw, ok := s.data.Items[key]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("webui.Storage: key %q not found", key)
	}
	return decryptValue(raw, s.key)
}

// Set encrypts value and persists it under key.
func (s *Storage) Set(key, value string) error {
	encrypted := encryptValue(value, s.key)
	s.mu.Lock()
	s.data.Items[key] = encrypted
	s.mu.Unlock()
	return s.save()
}

// Remove deletes key from storage and persists the change.
func (s *Storage) Remove(key string) {
	s.mu.Lock()
	delete(s.data.Items, key)
	s.mu.Unlock()
	s.save() //nolint:errcheck
}

// Has reports whether key exists in storage.
func (s *Storage) Has(key string) bool {
	s.mu.RLock()
	_, ok := s.data.Items[key]
	s.mu.RUnlock()
	return ok
}

// Keys returns a snapshot of all stored keys.
func (s *Storage) Keys() []string {
	s.mu.RLock()
	keys := make([]string, 0, len(s.data.Items))
	for k := range s.data.Items {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	return keys
}

// Clear removes all keys and persists the empty state.
func (s *Storage) Clear() {
	s.mu.Lock()
	s.data.Items = make(map[string]string)
	s.mu.Unlock()
	s.save() //nolint:errcheck
}
