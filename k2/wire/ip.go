package wire

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ProbePublicIP makes an HTTP GET to the given URL and returns the plain-text
// IP address in the response body. Returns "" on any error.
func ProbePublicIP(url string) string {
	return ProbePublicIPWith(url, nil)
}

// ProbePublicIPWith is like ProbePublicIP but accepts an optional *http.Client.
// Pass a custom client to route the probe through a DirectDialer (bypassing TUN).
// If client is nil, a default client with 5s timeout is used.
func ProbePublicIPWith(url string, client *http.Client) string {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// FormatSslipHost formats an IP address as a sslip.io domain.
// IPv4: 1.2.3.4.sslip.io
// IPv6: 2001-db8--1.sslip.io (colons replaced with dashes)
func FormatSslipHost(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip + ".sslip.io"
	}
	if parsed.To4() != nil {
		return ip + ".sslip.io"
	}
	return strings.ReplaceAll(ip, ":", "-") + ".sslip.io"
}

