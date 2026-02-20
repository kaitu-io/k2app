package sidecar

import (
	"fmt"
	"log"
	"net/url"
	"strings"
)

// BuildServerURL constructs a complete k2v5 connection URL from connect-url.txt content
// and sidecar config. Strips auth credentials and dev flags, uses configured domain/port,
// appends hop range if configured.
//
// Input: k2v5://udid:token@host:port?ech=base64url&pin=sha256:base64&insecure=1
// Output: k2v5://domain:port?ech=base64url&pin=sha256:base64[&hop=start-end]
//
// Returns empty string if connectURLContent is empty or unparseable.
func BuildServerURL(connectURLContent, domain string, port, hopStart, hopEnd int) string {
	if connectURLContent == "" {
		return ""
	}
	u, err := url.Parse(connectURLContent)
	if err != nil {
		log.Printf("[Sidecar] Warning: failed to parse connect URL: %v", err)
		return ""
	}

	// Extract ech and pin from source URL
	ech := u.Query().Get("ech")
	pin := u.Query().Get("pin")
	if ech == "" && pin == "" {
		return ""
	}

	// Build clean URL with configured domain/port
	var params []string
	if ech != "" {
		params = append(params, "ech="+ech)
	}
	if pin != "" {
		params = append(params, "pin="+pin)
	}
	if hopStart > 0 && hopEnd > 0 {
		params = append(params, fmt.Sprintf("hop=%d-%d", hopStart, hopEnd))
	}

	result := fmt.Sprintf("k2v5://%s:%d", domain, port)
	if len(params) > 0 {
		result += "?" + strings.Join(params, "&")
	}
	return result
}
