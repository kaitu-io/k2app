package sidecar

import (
	"log"
	"net/url"
)

// ParseConnectURL extracts certPin and echConfigList from a k2v5:// connect URL.
// The URL format from k2s: k2v5://udid:token@host:port?ech=base64url&pin=sha256:base64&insecure=1
// Returns empty strings on parse failure (non-fatal).
func ParseConnectURL(rawURL string) (certPin, echConfigList string) {
	if rawURL == "" {
		return "", ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		log.Printf("[Sidecar] Warning: failed to parse connect URL: %v", err)
		return "", ""
	}
	return u.Query().Get("pin"), u.Query().Get("ech")
}
