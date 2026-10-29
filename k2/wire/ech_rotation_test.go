package wire

import (
	"crypto/ecdh"
	"crypto/rand"
	tls "github.com/metacubex/utls"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// --- helpers ---

// makeTestECHConfig builds a minimal valid ECHConfig (raw, no list prefix)
// with the given configID and public key from privKey.
func makeTestECHConfig(t *testing.T, configID uint8, privKey *ecdh.PrivateKey) []byte {
	t.Helper()
	pubKeyBytes := privKey.PublicKey().Bytes()
	publicNameBytes := []byte("example.com")
	cipherSuites := []byte{0x00, 0x01, 0x00, 0x01} // HKDF-SHA256 + AES-128-GCM

	innerLen := 1 + 2 + 2 + len(pubKeyBytes) + 2 + len(cipherSuites) + 1 + 1 + len(publicNameBytes) + 2

	config := make([]byte, 0, 4+innerLen)
	config = append(config, 0xfe, 0x0d)                         // version
	config = append(config, byte(innerLen>>8), byte(innerLen))   // length
	config = append(config, configID)                            // config_id
	config = append(config, 0x00, 0x20)                          // kem_id: X25519
	config = append(config, byte(len(pubKeyBytes)>>8), byte(len(pubKeyBytes)))
	config = append(config, pubKeyBytes...)
	config = append(config, byte(len(cipherSuites)>>8), byte(len(cipherSuites)))
	config = append(config, cipherSuites...)
	config = append(config, 0x00)                                // max_name_len
	config = append(config, byte(len(publicNameBytes)))          // public_name_len
	config = append(config, publicNameBytes...)
	config = append(config, 0x00, 0x00)                          // extensions_len
	return config
}

func generateTestKey(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// --- SaveECHPrivateKey / LoadECHPrivateKey ---

func TestSaveLoadECHPrivateKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ech.key")
	privKey := generateTestKey(t)

	// Save
	if err := SaveECHPrivateKey(keyPath, privKey.Bytes()); err != nil {
		t.Fatalf("SaveECHPrivateKey: %v", err)
	}

	// Verify file is exactly 32 bytes (X25519 key size)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 32 {
		t.Fatalf("expected 32 bytes, got %d", info.Size())
	}

	// Load
	loaded, err := LoadECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadECHPrivateKey: %v", err)
	}

	// Verify round-trip: same public key
	if loaded.PublicKey().Equal(privKey.PublicKey()) != true {
		t.Fatalf("loaded key does not match original")
	}
}

func TestLoadECHPrivateKey_NotFound(t *testing.T) {
	_, err := LoadECHPrivateKey("/nonexistent/ech.key")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- EnsureECHPrivateKey ---

func TestEnsureECHPrivateKey_GeneratesNew(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ech.key")

	key, err := EnsureECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("EnsureECHPrivateKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}

	// File should exist
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() != 32 {
		t.Fatalf("expected 32 bytes, got %d", info.Size())
	}
}

func TestEnsureECHPrivateKey_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ech.key")

	// Create key
	key1, err := EnsureECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("first EnsureECHPrivateKey: %v", err)
	}

	// Load same key
	key2, err := EnsureECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("second EnsureECHPrivateKey: %v", err)
	}

	if !key1.PublicKey().Equal(key2.PublicKey()) {
		t.Fatal("second call returned different key")
	}
}

