package wire

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

var remoteAuthClient = &http.Client{Timeout: 5 * time.Second}

// NewRemoteValidator creates a Validator that validates credentials against a
// remote Center API endpoint (POST /slave/device-check-auth).
//
// Verdict mapping (spec §6.4 — "fail-closed only for Center saying no, never
// for Center not speaking"):
//   - JSON code 0            → OK, with UserID + ServiceExpiredAt from data
//   - JSON code 401/402/403  → explicit rejection with that code
//   - transport error / HTTP 5xx / decode failure / any other JSON code
//     (422 bad request, 500 Center bug, …) → Code -1 (cannot determine)
//   - node config problems (empty NodeSecret, unresolvable node IPv4, bad
//     URL) → Code -2 (AuthCodeNodeFault): still fail-closed under enforce, but
//     reported as a SERVER fault rather than as the device's credentials
//     being invalid. See AuthCodeNodeFault's doc for why this is not folded
//     into the 401 tail. Each emits `DIAG: auth-node-misconfigured`, which is
//     also what the Slack alerter pages on.
//
// nodeIPv4Fn + nodeSecret are the node's Basic-auth identity (username=ipv4,
// password=NodeSecret): /slave/device-check-auth sits behind Center's
// SlaveAuthRequired(). nodeIPv4Fn is a lazy memoizing provider — see server.go.
func NewRemoteValidator(remoteURL string, nodeIPv4Fn func() string, nodeSecret string) Validator {
	return func(token, udid, mode string) AuthVerdict {
		if nodeSecret == "" {
			slog.Warn("DIAG: auth-node-misconfigured", "reason", "node_secret_unset", "udid", udid)
			return AuthVerdict{Code: AuthCodeNodeFault}
		}
		nodeIPv4 := nodeIPv4Fn()
		if nodeIPv4 == "" {
			slog.Warn("DIAG: auth-node-misconfigured", "reason", "node_ipv4_unresolved", "udid", udid)
			return AuthVerdict{Code: AuthCodeNodeFault}
		}
		body, err := json.Marshal(struct {
			UDID  string `json:"udid"`
			Token string `json:"token"`
			Mode  string `json:"mode,omitempty"`
		}{UDID: udid, Token: token, Mode: mode})
		if err != nil {
			slog.Warn("DIAG: auth-node-misconfigured", "reason", "marshal_failed", "err", err)
			return AuthVerdict{Code: AuthCodeNodeFault}
		}

		req, err := http.NewRequest(http.MethodPost, remoteURL, bytes.NewReader(body))
		if err != nil {
			slog.Warn("DIAG: auth-node-misconfigured", "reason", "bad_remote_url", "url", remoteURL, "err", err)
			return AuthVerdict{Code: AuthCodeNodeFault}
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(nodeIPv4, nodeSecret)

		resp, err := remoteAuthClient.Do(req)
		if err != nil {
			slog.Warn("wire: remote auth: request failed", "url", remoteURL, "err", err)
			return AuthVerdict{Code: -1}
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			slog.Warn("wire: remote auth: server error", "url", remoteURL, "status", resp.StatusCode)
			return AuthVerdict{Code: -1}
		}

		var result struct {
			Code int `json:"code"`
			Data struct {
				UserID           uint64 `json:"userID"`
				UDID             string `json:"udid"`
				ServiceExpiredAt int64  `json:"serviceExpiredAt"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			slog.Warn("wire: remote auth: decode failed", "url", remoteURL, "err", err)
			return AuthVerdict{Code: -1}
		}

		switch result.Code {
		case 0:
			return AuthVerdict{OK: true, UserID: result.Data.UserID, SvcExp: result.Data.ServiceExpiredAt}
		case 401, 402, 403:
			slog.Debug("wire: remote auth: rejected", "udid", udid, "code", result.Code)
			return AuthVerdict{Code: result.Code}
		default:
			// Center answered but not with a verdict about this device (422
			// malformed request, 500 system error, …) — cannot determine.
			slog.Warn("wire: remote auth: unexpected code", "udid", udid, "code", result.Code)
			return AuthVerdict{Code: -1}
		}
	}
}
