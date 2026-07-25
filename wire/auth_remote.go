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
//
// nodeIPv4Fn + nodeSecret are the node's own Basic-auth identity: that endpoint
// sits behind Center's SlaveAuthRequired() (username=ipv4, password=NodeSecret).
// Omitting them made every device-auth request 401 — the reason /slave/device-
// check-auth showed 0 hits in production despite being wired. An empty secret
// means the node has no Center identity: fail closed without firing a request.
//
// nodeIPv4Fn is a lazy provider: nodes commonly don't set K2_NODE_IPV4 and must
// ProbePublicIP at runtime, which can't run at NewServer() time (may block /
// network not up). It's called on first auth (a client already connected → net
// is up) and should memoize (sync.Once) — see server.go.
func NewRemoteValidator(remoteURL string, nodeIPv4Fn func() string, nodeSecret string) JWTValidator {
	return func(jwt, udid, mode string) bool {
		if nodeSecret == "" {
			slog.Warn("wire: remote auth: node secret unset, rejecting", "udid", udid)
			return false
		}
		nodeIPv4 := nodeIPv4Fn()
		if nodeIPv4 == "" {
			slog.Warn("wire: remote auth: node ipv4 unresolved, rejecting", "udid", udid)
			return false
		}
		body, err := json.Marshal(struct {
			UDID  string `json:"udid"`
			Token string `json:"token"`
			Mode  string `json:"mode,omitempty"`
		}{UDID: udid, Token: jwt, Mode: mode})
		if err != nil {
			slog.Warn("wire: remote auth: marshal failed", "err", err)
			return false
		}

		req, err := http.NewRequest(http.MethodPost, remoteURL, bytes.NewReader(body))
		if err != nil {
			slog.Warn("wire: remote auth: build request failed", "url", remoteURL, "err", err)
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(nodeIPv4, nodeSecret)

		resp, err := remoteAuthClient.Do(req)
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
