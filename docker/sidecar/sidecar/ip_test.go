package sidecar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupMockIPServer creates a mock IP server for testing
// Returns: mock server, cleanup function
func setupMockIPServer(t *testing.T, mockIP string, mockCountry string, mockLocation string) (*httptest.Server, func()) {
	t.Helper()

	// Create ipwhois-style mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := IPWhois{
			IP:          mockIP,
			CountryCode: mockCountry,
			City:        mockLocation,
			Region:      mockLocation,
			Success:     true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))

	// Save original values
	origURLs := testIPServiceURLs
	origClient := testHTTPClient

	// Set test URL and client
	// URL must contain "ipwhois.app" to match URL judgment logic in GetExternalIPWithData
	testIPServiceURLs = []string{server.URL + "/ipwhois.app/json/"}
	testHTTPClient = server.Client()

	cleanup := func() {
		server.Close()
		testIPServiceURLs = origURLs
		testHTTPClient = origClient
	}

	return server, cleanup
}

// TestGetExternalIPv4 tests getting IPv4 address (using mock server)
func TestGetExternalIPv4(t *testing.T) {
	t.Log("Testing get IPv4 address (using mock server)...")

	// Set up mock server
	mockIP := "203.0.113.42"
	mockCountry := "US"
	mockLocation := "San Francisco"
	_, cleanup := setupMockIPServer(t, mockIP, mockCountry, mockLocation)
	defer cleanup()

	ipData, err := GetExternalIP("ipv4")
	if err != nil {
		t.Fatalf("Failed to get IPv4: %v", err)
	}

	if ipData.IP == "" {
		t.Fatal("IPv4 address is empty")
	}

	if ipData.IP != mockIP {
		t.Errorf("IP mismatch: expected %s, got %s", mockIP, ipData.IP)
	}

	if ipData.CountryCode != mockCountry {
		t.Errorf("Country code mismatch: expected %s, got %s", mockCountry, ipData.CountryCode)
	}

	if ipData.Location != mockLocation {
		t.Errorf("Location mismatch: expected %s, got %s", mockLocation, ipData.Location)
	}

	t.Logf("Got IPv4: %s", ipData.IP)
	t.Logf("Country code: %s", ipData.CountryCode)
	t.Logf("Location: %s", ipData.Location)

	// Validate is IPv4 format (simple check: contains dot)
	if !containsChar(ipData.IP, '.') {
		t.Errorf("Returned address is not IPv4: %s", ipData.IP)
	}

	// IPv4 should not contain colon
	if containsChar(ipData.IP, ':') {
		t.Errorf("IPv4 address should not contain colon: %s", ipData.IP)
	}
}

// TestGetExternalIPv6 tests getting IPv6 address (using mock server)
func TestGetExternalIPv6(t *testing.T) {
	t.Log("Testing get IPv6 address (using mock server)...")

	// Set up mock server
	mockIP := "2001:db8::1"
	mockCountry := "DE"
	mockLocation := "Frankfurt"
	_, cleanup := setupMockIPServer(t, mockIP, mockCountry, mockLocation)
	defer cleanup()

	ipData, err := GetExternalIP("ipv6")
	if err != nil {
		t.Fatalf("Failed to get IPv6: %v", err)
	}

	if ipData.IP == "" {
		t.Fatal("IPv6 address is empty")
	}

	if ipData.IP != mockIP {
		t.Errorf("IP mismatch: expected %s, got %s", mockIP, ipData.IP)
	}

	if ipData.CountryCode != mockCountry {
		t.Errorf("Country code mismatch: expected %s, got %s", mockCountry, ipData.CountryCode)
	}

	t.Logf("Got IPv6: %s", ipData.IP)
	t.Logf("Country code: %s", ipData.CountryCode)
	t.Logf("Location: %s", ipData.Location)

	// Validate is IPv6 format (simple check: contains colon)
	if !containsChar(ipData.IP, ':') {
		t.Errorf("Returned address is not IPv6: %s", ipData.IP)
	}
}

// TestGetExternalIPInvalidVersion tests invalid IP version parameter
func TestGetExternalIPInvalidVersion(t *testing.T) {
	t.Log("Testing invalid IP version parameter...")

	_, err := GetExternalIP("invalid")
	if err == nil {
		t.Fatal("Should return error, but didn't")
	}

	t.Logf("Correctly returned error: %v", err)
}

// TestIPDataStructure tests IPData struct
func TestIPDataStructure(t *testing.T) {
	t.Log("Testing IPData struct...")

	ipData := IPData{
		IP:          "1.2.3.4",
		Location:    "Test City",
		CountryCode: "US",
	}

	if ipData.IP != "1.2.3.4" {
		t.Errorf("IP field mismatch: expected 1.2.3.4, got %s", ipData.IP)
	}

	if ipData.Location != "Test City" {
		t.Errorf("Location field mismatch: expected Test City, got %s", ipData.Location)
	}

	if ipData.CountryCode != "US" {
		t.Errorf("CountryCode field mismatch: expected US, got %s", ipData.CountryCode)
	}
}

