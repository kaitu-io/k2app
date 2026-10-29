package k2p

import (
	"net/url"
	"testing"
)

func TestValidateUDID(t *testing.T) {
	valid := []string{
		"home-abc",
		"abc",
		"a-b",
		"home-node-01",
		"aa1",
		"a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p",
	}
	for _, u := range valid {
		if err := ValidateUDID(u); err != nil {
			t.Errorf("ValidateUDID(%q) unexpected error: %v", u, err)
		}
	}

	invalid := []string{
		"",          // empty
		"a",         // too short (only 1 char, need at least 3 for [a-z0-9] + {1,30} + [a-z0-9])
		"HOME-ABC",  // uppercase
		"-abc",      // starts with dash
		"abc-",      // ends with dash
		"ab.cd",     // dot
		"ab_cd",     // underscore
		"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p-q-r-s-t-u-v-w-x-y-z-0-1-2-3-4-5-6-7-8-9-extra", // too long
	}
	for _, u := range invalid {
		if err := ValidateUDID(u); err == nil {
			t.Errorf("ValidateUDID(%q) expected error, got nil", u)
		}
	}
}

func TestParseURL_Valid(t *testing.T) {
	raw := "k2p://my-home:s3cr3t@my-home.signal.example.com:8443?pin=sha256:abc&ip=1.2.3.4&ip6=::1&ech=AAAA&fp=firefox"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL(%q) error: %v", raw, err)
	}

	if cfg.HomeUDID != "my-home" {
		t.Errorf("HomeUDID = %q, want %q", cfg.HomeUDID, "my-home")
	}
	if cfg.PeerSecret != "s3cr3t" {
		t.Errorf("PeerSecret = %q, want %q", cfg.PeerSecret, "s3cr3t")
	}
	if cfg.Pin != "sha256:abc" {
		t.Errorf("Pin = %q, want %q", cfg.Pin, "sha256:abc")
	}
	if cfg.SignalServer != "https://signal.example.com:8443" {
		t.Errorf("SignalServer = %q, want %q", cfg.SignalServer, "https://signal.example.com:8443")
	}
	if cfg.WireCfg.Pin != "sha256:abc" {
		t.Errorf("WireCfg.Pin = %q, want %q", cfg.WireCfg.Pin, "sha256:abc")
	}
	if cfg.WireCfg.IPv4 != "1.2.3.4" {
		t.Errorf("WireCfg.IPv4 = %q, want %q", cfg.WireCfg.IPv4, "1.2.3.4")
	}
	if cfg.WireCfg.IPv6 != "::1" {
		t.Errorf("WireCfg.IPv6 = %q, want %q", cfg.WireCfg.IPv6, "::1")
	}
	if cfg.WireCfg.ECH != "AAAA" {
		t.Errorf("WireCfg.ECH = %q, want %q", cfg.WireCfg.ECH, "AAAA")
	}
	if cfg.WireCfg.Fingerprint != "firefox" {
		t.Errorf("WireCfg.Fingerprint = %q, want %q", cfg.WireCfg.Fingerprint, "firefox")
	}
	if cfg.WireCfg.Host != "signal.example.com" {
		t.Errorf("WireCfg.Host = %q, want %q", cfg.WireCfg.Host, "signal.example.com")
	}
}

func TestParseURL_DefaultPort443(t *testing.T) {
	// Port 443 should be default when omitted — but k2p URL requires explicit port in host
	// Test that port 443 works explicitly
	raw := "k2p://my-home:secret@my-home.signal.example.com:443"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL(%q) error: %v", raw, err)
	}
	if cfg.SignalServer != "https://signal.example.com:443" {
		t.Errorf("SignalServer = %q, want %q", cfg.SignalServer, "https://signal.example.com:443")
	}
}

func TestParseURL_DefaultFingerprint(t *testing.T) {
	raw := "k2p://my-home:secret@my-home.signal.example.com:443"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL(%q) error: %v", raw, err)
	}
	if cfg.WireCfg.Fingerprint != "chrome" {
		t.Errorf("WireCfg.Fingerprint = %q, want %q", cfg.WireCfg.Fingerprint, "chrome")
	}
}

