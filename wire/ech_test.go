package wire

import (
	"encoding/base64"
	"testing"

	utls "github.com/metacubex/utls"
)

func TestParseFingerprint(t *testing.T) {
	tests := []struct {
		fp   string
		want utls.ClientHelloID
	}{
		{"chrome", utls.HelloChrome_120_PQ},
		{"firefox", utls.HelloFirefox_120},
		{"safari", utls.HelloSafari_16_0},
		{"random", utls.HelloRandomizedALPN},
		{"", utls.HelloChrome_120_PQ}, // default
		{"unknown", utls.HelloChrome_120_PQ}, // fallback to default
	}

	for _, tt := range tests {
		t.Run(tt.fp, func(t *testing.T) {
			got := ParseFingerprint(tt.fp)
			if got != tt.want {
				t.Errorf("ParseFingerprint(%q) = %v, want %v", tt.fp, got, tt.want)
			}
		})
	}
}

func TestParseECHConfig(t *testing.T) {
	// Valid base64 data (simulated ECH config bytes)
	raw := []byte{0xfe, 0x0d, 0x00, 0x10, 0x01, 0x02, 0x03}
	encoded := base64.StdEncoding.EncodeToString(raw)

	got, err := ParseECHConfig(encoded)
	if err != nil {
		t.Fatalf("ParseECHConfig failed: %v", err)
	}
	if len(got) != len(raw) {
		t.Errorf("len = %d, want %d", len(got), len(raw))
	}

	// URL-safe base64 should also work
	encodedURL := base64.URLEncoding.EncodeToString(raw)
	got2, err := ParseECHConfig(encodedURL)
	if err != nil {
		t.Fatalf("ParseECHConfig (URL-safe) failed: %v", err)
	}
	if len(got2) != len(raw) {
		t.Errorf("URL-safe: len = %d, want %d", len(got2), len(raw))
	}

	// Empty string → nil, no error
	got3, err := ParseECHConfig("")
	if err != nil {
		t.Fatalf("empty string should not error: %v", err)
	}
	if got3 != nil {
		t.Error("empty string should return nil")
	}

	// Invalid base64 → error
	_, err = ParseECHConfig("!!!not-base64!!!")
	if err == nil {
		t.Error("invalid base64 should return error")
	}
}

func TestBuildClientTLSConfig(t *testing.T) {
	echRaw := []byte{0xfe, 0x0d, 0x00, 0x10}
	cfg := Config{
		Host:        "real.server.com",
		Port:        443,
		SNI:         "www.microsoft.com",
		ECH:         base64.StdEncoding.EncodeToString(echRaw),
		Fingerprint: "chrome",
	}

	tlsCfg, clientHelloID, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildClientTLSConfig failed: %v", err)
	}

	// Inner SNI should be the real server
	if tlsCfg.ServerName != "real.server.com" {
		t.Errorf("ServerName = %q, want %q", tlsCfg.ServerName, "real.server.com")
	}

	// ECH config should be set
	if len(tlsCfg.EncryptedClientHelloConfigList) == 0 {
		t.Error("EncryptedClientHelloConfigList should be set")
	}

	// Fingerprint
	if clientHelloID != utls.HelloChrome_120_PQ {
		t.Errorf("clientHelloID = %v, want HelloChrome_120_PQ", clientHelloID)
	}

	// Without ECH → error (k2v5 mandates ECH)
	cfgNoECH := Config{
		Host:        "real.server.com",
		Port:        443,
		SNI:         "www.microsoft.com",
		Fingerprint: "chrome",
	}
	_, _, err = BuildClientTLSConfig(cfgNoECH)
	if err == nil {
		t.Error("should error when ECH is not configured (k2v5 mandates ECH)")
	}
}

func TestPeekClientHelloSNI(t *testing.T) {
	// Construct a minimal TLS ClientHello with SNI "example.com"
	hello := buildTestClientHello("example.com")

	sni, err := PeekClientHelloSNI(hello)
	if err != nil {
		t.Fatalf("PeekClientHelloSNI failed: %v", err)
	}
	if sni != "example.com" {
		t.Errorf("SNI = %q, want %q", sni, "example.com")
	}
}

// buildTestClientHello constructs a minimal TLS 1.3 ClientHello with the given SNI.
func buildTestClientHello(sni string) []byte {
	// SNI extension
	sniBytes := []byte(sni)
	sniExt := make([]byte, 0, 9+len(sniBytes))
	// Extension type: server_name (0x0000)
	sniExt = append(sniExt, 0x00, 0x00)
	sniExtLen := 5 + len(sniBytes)
	sniExt = append(sniExt, byte(sniExtLen>>8), byte(sniExtLen))
	// Server name list length
	sniListLen := 3 + len(sniBytes)
	sniExt = append(sniExt, byte(sniListLen>>8), byte(sniListLen))
	// Host name type (0x00)
	sniExt = append(sniExt, 0x00)
	// Host name length
	sniExt = append(sniExt, byte(len(sniBytes)>>8), byte(len(sniBytes)))
	sniExt = append(sniExt, sniBytes...)

	// Extensions total
	extsLen := len(sniExt)

	// ClientHello body
	body := make([]byte, 0, 256)
	// Client version: TLS 1.2 (0x0303)
	body = append(body, 0x03, 0x03)
	// Random (32 bytes)
	body = append(body, make([]byte, 32)...)
	// Session ID length (0)
	body = append(body, 0x00)
	// Cipher suites length (2) + TLS_AES_128_GCM_SHA256
	body = append(body, 0x00, 0x02, 0x13, 0x01)
	// Compression methods (1 byte: null)
	body = append(body, 0x01, 0x00)
	// Extensions length
	body = append(body, byte(extsLen>>8), byte(extsLen))
	body = append(body, sniExt...)

	// Handshake header: type ClientHello (1) + length
	handshake := make([]byte, 0, 4+len(body))
	handshake = append(handshake, 0x01)
	handshake = append(handshake, 0x00, byte(len(body)>>8), byte(len(body)))
	handshake = append(handshake, body...)

	// TLS record header: ContentType Handshake (22) + version TLS 1.0 + length
	record := make([]byte, 0, 5+len(handshake))
	record = append(record, 0x16, 0x03, 0x01)
	record = append(record, byte(len(handshake)>>8), byte(len(handshake)))
	record = append(record, handshake...)

	return record
}