func TestEnsureECHPrivateKey_MigratesOldFormat(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ech.key")

	// Write old-format file (key+config blob, >40 bytes)
	privKey := generateTestKey(t)
	echConfig := makeTestECHConfig(t, 42, privKey)

	// Build old format: ECHConfigList
	echConfigList := make([]byte, 2+len(echConfig))
	binary.BigEndian.PutUint16(echConfigList[0:2], uint16(len(echConfig)))
	copy(echConfigList[2:], echConfig)

	if err := SaveECHKey(keyPath, privKey.Bytes(), echConfigList); err != nil {
		t.Fatalf("SaveECHKey (old format): %v", err)
	}

	// Also create ech-configs dir path for migration target
	configsDir := filepath.Join(dir, "ech-configs")

	// File should be > 40 bytes (old format)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() <= 40 {
		t.Fatalf("old format file should be > 40 bytes, got %d", info.Size())
	}

	// EnsureECHPrivateKey should detect old format, migrate
	loaded, err := EnsureECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("EnsureECHPrivateKey (migration): %v", err)
	}

	// Key should match
	if !loaded.PublicKey().Equal(privKey.PublicKey()) {
		t.Fatal("migrated key does not match original")
	}

	// File should now be 32 bytes (new format)
	info, err = os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat after migration: %v", err)
	}
	if info.Size() != 32 {
		t.Fatalf("after migration: expected 32 bytes, got %d", info.Size())
	}

	// Config should be saved to ech-configs/
	configs, err := LoadAllECHConfigs(configsDir)
	if err != nil {
		t.Fatalf("LoadAllECHConfigs after migration: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 migrated config, got %d", len(configs))
	}
}

// --- SaveECHConfig / LoadAllECHConfigs ---

