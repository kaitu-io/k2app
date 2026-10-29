//go:build linux

package webui

import (
	"encoding/json"
	"net/http"
)

// maxRequestBody is the upper bound on any webui HTTP POST body.
const maxRequestBody = 1 << 20 // 1 MB

// response is the shared JSON envelope for webui HTTP responses.
// Mirrors the gateway/daemon Response envelope (code/message/data).
type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