// TestFirstNonEmpty tests firstNonEmpty helper function
func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		expected string
	}{
		{
			name:     "first non-empty",
			inputs:   []string{"first", "second", "third"},
			expected: "first",
		},
		{
			name:     "skip empty strings",
			inputs:   []string{"", "second", "third"},
			expected: "second",
		},
		{
			name:     "all empty",
			inputs:   []string{"", "", ""},
			expected: "",
		},
		{
			name:     "empty array",
			inputs:   []string{},
			expected: "",
		},
		{
			name:     "last non-empty",
			inputs:   []string{"", "", "last"},
			expected: "last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := firstNonEmpty(tt.inputs...)
			if result != tt.expected {
				t.Errorf("firstNonEmpty(%v) = %s; expected %s", tt.inputs, result, tt.expected)
			}
		})
	}
}

// TestIsIPVersionMatch tests IP version matching
func TestIsIPVersionMatch(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		version  string
		expected bool
	}{
		{"IPv4 match", "192.168.1.1", "ipv4", true},
		{"IPv4 no match", "192.168.1.1", "ipv6", false},
		{"IPv6 match", "2001:db8::1", "ipv6", true},
		{"IPv6 no match", "2001:db8::1", "ipv4", false},
		{"invalid IP", "invalid-ip", "ipv4", false},
		{"empty IP", "", "ipv4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIPVersionMatch(tt.ip, tt.version)
			if result != tt.expected {
				t.Errorf("isIPVersionMatch(%s, %s) = %v; expected %v", tt.ip, tt.version, result, tt.expected)
			}
		})
	}
}

// TestDetectIPVersion tests IP version detection
func TestDetectIPVersion(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{"detect IPv4", "192.168.1.1", "ipv4"},
		{"detect IPv4 public", "8.8.8.8", "ipv4"},
		{"detect IPv6", "2001:db8::1", "ipv6"},
		{"detect IPv6 short", "::1", "ipv6"},
		{"invalid IP", "invalid-ip", "invalid"},
		{"empty IP", "", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectIPVersion(tt.ip)
			if result != tt.expected {
				t.Errorf("detectIPVersion(%s) = %s; expected %s", tt.ip, result, tt.expected)
			}
		})
	}
}

// TestConcurrentIPRequests tests concurrent requests (using mock server)
func TestConcurrentIPRequests(t *testing.T) {
	t.Log("Testing concurrent IP requests (using mock server)...")

	// Set up mock server
	mockIP := "203.0.113.100"
	mockCountry := "JP"
	mockLocation := "Tokyo"
	_, cleanup := setupMockIPServer(t, mockIP, mockCountry, mockLocation)
	defer cleanup()

	concurrency := 5
	results := make(chan IPData, concurrency)
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			ipData, err := GetExternalIP("ipv4")
			if err != nil {
				errors <- err
				return
			}
			results <- ipData
		}()
	}

	successCount := 0
	errorCount := 0

	for i := 0; i < concurrency; i++ {
		select {
		case ipData := <-results:
			successCount++
			t.Logf("Successfully got IP: %s", ipData.IP)
		case err := <-errors:
			errorCount++
			t.Logf("Failed: %v", err)
		}
	}

	t.Logf("Concurrent test complete: success %d, failure %d", successCount, errorCount)

	if successCount != concurrency {
		t.Errorf("Concurrent requests failed: expected %d success, got %d", concurrency, successCount)
	}
}

// containsChar helper function: checks if string contains a character
func containsChar(s string, ch rune) bool {
	for _, c := range s {
		if c == ch {
			return true
		}
	}
	return false
}

// TestIPDataWithLocation tests IPData with location info (using mock server)
func TestIPDataWithLocation(t *testing.T) {
	t.Log("Testing IPData location info formatting (using mock server)...")

	// Set up mock server
	mockIP := "198.51.100.50"
	mockCountry := "CN"
	mockLocation := "Shanghai"
	_, cleanup := setupMockIPServer(t, mockIP, mockCountry, mockLocation)
	defer cleanup()

	// Get IP info
	ipData, err := GetExternalIP("ipv4")
	if err != nil {
		t.Fatalf("Failed to get IP: %v", err)
	}

	t.Logf("IP: %s", ipData.IP)
	t.Logf("CountryCode: %s", ipData.CountryCode)
	t.Logf("Location: %s", ipData.Location)

	// Validate returned data
	if ipData.IP != mockIP {
		t.Errorf("IP mismatch: expected %s, got %s", mockIP, ipData.IP)
	}
	if ipData.CountryCode != mockCountry {
		t.Errorf("Country code mismatch: expected %s, got %s", mockCountry, ipData.CountryCode)
	}
	if ipData.Location != mockLocation {
		t.Errorf("Location mismatch: expected %s, got %s", mockLocation, ipData.Location)
	}

	// Test Region formatting
	region := ipData.CountryCode + "-" + ipData.Location
	t.Logf("Generated Region: %s", region)

	// Validate format contains hyphen
	if !containsChar(region, '-') {
		t.Errorf("Region format incorrect, should contain hyphen: %s", region)
	}

	// Validate not empty
	if region == "-" || region == "" {
		t.Errorf("Region should not be empty or just a hyphen: %s", region)
	}

	// Validate expected format
	expectedRegion := mockCountry + "-" + mockLocation
	if region != expectedRegion {
		t.Errorf("Region mismatch: expected %s, got %s", expectedRegion, region)
	}
}
