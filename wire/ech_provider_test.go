package wire

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// buildTestECHConfigList constructs a valid ECHConfigList with the given fields.
func buildTestECHConfigList(configID uint8, kemID uint16, pubKey []byte, cipherSuites []byte, maxNameLen uint8, publicName string) []byte {
	pubNameBytes := []byte(publicName)
	innerLen := 1 + 2 + 2 + len(pubKey) + 2 + len(cipherSuites) + 1 + 1 + len(pubNameBytes) + 2

	config := make([]byte, 0, 4+innerLen)
	// Version
	config = append(config, 0xfe, 0x0d)
	// Length
	config = append(config, byte(innerLen>>8), byte(innerLen))
	// Config ID
	config = append(config, configID)
	// KEM ID
	config = append(config, byte(kemID>>8), byte(kemID))
	// Public key
	config = append(config, byte(len(pubKey)>>8), byte(len(pubKey)))
	config = append(config, pubKey...)
	// Cipher suites
	config = append(config, byte(len(cipherSuites)>>8), byte(len(cipherSuites)))
	config = append(config, cipherSuites...)
	// Max name length
	config = append(config, maxNameLen)
	// Public name
	config = append(config, byte(len(pubNameBytes)))
	config = append(config, pubNameBytes...)
	// Extensions length: 0
	config = append(config, 0x00, 0x00)

	// Wrap in ECHConfigList with 2-byte length prefix
	list := make([]byte, 2+len(config))
	binary.BigEndian.PutUint16(list[0:2], uint16(len(config)))
	copy(list[2:], config)
	return list
}

func TestParseECHConfigBytes(t *testing.T) {
	pubKey := make([]byte, 32)
	for i := range pubKey {
		pubKey[i] = byte(i)
	}
	cipherSuites := []byte{0x00, 0x01, 0x00, 0x01}
	configID := uint8(42)
	kemID := uint16(0x0020)
	maxNameLen := uint8(128)
	publicName := "cloudflare-ech.com"

	data := buildTestECHConfigList(configID, kemID, pubKey, cipherSuites, maxNameLen, publicName)

	tmpl, err := ParseECHConfigBytes(data)
	if err != nil {
		t.Fatalf("ParseECHConfigBytes failed: %v", err)
	}

	if tmpl.ConfigID != configID {
		t.Errorf("ConfigID = %d, want %d", tmpl.ConfigID, configID)
	}
	if tmpl.KemID != kemID {
		t.Errorf("KemID = 0x%04x, want 0x%04x", tmpl.KemID, kemID)
	}
	if !bytes.Equal(tmpl.CipherSuites, cipherSuites) {
		t.Errorf("CipherSuites = %x, want %x", tmpl.CipherSuites, cipherSuites)
	}
	if tmpl.PublicName != publicName {
		t.Errorf("PublicName = %q, want %q", tmpl.PublicName, publicName)
	}
	if tmpl.MaxNameLen != maxNameLen {
		t.Errorf("MaxNameLen = %d, want %d", tmpl.MaxNameLen, maxNameLen)
	}
}

func TestParseECHConfigBytes_MultipleCipherSuites(t *testing.T) {
	pubKey := make([]byte, 32)
	// Two cipher suite pairs: HKDF-SHA256+AES-128-GCM and HKDF-SHA384+AES-256-GCM
	cipherSuites := []byte{
		0x00, 0x01, 0x00, 0x01, // HKDF-SHA256 + AES-128-GCM
		0x00, 0x02, 0x00, 0x02, // HKDF-SHA384 + AES-256-GCM
	}

	data := buildTestECHConfigList(10, 0x0020, pubKey, cipherSuites, 0, "example.com")

	tmpl, err := ParseECHConfigBytes(data)
	if err != nil {
		t.Fatalf("ParseECHConfigBytes failed: %v", err)
	}
	if !bytes.Equal(tmpl.CipherSuites, cipherSuites) {
		t.Errorf("CipherSuites = %x, want %x", tmpl.CipherSuites, cipherSuites)
	}
}

