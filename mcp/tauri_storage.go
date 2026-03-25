package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// TauriSession holds the session credentials extracted from Tauri's storage.json.
type TauriSession struct {
	AccessToken  string
	RefreshToken string
	RawUDID      string // Raw UUID before hashing
	HashedUDID   string // SHA-256[:16 bytes] as 32 hex chars — the UDID used by Center API
}

// tauriStoragePath returns the platform-specific path to Tauri's storage.json.
func tauriStoragePath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "io.kaitu.desktop", "storage.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		return filepath.Join(appdata, "io.kaitu.desktop", "storage.json")
	case "linux":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "io.kaitu.desktop", "storage.json")
	default:
		return ""
	}
}

// readTauriStorage reads and decrypts values from a Tauri storage.json file.
// If key is nil, encrypted values are skipped silently.
func readTauriStorage(path string, key *[32]byte) (*TauriSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read storage file: %w", err)
	}

	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse storage JSON: %w", err)
	}

	// Decrypt values in place (per-key fault tolerance — one corrupt key
	// must not prevent reading other healthy keys)
	decrypted := make(map[string]string, len(store))
	for k, v := range store {
		if isEncrypted(v) {
			if key == nil {
				continue // skip encrypted values when no key provided
			}
			plain, err := decryptValue(v, *key)
			if err != nil {
				log.Printf("[tauri_storage] skip key %q: decrypt failed: %v", k, err)
				continue
			}
			decrypted[k] = plain
		} else {
			decrypted[k] = v
		}
	}

	// Strip JSON double-encoding: Tauri JS does JSON.stringify() on values,
	// so a string "hello" is stored as "\"hello\"". We unmarshal to strip the outer quotes.
	stripped := make(map[string]string, len(decrypted))
	for k, v := range decrypted {
		var s string
		if err := json.Unmarshal([]byte(v), &s); err == nil {
			stripped[k] = s
		} else {
			stripped[k] = v // keep as-is if not a JSON string
		}
	}

	session := &TauriSession{
		AccessToken:  stripped["k2.auth.token"],
		RefreshToken: stripped["k2.auth.refresh"],
		RawUDID:      stripped["device-udid"],
	}

	if session.RawUDID != "" {
		session.HashedUDID = hashUDID(session.RawUDID)
	}

	return session, nil
}

// hashUDID computes SHA-256 of the raw UDID and returns the first 16 bytes as 32 hex chars.
// This matches the webapp's UDID hashing.
func hashUDID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum[:16])
}