func TestParseURL_InvalidScheme(t *testing.T) {
	_, err := ParseURL("k2v5://my-home:secret@my-home.signal.example.com:443")
	if err == nil {
		t.Fatal("expected error for k2v5 scheme, got nil")
	}
}

func TestParseURL_MissingSecret(t *testing.T) {
	_, err := ParseURL("k2p://my-home@my-home.signal.example.com:443")
	if err == nil {
		t.Fatal("expected error for missing secret, got nil")
	}
}

func TestParseURL_InvalidUDID(t *testing.T) {
	_, err := ParseURL("k2p://MY-HOME:secret@MY-HOME.signal.example.com:443")
	if err == nil {
		t.Fatal("expected error for uppercase UDID, got nil")
	}
}

func TestParseURL_MissingPort(t *testing.T) {
	// Host without port should fail because we need explicit port
	_, err := ParseURL("k2p://my-home:secret@my-home.signal.example.com")
	if err == nil {
		t.Fatal("expected error for missing port, got nil")
	}
}

func TestParseURL_SingleLabelHost(t *testing.T) {
	// Host must have at least 2 labels (udid.domain)
	_, err := ParseURL("k2p://my-home:secret@my-home:443")
	if err == nil {
		t.Fatal("expected error for single-label host, got nil")
	}
}

func TestBuildURL(t *testing.T) {
	extra := url.Values{}
	extra.Set("ech", "BBBB")
	extra.Set("ip", "5.6.7.8")

	built := BuildURL("my-home", "s3cr3t", "signal.example.com", 8443, "sha256:xyz", extra)

	cfg, err := ParseURL(built)
	if err != nil {
		t.Fatalf("ParseURL(BuildURL(...)) error: %v", err)
	}

	if cfg.HomeUDID != "my-home" {
		t.Errorf("HomeUDID = %q, want %q", cfg.HomeUDID, "my-home")
	}
	if cfg.SignalServer != "https://signal.example.com:8443" {
		t.Errorf("SignalServer = %q, want %q", cfg.SignalServer, "https://signal.example.com:8443")
	}
	if cfg.WireCfg.Pin != "sha256:xyz" {
		t.Errorf("WireCfg.Pin = %q, want %q", cfg.WireCfg.Pin, "sha256:xyz")
	}
	if cfg.WireCfg.ECH != "BBBB" {
		t.Errorf("WireCfg.ECH = %q, want %q", cfg.WireCfg.ECH, "BBBB")
	}
	if cfg.WireCfg.IPv4 != "5.6.7.8" {
		t.Errorf("WireCfg.IPv4 = %q, want %q", cfg.WireCfg.IPv4, "5.6.7.8")
	}
}

func TestBuildURL_NoPin(t *testing.T) {
	built := BuildURL("my-home", "s3cr3t", "signal.example.com", 443, "", nil)
	cfg, err := ParseURL(built)
	if err != nil {
		t.Fatalf("ParseURL(BuildURL(...)) error: %v", err)
	}
	if cfg.WireCfg.Pin != "" {
		t.Errorf("WireCfg.Pin = %q, want empty", cfg.WireCfg.Pin)
	}
}

// TestParseURL_RoundTrip verifies that ParseURL(BuildURL(...)) preserves
// PeerSecret, Pin, and HomeUDID exactly.
func TestParseURL_RoundTrip(t *testing.T) {
	extra := url.Values{}
	extra.Set("ip", "1.2.3.4")

	built := BuildURL("test-home", "my-secret", "signal.example.com", 443, "sha256:AAAA", extra)
	cfg, err := ParseURL(built)
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if cfg.PeerSecret != "my-secret" {
		t.Errorf("PeerSecret = %q, want my-secret", cfg.PeerSecret)
	}
	if cfg.Pin != "sha256:AAAA" {
		t.Errorf("Pin = %q, want sha256:AAAA", cfg.Pin)
	}
	if cfg.HomeUDID != "test-home" {
		t.Errorf("HomeUDID = %q, want test-home", cfg.HomeUDID)
	}
}
