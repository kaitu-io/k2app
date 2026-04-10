//go:build linux

package webui

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/kaitu-io/k2/safego"
)

// NewUpgradeHandler returns an http.HandlerFunc for the /api/upgrade endpoint.
//
// Request body: {"action": "check"|"apply"}
//
// action "check" — calls Upgrader.FetchLatest() and returns {current, latest}.
// action "apply" — spawns Upgrader.Apply() in a background goroutine and
//                  returns immediately with status "upgrading". The running
//                  process is expected to be killed by RestartFunc.
func NewUpgradeHandler(upgrader *Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, response{Code: 1, Message: "bad request"})
			return
		}

		switch req.Action {
		case "check":
			info, err := upgrader.FetchLatest()
			if err != nil {
				writeJSON(w, response{Code: 1, Message: "check failed: " + err.Error()})
				return
			}
			writeJSON(w, response{Code: 0, Data: map[string]string{
				"current": upgrader.CurrentVersion(),
				"latest":  info.Version,
			}})

		case "apply":
			safego.Go(func() {
				if err := upgrader.Apply(); err != nil {
					slog.Warn("webui: upgrade failed", "err", err)
				}
			})
			writeJSON(w, response{Code: 0, Message: "upgrading", Data: map[string]string{
				"status": "upgrading",
			}})

		default:
			writeJSON(w, response{Code: 1, Message: "unknown action (use check or apply)"})
		}
	}
}
