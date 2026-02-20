package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

var ipHTTPClient = &http.Client{Timeout: 30 * time.Second}

// testIPServiceURLs is used in tests to inject mock server URLs
// Format: ["ipify_url", "ipinfo_url", "ipwhois_url"]
var testIPServiceURLs []string

// testHTTPClient is used in tests to inject mock HTTP client
var testHTTPClient *http.Client

// createIPHTTPClient creates an HTTP client for a specific network type (internal use)
func createIPHTTPClient(ipVersion string) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Force specific network type based on IP version
			if ipVersion == "ipv4" {
				return dialer.DialContext(ctx, "tcp4", addr)
			} else if ipVersion == "ipv6" {
				return dialer.DialContext(ctx, "tcp6", addr)
			}
			return dialer.DialContext(ctx, network, addr)
		},
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

func fetchJSON(url string, target interface{}) error {
	resp, err := ipHTTPClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func fetchIPJSONWithClient(client *http.Client, url string, target interface{}) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

// GetExternalIP gets IP information (exported interface)
func GetExternalIP(ipVersion string) (IPData, error) {
	ip, data, err := GetExternalIPWithData(ipVersion)
	if err != nil {
		return IPData{}, err
	}
	data.IP = ip
	return data, nil
}

// GetExternalIPWithData gets IP information (exported interface)
// ipVersion: "ipv4" or "ipv6"
// Returns: IP address string, IP detailed info, error
func GetExternalIPWithData(ipVersion string) (string, IPData, error) {
	if ipVersion != "ipv4" && ipVersion != "ipv6" {
		return "", IPData{}, fmt.Errorf("unknown ip version: %s (must be 'ipv4' or 'ipv6')", ipVersion)
	}

	// Create dedicated HTTP client (forces tcp4 or tcp6)
	// If test client is set, use it preferentially
	var client *http.Client
	if testHTTPClient != nil {
		client = testHTTPClient
	} else {
		client = createIPHTTPClient(ipVersion)
	}

	// Use general IP detection services (returns the corresponding IP version via different network layers)
	// If test URLs are set, use them preferentially
	var urls []string
	if len(testIPServiceURLs) > 0 {
		urls = testIPServiceURLs
	} else {
		urls = []string{
			"https://api64.ipify.org?format=json", // Supports IPv4 and IPv6
			"https://ipinfo.io/json",              // Supports IPv4 and IPv6
			"https://ipwhois.app/json/",           // Supports IPv4 and IPv6
		}
	}

	var lastErr error
	for _, url := range urls {
		var result IPData
		var err error

		switch {
		case strings.Contains(url, "ipify.org"):
			// ipify only returns IP address
			var data struct {
				IP string `json:"ip"`
			}
			err = fetchIPJSONWithClient(client, url, &data)
			if err == nil && data.IP != "" {
				// Validate returned IP type matches
				if !isIPVersionMatch(data.IP, ipVersion) {
					lastErr = fmt.Errorf("ipify returned wrong IP version: got %s, want %s", detectIPVersion(data.IP), ipVersion)
					continue
				}

				// Try to get country code
				countryCode := ""
				location := ""
				var ipInfo IPInfo
				infoURL := "https://ipinfo.io/" + data.IP + "/json"
				if fetchIPJSONWithClient(client, infoURL, &ipInfo) == nil {
					countryCode = ipInfo.Country
					location = firstNonEmpty(ipInfo.City, ipInfo.Region)
				}

				// If unable to get country code, skip this service and try next
				if countryCode == "" {
					lastErr = fmt.Errorf("ipify+ipinfo failed to get country code for IP: %s", data.IP)
					continue
				}

				result = IPData{
					IP:          data.IP,
					CountryCode: countryCode,
					Location:    location,
				}
			}

		case strings.Contains(url, "ipinfo.io"):
			var data IPInfo
			err = fetchIPJSONWithClient(client, url, &data)
			if err == nil && data.IP != "" {
				// Validate returned IP type matches
				if !isIPVersionMatch(data.IP, ipVersion) {
					lastErr = fmt.Errorf("ipinfo.io returned wrong IP version: got %s, want %s", detectIPVersion(data.IP), ipVersion)
					continue
				}

				// Validate must contain country code
				if data.Country == "" {
					lastErr = fmt.Errorf("ipinfo.io failed to get country code for IP: %s", data.IP)
					continue
				}

				result = IPData{
					IP:          data.IP,
					Location:    firstNonEmpty(data.City, data.Region),
					CountryCode: data.Country,
				}
			}

		case strings.Contains(url, "ipwhois.app"):
			var data IPWhois
			err = fetchIPJSONWithClient(client, url, &data)
			if err == nil && data.Success && data.IP != "" {
				// Validate returned IP type matches
				if !isIPVersionMatch(data.IP, ipVersion) {
					lastErr = fmt.Errorf("ipwhois.app returned wrong IP version: got %s, want %s", detectIPVersion(data.IP), ipVersion)
					continue
				}

				// Validate must contain country code
				if data.CountryCode == "" {
					lastErr = fmt.Errorf("ipwhois.app failed to get country code for IP: %s", data.IP)
					continue
				}

				result = IPData{
					IP:          data.IP,
					Location:    firstNonEmpty(data.City, data.Region),
					CountryCode: data.CountryCode,
				}
			}
		}

		if err != nil {
			lastErr = err
			continue
		}

		// Successfully got correct version IP
		if result.IP != "" {
			return result.IP, result, nil
		}
	}

	if lastErr != nil {
		return "", IPData{}, fmt.Errorf("all %s services failed: %w", ipVersion, lastErr)
	}
	return "", IPData{}, fmt.Errorf("all %s services failed", ipVersion)
}

// detectIPVersion detects the version of an IP address
func detectIPVersion(ip string) string {
	// Empty string is invalid
	if ip == "" {
		return "invalid"
	}

	// Use net.ParseIP to validate the IP address
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "invalid"
	}

	// Check if IPv4 or IPv6
	if parsedIP.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

