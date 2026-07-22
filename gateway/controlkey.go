//go:build linux

package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// controlKeyFile persists the account control-key hash that gates the panel
// HTTP API. Only the SHA-256 hex ever touches disk — the router never stores
// the plaintext key.
type controlKeyFile struct {
	Hash string `json:"hash"`
}

func controlKeyPath() string { return filepath.Join(stateDir(), "control-key.json") }

func hashControlKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func loadControlKeyHash() string {
	data, err := os.ReadFile(controlKeyPath())
	if err != nil {
		return ""
	}
	var f controlKeyFile
	if json.Unmarshal(data, &f) != nil {
		return ""
	}
	return f.Hash
}

func saveControlKeyHash(hash string) error {
	if err := os.MkdirAll(stateDir(), 0700); err != nil {
		return err
	}
	data, _ := json.Marshal(controlKeyFile{Hash: hash})
	return os.WriteFile(controlKeyPath(), data, 0600)
}

func clearControlKeyHash() error {
	if err := os.Remove(controlKeyPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
