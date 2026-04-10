//go:build linux

package gateway

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
	encPrefix       = "ENC1:"
	hkdfSalt        = "kaitu-gateway-storage-v1"
	hkdfInfo        = "aes-256-gcm-key"
	defaultStorPath = "storage.json"
)

// storageFile is the on-disk JSON structure.
type storageFile struct {
	Items map[string]string `json:"items"`
}

// Storage is a thread-safe, encrypted key-value store persisted to disk.
// Values are encrypted with AES-256-GCM; the key is derived from a hardware
// identifier via HKDF-SHA256. Plaintext values (no ENC1: prefix) are read
// as-is for backward compatibility.
type Storage struct {
	mu   deadlock.RWMutex
	path string
	key  [32]byte
	data storageFile
}

// deriveKey derives a 32-byte AES key from hardwareID using HKDF-SHA256.
func deriveKey(hardwareID string) [32]byte {
	r := hkdf.New(sha256.New, []byte(hardwareID), []byte(hkdfSalt), []byte(hkdfInfo))
	var key [32]byte
	if _, err := io.ReadFull(r, key[:]); err != nil {
		// hkdf with sha256 never fails for 32 bytes — panic would indicate a
		// programming error.
		panic(fmt.Sprintf("deriveKey: %v", err))
	}
	return key
}

// getMachineID reads the Linux machine-id from the canonical locations.
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
		panic(fmt.Sprintf("encryptValue: NewCipher: %v", err))
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(fmt.Sprintf("encryptValue: NewGCM: %v", err))
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(fmt.Sprintf("encryptValue: read nonce: %v", err))
	}

	// Seal appends ciphertext + tag after nonce.
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext)
}

// decryptValue decrypts a value produced by encryptValue.
// If the value does not have the ENC1: prefix it is returned as-is (backward
// compatibility with plaintext storage files).
func decryptValue(encrypted string, key [32]byte) (string, error) {
	if !strings.HasPrefix(encrypted, encPrefix) {
		return encrypted, nil
	}

	raw, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("decryptValue: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("decryptValue: NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decryptValue: NewGCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("decryptValue: ciphertext too short")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryptValue: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// isEncrypted reports whether value was produced by encryptValue.
func isEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}

// newStorage creates a Storage at path using key, loading any existing data.
func newStorage(path string, key [32]byte) *Storage {
	s := &Storage{
		path: path,
		key:  key,
		data: storageFile{Items: make(map[string]string)},
	}
	s.load()
	return s
}

// NewDefaultStorage returns a Storage backed by /etc/k2r/storage.json with a
// key derived from the Linux machine-id.
func NewDefaultStorage() *Storage {
	path := filepath.Join(stateDir(), defaultStorPath)
	key := deriveKey(getMachineID())
	return newStorage(path, key)
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
		return fmt.Errorf("storage save: mkdir: %w", err)
	}

	s.mu.RLock()
	data, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("storage save: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("storage save: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("storage save: rename: %w", err)
	}
	return nil
}

// Get returns the decrypted value for key, or an error if the key is absent or
// decryption fails.
func (s *Storage) Get(key string) (string, error) {
	s.mu.RLock()
	raw, ok := s.data.Items[key]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("storage: key %q not found", key)
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
