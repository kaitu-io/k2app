//go:build linux

package webui

import "net/http"

// PlatformInfo is the JSON body returned by the /api/platform endpoint.
// Callers fill in the fields relevant to their deployment (e.g. gateway sets
// PlatformType="gateway", desktop sets PlatformType="desktop").
type PlatformInfo struct {
	OS           string `json:"os"`
	PlatformType string `json:"platformType"`
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Arch         string `json:"arch"`
}

// NewPlatformHandler returns an http.HandlerFunc that responds with the given
// PlatformInfo. If info.OS is empty it is defaulted to "linux".
func NewPlatformHandler(info PlatformInfo) http.HandlerFunc {
	if info.OS == "" {
		info.OS = "linux"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, response{Code: 0, Data: info})
	}
}
