package wire

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// AuthInfo holds auto-generated server credentials for zero-config startup.
type AuthInfo struct {
	UDID  string `json:"udid"`
	Token string `json:"token"`
}

// EnsureAuth loads auth credentials from path if it exists, or generates
// new random credentials and saves them.
func EnsureAuth(path string) (*AuthInfo, error) {
	// Try to load existing
	if data, err := os.ReadFile(path); err == nil {
		var auth AuthInfo
		if err := json.Unmarshal(data, &auth); err != nil {
			return nil, fmt.Errorf("wire: parse auth file: %w", err)
		}
		return &auth, nil
	}

	// Generate new credentials
	udidBytes := make([]byte, 4) // 4 bytes = 8 hex chars
	if _, err := rand.Read(udidBytes); err != nil {
		return nil, fmt.Errorf("wire: generate UDID: %w", err)
	}

	tokenBytes := make([]byte, 16) // 16 bytes = 32 hex chars
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("wire: generate token: %w", err)
	}

	auth := &AuthInfo{
		UDID:  hex.EncodeToString(udidBytes),
		Token: hex.EncodeToString(tokenBytes),
	}

	// Save to file
	data, err := json.Marshal(auth)
	if err != nil {
		return nil, fmt.Errorf("wire: marshal auth: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, fmt.Errorf("wire: save auth file: %w", err)
	}

	return auth, nil
}
