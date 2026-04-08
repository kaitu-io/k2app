package wire

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

var remoteAuthClient = &http.Client{Timeout: 5 * time.Second}

// NewRemoteValidator creates a JWTValidator that validates credentials
// against a remote Center API endpoint (POST /slave/device-check-auth).
func NewRemoteValidator(remoteURL string) JWTValidator {
	return func(jwt, udid, mode string) bool {
		body, err := json.Marshal(struct {
			UDID  string `json:"udid"`
			Token string `json:"token"`
			Mode  string `json:"mode,omitempty"`
		}{UDID: udid, Token: jwt, Mode: mode})
		if err != nil {
			slog.Warn("wire: remote auth: marshal failed", "err", err)
			return false
		}

		resp, err := remoteAuthClient.Post(remoteURL, "application/json", bytes.NewReader(body))
		if err != nil {
			slog.Warn("wire: remote auth: request failed", "url", remoteURL, "err", err)
			return false
		}
		defer resp.Body.Close()

		var result struct {
			Code int `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			slog.Warn("wire: remote auth: decode failed", "url", remoteURL, "err", err)
			return false
		}

		if result.Code != 0 {
			slog.Debug("wire: remote auth: rejected", "udid", udid, "code", result.Code)
			return false
		}
		return true
	}
}