func TestParseECHConfigBytes_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"too short", []byte{0x00}},
		{"empty list", []byte{0x00, 0x00}},
		{"truncated list", []byte{0x00, 0x10, 0xfe}},
		{"bad version", []byte{0x00, 0x04, 0xff, 0xff, 0x00, 0x00}},
		{"truncated body", buildTruncatedConfig()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseECHConfigBytes(tt.data)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// buildTruncatedConfig creates a config that declares a body length but truncates it.
func buildTruncatedConfig() []byte {
	// Declare inner length of 50 but only provide 2 bytes
	config := []byte{0xfe, 0x0d, 0x00, 0x32, 0x01, 0x00}
	list := make([]byte, 2+len(config))
	binary.BigEndian.PutUint16(list[0:2], uint16(len(config)))
	copy(list[2:], config)
	return list
}

func TestGenerateECHKeyFromTemplate(t *testing.T) {
	tmpl := &ECHTemplate{
		ConfigID:     96,
		KemID:        0x0020,
		CipherSuites: []byte{0x00, 0x01, 0x00, 0x01},
		PublicName:   "cloudflare-ech.com",
		MaxNameLen:   128,
	}

	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	key, echConfigList, err := GenerateECHKeyFromTemplate(tmpl, privKey)
	if err != nil {
		t.Fatalf("GenerateECHKeyFromTemplate failed: %v", err)
	}

	// Verify private key
	if !bytes.Equal(key.PrivateKey, privKey.Bytes()) {
		t.Error("PrivateKey mismatch")
	}

	// Verify config is non-empty
	if len(key.Config) == 0 {
		t.Error("Config is empty")
	}

	// Verify ECHConfigList has length prefix
	if len(echConfigList) < 2 {
		t.Fatal("ECHConfigList too short")
	}
	listLen := binary.BigEndian.Uint16(echConfigList[0:2])
	if int(listLen) != len(echConfigList)-2 {
		t.Errorf("ECHConfigList length prefix = %d, body = %d", listLen, len(echConfigList)-2)
	}

	// Parse the generated config back and verify fields
	parsed, err := ParseECHConfigBytes(echConfigList)
	if err != nil {
		t.Fatalf("ParseECHConfigBytes on generated config: %v", err)
	}

	// config_id should be template + 1
	if parsed.ConfigID != tmpl.ConfigID+1 {
		t.Errorf("ConfigID = %d, want %d (template %d + 1)", parsed.ConfigID, tmpl.ConfigID+1, tmpl.ConfigID)
	}

	// KEM ID should match template
	if parsed.KemID != tmpl.KemID {
		t.Errorf("KemID = 0x%04x, want 0x%04x", parsed.KemID, tmpl.KemID)
	}

	// Cipher suites should match template
	if !bytes.Equal(parsed.CipherSuites, tmpl.CipherSuites) {
		t.Errorf("CipherSuites = %x, want %x", parsed.CipherSuites, tmpl.CipherSuites)
	}

	// Public name should match template
	if parsed.PublicName != tmpl.PublicName {
		t.Errorf("PublicName = %q, want %q", parsed.PublicName, tmpl.PublicName)
	}

	// Max name length should match template
	if parsed.MaxNameLen != tmpl.MaxNameLen {
		t.Errorf("MaxNameLen = %d, want %d", parsed.MaxNameLen, tmpl.MaxNameLen)
	}

	// Verify version bytes in raw config
	if key.Config[0] != 0xfe || key.Config[1] != 0x0d {
		t.Errorf("Config version = 0x%02x%02x, want 0xfe0d", key.Config[0], key.Config[1])
	}
}

func TestGenerateECHKeyFromTemplate_ConfigIDWrap(t *testing.T) {
	// When template config_id is 255, forged should wrap to 0
	tmpl := &ECHTemplate{
		ConfigID:     255,
		KemID:        0x0020,
		CipherSuites: []byte{0x00, 0x01, 0x00, 0x01},
		PublicName:   "example.com",
		MaxNameLen:   0,
	}

	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	_, echConfigList, err := GenerateECHKeyFromTemplate(tmpl, privKey)
	if err != nil {
		t.Fatalf("GenerateECHKeyFromTemplate failed: %v", err)
	}

	parsed, err := ParseECHConfigBytes(echConfigList)
	if err != nil {
		t.Fatalf("ParseECHConfigBytes: %v", err)
	}

	if parsed.ConfigID != 0 {
		t.Errorf("ConfigID = %d, want 0 (255 + 1 wraps to 0)", parsed.ConfigID)
	}
}

func TestGenerateECHKeyFromTemplate_NilInputs(t *testing.T) {
	privKey, _ := ecdh.X25519().GenerateKey(rand.Reader)

	_, _, err := GenerateECHKeyFromTemplate(nil, privKey)
	if err == nil {
		t.Error("expected error for nil template")
	}

	tmpl := &ECHTemplate{ConfigID: 1, KemID: 0x0020, CipherSuites: []byte{0x00, 0x01, 0x00, 0x01}, PublicName: "x.com"}
	_, _, err = GenerateECHKeyFromTemplate(tmpl, nil)
	if err == nil {
		t.Error("expected error for nil private key")
	}
}

// failingProvider always returns an error from FetchTemplate.
type failingProvider struct{}

func (f *failingProvider) FetchTemplate() (*ECHTemplate, error) {
	return nil, fmt.Errorf("network unavailable")
}

// staticProvider returns a fixed template.
type staticProvider struct {
	tmpl *ECHTemplate
}

func (s *staticProvider) FetchTemplate() (*ECHTemplate, error) {
	return s.tmpl, nil
}

func TestFetchTemplateWithFallback_LiveSuccess(t *testing.T) {
	expected := &ECHTemplate{
		ConfigID:     50,
		KemID:        0x0020,
		CipherSuites: []byte{0x00, 0x01, 0x00, 0x01},
		PublicName:   "live.example.com",
		MaxNameLen:   64,
	}

	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "ech_cache.json")

	tmpl, err := FetchTemplateWithFallback(&staticProvider{tmpl: expected}, cachePath)
	if err != nil {
		t.Fatalf("FetchTemplateWithFallback failed: %v", err)
	}

	if tmpl.ConfigID != expected.ConfigID {
		t.Errorf("ConfigID = %d, want %d", tmpl.ConfigID, expected.ConfigID)
	}
	if tmpl.PublicName != expected.PublicName {
		t.Errorf("PublicName = %q, want %q", tmpl.PublicName, expected.PublicName)
	}

	// Verify cache file was written
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	var cached ECHTemplate
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("cache file invalid JSON: %v", err)
	}
	if cached.ConfigID != expected.ConfigID {
		t.Errorf("cached ConfigID = %d, want %d", cached.ConfigID, expected.ConfigID)
	}
}