func TestSaveAndLoadECHConfigs(t *testing.T) {
	dir := t.TempDir()
	configsDir := filepath.Join(dir, "ech-configs")

	privKey := generateTestKey(t)
	config98 := makeTestECHConfig(t, 98, privKey)
	config97 := makeTestECHConfig(t, 97, privKey)

	// Save configs
	if err := SaveECHConfig(configsDir, config98); err != nil {
		t.Fatalf("SaveECHConfig 98: %v", err)
	}
	if err := SaveECHConfig(configsDir, config97); err != nil {
		t.Fatalf("SaveECHConfig 97: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(configsDir, "config-98.bin")); err != nil {
		t.Fatalf("config-98.bin not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configsDir, "config-97.bin")); err != nil {
		t.Fatalf("config-97.bin not found: %v", err)
	}

	// Load all — should be sorted descending by config_id
	configs, err := LoadAllECHConfigs(configsDir)
	if err != nil {
		t.Fatalf("LoadAllECHConfigs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	// First config should be 98 (highest), second 97
	id0 := configs[0][4] // version(2) + length(2) + config_id(1)
	id1 := configs[1][4]
	if id0 != 98 {
		t.Fatalf("expected first config_id=98, got %d", id0)
	}
	if id1 != 97 {
		t.Fatalf("expected second config_id=97, got %d", id1)
	}
}

func TestSaveECHConfig_ExtractsConfigID(t *testing.T) {
	dir := t.TempDir()
	configsDir := filepath.Join(dir, "ech-configs")

	privKey := generateTestKey(t)
	config := makeTestECHConfig(t, 42, privKey)

	// SaveECHConfig should read config_id from bytes and name the file config-42.bin
	if err := SaveECHConfig(configsDir, config); err != nil {
		t.Fatalf("SaveECHConfig: %v", err)
	}

	if _, err := os.Stat(filepath.Join(configsDir, "config-42.bin")); err != nil {
		t.Fatalf("expected config-42.bin, got error: %v", err)
	}
}

func TestLoadAllECHConfigs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	configsDir := filepath.Join(dir, "ech-configs")
	os.MkdirAll(configsDir, 0755)

	configs, err := LoadAllECHConfigs(configsDir)
	if err != nil {
		t.Fatalf("LoadAllECHConfigs empty: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(configs))
	}
}

func TestLoadAllECHConfigs_NonexistentDir(t *testing.T) {
	configs, err := LoadAllECHConfigs("/nonexistent/ech-configs")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(configs))
	}
}

// --- BuildECHKeys ---

func TestBuildECHKeys(t *testing.T) {
	privKey := generateTestKey(t)
	config98 := makeTestECHConfig(t, 98, privKey)
	config97 := makeTestECHConfig(t, 97, privKey)

	keys := BuildECHKeys(privKey, [][]byte{config98, config97})
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// All keys should have same private key
	for i, k := range keys {
		if string(k.PrivateKey) != string(privKey.Bytes()) {
			t.Fatalf("key %d: private key mismatch", i)
		}
	}

	// Each key has its own config
	if keys[0].Config[4] != 98 {
		t.Fatalf("key 0: expected config_id 98, got %d", keys[0].Config[4])
	}
	if keys[1].Config[4] != 97 {
		t.Fatalf("key 1: expected config_id 97, got %d", keys[1].Config[4])
	}
}

func TestBuildECHKeys_Empty(t *testing.T) {
	privKey := generateTestKey(t)
	keys := BuildECHKeys(privKey, nil)
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

// --- BuildECHConfigList ---

func TestBuildECHConfigList_Single(t *testing.T) {
	privKey := generateTestKey(t)
	config := makeTestECHConfig(t, 42, privKey)

	list := BuildECHConfigList([][]byte{config})

	// ECHConfigList: [total_len:2][configs...]
	if len(list) < 2 {
		t.Fatalf("list too short: %d bytes", len(list))
	}
	totalLen := binary.BigEndian.Uint16(list[0:2])
	if int(totalLen) != len(config) {
		t.Fatalf("expected total_len=%d, got %d", len(config), totalLen)
	}
	// Payload should be the raw config
	payload := list[2:]
	if len(payload) != len(config) {
		t.Fatalf("payload length mismatch: %d vs %d", len(payload), len(config))
	}
	for i := range config {
		if payload[i] != config[i] {
			t.Fatalf("byte %d mismatch", i)
		}
	}
}

func TestBuildECHConfigList_Multiple(t *testing.T) {
	privKey := generateTestKey(t)
	config1 := makeTestECHConfig(t, 98, privKey)
	config2 := makeTestECHConfig(t, 97, privKey)

	list := BuildECHConfigList([][]byte{config1, config2})

	totalLen := binary.BigEndian.Uint16(list[0:2])
	expectedLen := len(config1) + len(config2)
	if int(totalLen) != expectedLen {
		t.Fatalf("expected total_len=%d, got %d", expectedLen, totalLen)
	}
	if len(list) != 2+expectedLen {
		t.Fatalf("expected list len=%d, got %d", 2+expectedLen, len(list))
	}
}

func TestBuildECHConfigList_Empty(t *testing.T) {
	list := BuildECHConfigList(nil)
	if len(list) != 2 {
		t.Fatalf("expected 2-byte header for empty list, got %d", len(list))
	}
	if list[0] != 0 || list[1] != 0 {
		t.Fatal("expected zero-length list")
	}
}

// --- Integration: full rotation flow ---

func TestECHRotationFlow(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ech.key")
	configsDir := filepath.Join(dir, "ech-configs")

	// 1. Ensure private key
	privKey, err := EnsureECHPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("EnsureECHPrivateKey: %v", err)
	}

	// 2. Generate config via template (simulate)
	tmpl := &ECHTemplate{
		ConfigID:     97,
		KemID:        0x0020,
		CipherSuites: []byte{0x00, 0x01, 0x00, 0x01},
		PublicName:   "cloudflare-ech.com",
		MaxNameLen:   0,
	}
	echKey, _, err := GenerateECHKeyFromTemplate(tmpl, privKey)
	if err != nil {
		t.Fatalf("GenerateECHKeyFromTemplate: %v", err)
	}

	// 3. Save the config
	if err := SaveECHConfig(configsDir, echKey.Config); err != nil {
		t.Fatalf("SaveECHConfig: %v", err)
	}

	// 4. Load all configs
	allConfigs, err := LoadAllECHConfigs(configsDir)
	if err != nil {
		t.Fatalf("LoadAllECHConfigs: %v", err)
	}
	if len(allConfigs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(allConfigs))
	}

	// 5. Build keys for TLS
	keys := BuildECHKeys(privKey, allConfigs)
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	// 6. Build config list for distribution
	configList := BuildECHConfigList(allConfigs[:1])
	if len(configList) < 2 {
		t.Fatal("config list too short")
	}

	// Verify the key is usable in tls.Config
	tlsCfg := &tls.Config{
		EncryptedClientHelloKeys: keys,
	}
	if len(tlsCfg.EncryptedClientHelloKeys) != 1 {
		t.Fatal("TLS config should have 1 ECH key")
	}

	// Verify config list starts with valid length prefix
	listLen := binary.BigEndian.Uint16(configList[0:2])
	if int(listLen) != len(allConfigs[0]) {
		t.Fatalf("config list length mismatch: %d vs %d", listLen, len(allConfigs[0]))
	}
}
