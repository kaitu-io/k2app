package wire

import (
	"encoding/base64"
	"os"
	"path/filepath"
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
	hello := buildTestClientHello("example.com", false)

	sni, err := PeekClientHelloSNI(hello)
	if err != nil {
		t.Fatalf("PeekClientHelloSNI failed: %v", err)
	}
	if sni != "example.com" {
		t.Errorf("SNI = %q, want %q", sni, "example.com")
	}
}

func TestPeekClientHello_WithECH(t *testing.T) {
	// AC1: ClientHello with 0xfe0d extension → hasECH=true
	hello := buildTestClientHello("microsoft.com", true)

	info, err := PeekClientHello(hello)
	if err != nil {
		t.Fatalf("PeekClientHello failed: %v", err)
	}
	if info.SNI != "microsoft.com" {
		t.Errorf("SNI = %q, want %q", info.SNI, "microsoft.com")
	}
	if !info.HasECH {
		t.Error("HasECH = false, want true")
	}
}

func TestPeekClientHello_WithoutECH(t *testing.T) {
	// AC2: ClientHello without ECH extension → hasECH=false
	hello := buildTestClientHello("example.org", false)

	info, err := PeekClientHello(hello)
	if err != nil {
		t.Fatalf("PeekClientHello failed: %v", err)
	}
	if info.SNI != "example.org" {
		t.Errorf("SNI = %q, want %q", info.SNI, "example.org")
	}
	if info.HasECH {
		t.Error("HasECH = true, want false")
	}
}

func TestPeekClientHello_BackwardCompat(t *testing.T) {
	// Verify old PeekClientHelloSNI still works (as wrapper)
	hello := buildTestClientHello("test.com", false)

	sni, err := PeekClientHelloSNI(hello)
	if err != nil {
		t.Fatalf("PeekClientHelloSNI failed: %v", err)
	}
	if sni != "test.com" {
		t.Errorf("SNI = %q, want %q", sni, "test.com")
	}

	// Should also work with ECH present (SNI still extracted)
	helloWithECH := buildTestClientHello("ech.test.com", true)
	sni2, err := PeekClientHelloSNI(helloWithECH)
	if err != nil {
		t.Fatalf("PeekClientHelloSNI with ECH failed: %v", err)
	}
	if sni2 != "ech.test.com" {
		t.Errorf("SNI = %q, want %q", sni2, "ech.test.com")
	}
}

// buildTestClientHello constructs a minimal TLS 1.3 ClientHello with the given SNI.
// If includeECH is true, adds a placeholder ECH extension (0xfe0d).
func buildTestClientHello(sni string, includeECH bool) []byte {
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

	// All extensions
	allExts := sniExt

	// ECH extension (if requested)
	if includeECH {
		echExt := make([]byte, 0, 20)
		// Extension type: encrypted_client_hello (0xfe0d)
		echExt = append(echExt, 0xfe, 0x0d)
		// Extension length (placeholder 10 bytes of data)
		echExt = append(echExt, 0x00, 0x0a)
		// Dummy ECH payload
		echExt = append(echExt, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09)
		allExts = append(allExts, echExt...)
	}

	extsLen := len(allExts)

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
	body = append(body, allExts...)

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

func TestGenerateECHKey(t *testing.T) {
	// AC3: Generate ECH key pair
	key, echConfig, err := GenerateECHKey("www.microsoft.com")
	if err != nil {
		t.Fatalf("GenerateECHKey failed: %v", err)
	}
	if len(key.PrivateKey) == 0 {
		t.Error("PrivateKey is empty")
	}
	if len(key.Config) == 0 {
		t.Error("Config is empty")
	}
	if len(echConfig) == 0 {
		t.Error("echConfig bytes are empty")
	}
}

func TestSaveLoadECHKey(t *testing.T) {
	// AC4: Save and load ECH keys
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "ech.key")

	// Generate
	origKey, origConfig, err := GenerateECHKey("public.example.com")
	if err != nil {
		t.Fatalf("GenerateECHKey failed: %v", err)
	}

	// Save
	if err := SaveECHKey(keyPath, origKey.PrivateKey, origConfig); err != nil {
		t.Fatalf("SaveECHKey failed: %v", err)
	}

	// Load
	loadedKey, loadedConfig, err := LoadECHKey(keyPath)
	if err != nil {
		t.Fatalf("LoadECHKey failed: %v", err)
	}

	// Verify round-trip
	if string(loadedKey.PrivateKey) != string(origKey.PrivateKey) {
		t.Error("PrivateKey mismatch after round-trip")
	}
	if string(loadedKey.Config) != string(origKey.Config) {
		t.Error("Config mismatch after round-trip")
	}
	if string(loadedConfig) != string(origConfig) {
		t.Error("echConfig mismatch after round-trip")
	}
}

func TestEnsureECHKey_AutoGenerate(t *testing.T) {
	// AC3: Path doesn't exist → auto-generates
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "new-ech.key")

	keys, echConfig, err := EnsureECHKey(keyPath, "fronting.example.com")
	if err != nil {
		t.Fatalf("EnsureECHKey (auto-generate) failed: %v", err)
	}
	if len(keys) == 0 {
		t.Error("keys slice is empty")
	}
	if len(echConfig) == 0 {
		t.Error("echConfig is empty")
	}

	// Verify file was created
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("key file was not created")
	}
}

func TestEnsureECHKey_LoadExisting(t *testing.T) {
	// AC4: Path exists → loads
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "existing-ech.key")

	// First call: generate and save
	keys1, echConfig1, err := EnsureECHKey(keyPath, "cdn.example.com")
	if err != nil {
		t.Fatalf("EnsureECHKey (first call) failed: %v", err)
	}

	// Second call: should load existing
	keys2, echConfig2, err := EnsureECHKey(keyPath, "cdn.example.com")
	if err != nil {
		t.Fatalf("EnsureECHKey (second call) failed: %v", err)
	}

	// Verify same keys loaded
	if string(keys1[0].PrivateKey) != string(keys2[0].PrivateKey) {
		t.Error("PrivateKey mismatch between first and second call")
	}
	if string(echConfig1) != string(echConfig2) {
		t.Error("echConfig mismatch between first and second call")
	}
}