func TestFetchTemplateWithFallback_CacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "ech_cache.json")

	// Pre-populate cache
	cached := &ECHTemplate{
		ConfigID:     77,
		KemID:        0x0020,
		CipherSuites: []byte{0x00, 0x01, 0x00, 0x01},
		PublicName:   "cached.example.com",
		MaxNameLen:   32,
	}
	data, _ := json.Marshal(cached)
	os.WriteFile(cachePath, data, 0600)

	// Provider fails → should fall back to cache
	tmpl, err := FetchTemplateWithFallback(&failingProvider{}, cachePath)
	if err != nil {
		t.Fatalf("FetchTemplateWithFallback failed: %v", err)
	}
	if tmpl.ConfigID != cached.ConfigID {
		t.Errorf("ConfigID = %d, want %d", tmpl.ConfigID, cached.ConfigID)
	}
	if tmpl.PublicName != cached.PublicName {
		t.Errorf("PublicName = %q, want %q", tmpl.PublicName, cached.PublicName)
	}
}

func TestFetchTemplateWithFallback_DefaultFallback(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "nonexistent_cache.json")

	// Provider fails, no cache → should return hardcoded defaults
	tmpl, err := FetchTemplateWithFallback(&failingProvider{}, cachePath)
	if err != nil {
		t.Fatalf("FetchTemplateWithFallback failed: %v", err)
	}

	if tmpl.ConfigID != 97 {
		t.Errorf("ConfigID = %d, want 97 (hardcoded default)", tmpl.ConfigID)
	}
	if tmpl.KemID != 0x0020 {
		t.Errorf("KemID = 0x%04x, want 0x0020", tmpl.KemID)
	}
	if tmpl.PublicName != "cloudflare-ech.com" {
		t.Errorf("PublicName = %q, want %q", tmpl.PublicName, "cloudflare-ech.com")
	}
	expectedCS := []byte{0x00, 0x01, 0x00, 0x01}
	if !bytes.Equal(tmpl.CipherSuites, expectedCS) {
		t.Errorf("CipherSuites = %x, want %x", tmpl.CipherSuites, expectedCS)
	}
}

func TestFetchTemplateWithFallback_EmptyCachePath(t *testing.T) {
	// Provider fails, empty cache path → should return defaults without error
	tmpl, err := FetchTemplateWithFallback(&failingProvider{}, "")
	if err != nil {
		t.Fatalf("FetchTemplateWithFallback failed: %v", err)
	}
	if tmpl.ConfigID != 97 {
		t.Errorf("ConfigID = %d, want 97", tmpl.ConfigID)
	}
}

func TestLocalECHProviderFetchTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live DNS test in short mode")
	}

	// Check if network is available by trying to reach 1.1.1.1
	conn, err := net.DialTimeout("udp", "1.1.1.1:53", 2*1e9)
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	conn.Close()

	provider := &LocalECHProvider{}
	tmpl, err := provider.FetchTemplate()
	if err != nil {
		t.Fatalf("FetchTemplate failed: %v", err)
	}

	// Cloudflare's ECH config should have reasonable values
	if tmpl.KemID != 0x0020 {
		t.Errorf("KemID = 0x%04x, expected 0x0020 (X25519)", tmpl.KemID)
	}
	if len(tmpl.CipherSuites) == 0 {
		t.Error("CipherSuites is empty")
	}
	if len(tmpl.CipherSuites)%4 != 0 {
		t.Errorf("CipherSuites length %d is not a multiple of 4", len(tmpl.CipherSuites))
	}
	if tmpl.PublicName == "" {
		t.Error("PublicName is empty")
	}

	t.Logf("Cloudflare ECH template: config_id=%d kem=0x%04x cs=%x public_name=%q max_name_len=%d",
		tmpl.ConfigID, tmpl.KemID, tmpl.CipherSuites, tmpl.PublicName, tmpl.MaxNameLen)
}
