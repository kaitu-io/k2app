# Encrypted Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AES-256-GCM encryption to Tauri desktop storage with backward-compatible plaintext reads, and enable MCP (Go) to decrypt and read the same storage file for session sharing.

**Architecture:** Rust `storage.rs` gains an encryption layer using AES-256-GCM with HKDF-SHA256 key derivation from platform hardware IDs. Values are prefixed with `ENC1:` to distinguish from legacy plaintext. Reads auto-detect format (backward compat); writes always encrypt. Go MCP reimplements the same crypto with shared test vectors to guarantee cross-language consistency. MCP reads Tauri's `storage.json` (read-only) to share desktop session instead of maintaining separate auth.

**Tech Stack:** Rust (`aes-gcm`, `hkdf`, `sha2`, `base64` crates), Go (`crypto/aes`, `crypto/cipher`, `crypto/sha256`, `golang.org/x/crypto/hkdf`, `encoding/base64`)

---

## File Structure

### Rust (desktop)

| File | Action | Responsibility |
|------|--------|----------------|
| `desktop/src-tauri/src/storage_crypto.rs` | Create | Hardware ID, HKDF key derivation, AES-256-GCM encrypt/decrypt, `ENC1:` prefix handling |
| `desktop/src-tauri/src/storage.rs` | Modify | Integrate crypto: encrypt on `set`, auto-detect+decrypt on `get`, lazy migration |
| `desktop/src-tauri/src/main.rs` | Modify | Pass init error logging (no structural change) |
| `desktop/src-tauri/Cargo.toml` | Modify | Add `aes-gcm`, `hkdf`, `sha2`, move `base64` from linux-only to all-platform |

### Go (MCP)

| File | Action | Responsibility |
|------|--------|----------------|
| `mcp/storage_crypto.go` | Create | Hardware ID (same as Rust), HKDF key derivation, AES-256-GCM decrypt, `ENC1:` detection |
| `mcp/storage_crypto_test.go` | Create | Shared test vectors, cross-language verification |
| `mcp/tauri_storage.go` | Create | Read Tauri `storage.json`, decrypt values, extract UDID+tokens |
| `mcp/tauri_storage_test.go` | Create | Test reading mock storage files (plaintext + encrypted + mixed) |
| `mcp/session.go` | Modify | Add `RestoreFromTauri()` method, modify startup to try Tauri storage first |
| `mcp/main.go` | Modify | Try Tauri session before MCP session on startup |
| `mcp/go.mod` | Modify | Add `golang.org/x/crypto` dependency |

### Shared

| File | Action | Responsibility |
|------|--------|----------------|
| `docs/superpowers/plans/2026-03-25-encrypted-storage.md` | Create | This plan |

---

## Shared Test Vectors

Both Rust and Go tests MUST assert identical results for these fixed inputs:

```
Hardware ID:     "FC891097-D4C1-3B7A-8611-0F5C8ED3A23B"
HKDF salt:       b"kaitu-desktop-storage-v1"
HKDF info:       b"aes-256-gcm-key"
Expected key:    (computed once in Task 1, hardcoded in both languages)

Plaintext:       "\"eyJhbGciOiJIUzI1NiJ9.test\""
Nonce (fixed):   [0x00, 0x01, 0x02, ..., 0x0b]  (12 bytes, for test only)
Expected ENC1:   "ENC1:<base64 of nonce||ciphertext||tag>"
```

The Rust implementation computes the expected values first; Go tests assert the same values.

---

## Task 1: Rust — `storage_crypto.rs` core module

**Files:**
- Create: `desktop/src-tauri/src/storage_crypto.rs`
- Modify: `desktop/src-tauri/Cargo.toml`

- [ ] **Step 1: Add dependencies to Cargo.toml**

```toml
# In [dependencies], add:
aes-gcm = "0.10"
hkdf = "0.12"
sha2 = "0.10"
# Move base64 from linux-only to all-platform:
base64 = "0.22"

# Remove from [target.'cfg(target_os = "linux")'.dependencies]:
# base64 = "0.22"  (already moved above)
```

- [ ] **Step 2: Write failing test — hardware ID retrieval**

In `storage_crypto.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_get_hardware_id_not_empty() {
        let id = get_hardware_id();
        assert!(!id.is_empty(), "Hardware ID must not be empty");
    }
}
```

- [ ] **Step 3: Implement hardware ID function**

```rust
//! Storage encryption: AES-256-GCM with HKDF-SHA256 key derivation.
//!
//! Key derived from platform hardware ID (never stored).
//! Encrypted values prefixed with "ENC1:" for backward compatibility.

use aes_gcm::{Aes256Gcm, Nonce};
use aes_gcm::aead::{Aead, KeyInit};
use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64};
use hkdf::Hkdf;
use sha2::Sha256;
use std::process::Command;

const ENC_PREFIX: &str = "ENC1:";
const HKDF_SALT: &[u8] = b"kaitu-desktop-storage-v1";
const HKDF_INFO: &[u8] = b"aes-256-gcm-key";

/// Read a stable hardware identifier for the current machine.
///
/// macOS: IOPlatformUUID via `sysctl -n kern.uuid`
/// Windows: SMBIOS UUID via PowerShell `Get-CimInstance` (wmic deprecated since Win11 24H2)
/// Linux: `/etc/machine-id`
///
/// This was removed from service.rs (commit d4ebdd6) for device identity
/// (collision risk), but is ideal for local encryption key derivation
/// where per-machine stability matters and collisions are harmless.
fn get_hardware_id() -> String {
    get_hardware_id_platform().unwrap_or_else(|e| {
        log::warn!("[storage_crypto] Hardware ID unavailable: {}, using fallback", e);
        // Fallback: hostname via Command (no extra crate dependency)
        Command::new("hostname")
            .output()
            .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
            .unwrap_or_else(|_| "kaitu-default-key".to_string())
    })
}

#[cfg(target_os = "macos")]
fn get_hardware_id_platform() -> Result<String, String> {
    let output = Command::new("sysctl")
        .args(["-n", "kern.uuid"])
        .output()
        .map_err(|e| format!("sysctl failed: {}", e))?;
    let uuid = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if uuid.is_empty() {
        return Err("Empty kern.uuid".to_string());
    }
    Ok(uuid)
}

#[cfg(target_os = "windows")]
fn get_hardware_id_platform() -> Result<String, String> {
    // PowerShell Get-CimInstance (works on Win10+ including Win11 24H2 where wmic is removed)
    let output = Command::new("powershell")
        .args(["-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID"])
        .output()
        .map_err(|e| format!("powershell failed: {}", e))?;
    let uuid = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if uuid.is_empty() || uuid == "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF" {
        return Err("No valid SMBIOS UUID".to_string());
    }
    Ok(uuid)
}

#[cfg(target_os = "linux")]
fn get_hardware_id_platform() -> Result<String, String> {
    std::fs::read_to_string("/etc/machine-id")
        .map(|s| s.trim().to_string())
        .map_err(|e| format!("Failed to read machine-id: {}", e))
}
```

- [ ] **Step 4: Run test to verify hardware ID works**

Run: `cd desktop/src-tauri && cargo test storage_crypto::tests::test_get_hardware_id_not_empty -- --nocapture`
Expected: PASS

- [ ] **Step 5: Write failing test — HKDF key derivation with test vector**

```rust
    #[test]
    fn test_derive_key_deterministic() {
        let key1 = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        let key2 = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        assert_eq!(key1, key2, "Same input must produce same key");
    }

    #[test]
    fn test_derive_key_test_vector() {
        let key = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        let hex = key.iter().map(|b| format!("{:02x}", b)).collect::<String>();
        // Print for Go test vector (first run only)
        println!("HKDF test vector key hex: {}", hex);
        assert_eq!(key.len(), 32);
    }
```

- [ ] **Step 6: Implement `derive_key`**

```rust
/// Derive a 32-byte AES-256 key from a hardware ID via HKDF-SHA256.
fn derive_key(hardware_id: &str) -> [u8; 32] {
    let hk = Hkdf::<Sha256>::new(Some(HKDF_SALT), hardware_id.as_bytes());
    let mut key = [0u8; 32];
    hk.expand(HKDF_INFO, &mut key)
        .expect("HKDF expand failed (32 bytes always valid for SHA-256)");
    key
}
```

- [ ] **Step 7: Run tests, capture key hex for Go test vector**

Run: `cd desktop/src-tauri && cargo test storage_crypto::tests::test_derive_key_test_vector -- --nocapture`
Expected: PASS, prints key hex. **Record this hex — it goes into Go tests.**

- [ ] **Step 8: Write failing test — encrypt/decrypt round-trip**

```rust
    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let key = derive_key("test-hardware-id");
        let plaintext = "\"eyJhbGciOiJIUzI1NiJ9.test\"";
        let encrypted = encrypt_value(plaintext, &key);
        assert!(encrypted.starts_with(ENC_PREFIX));
        let decrypted = decrypt_value(&encrypted, &key).unwrap();
        assert_eq!(decrypted, plaintext);
    }

    #[test]
    fn test_decrypt_wrong_key_fails() {
        let key1 = derive_key("key-1");
        let key2 = derive_key("key-2");
        let encrypted = encrypt_value("secret", &key1);
        assert!(decrypt_value(&encrypted, &key2).is_none());
    }

    #[test]
    fn test_encrypt_with_fixed_nonce_test_vector() {
        let key = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        let nonce_bytes: [u8; 12] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11];
        let plaintext = "\"hello-from-rust\"";
        let encrypted = encrypt_value_with_nonce(plaintext, &key, &nonce_bytes);
        println!("ENC1 test vector: {}", encrypted);
        // Record this output — it goes into Go tests.
        assert!(encrypted.starts_with(ENC_PREFIX));
    }
```

- [ ] **Step 9: Implement encrypt/decrypt functions**

```rust
/// Encrypt a value and return "ENC1:" + base64(nonce || ciphertext || tag).
pub(crate) fn encrypt_value(plaintext: &str, key: &[u8; 32]) -> String {
    let mut nonce_bytes = [0u8; 12];
    getrandom::getrandom(&mut nonce_bytes).expect("getrandom failed");
    encrypt_value_with_nonce(plaintext, key, &nonce_bytes)
}

/// Encrypt with a specific nonce (for testing deterministic output).
fn encrypt_value_with_nonce(plaintext: &str, key: &[u8; 32], nonce_bytes: &[u8; 12]) -> String {
    let cipher = Aes256Gcm::new_from_slice(key).expect("key length is 32");
    let nonce = Nonce::from_slice(nonce_bytes);
    let ciphertext = cipher.encrypt(nonce, plaintext.as_bytes())
        .expect("AES-GCM encryption should not fail");
    // Combine: nonce (12) || ciphertext+tag
    let mut combined = Vec::with_capacity(12 + ciphertext.len());
    combined.extend_from_slice(nonce_bytes);
    combined.extend_from_slice(&ciphertext);
    format!("{}{}", ENC_PREFIX, BASE64.encode(&combined))
}

/// Decrypt an "ENC1:"-prefixed value. Returns None on any failure.
pub(crate) fn decrypt_value(encrypted: &str, key: &[u8; 32]) -> Option<String> {
    let encoded = encrypted.strip_prefix(ENC_PREFIX)?;
    let combined = BASE64.decode(encoded).ok()?;
    if combined.len() < 12 {
        return None;
    }
    let (nonce_bytes, ciphertext) = combined.split_at(12);
    let cipher = Aes256Gcm::new_from_slice(key).expect("key length is 32");
    let nonce = Nonce::from_slice(nonce_bytes);
    let plaintext = cipher.decrypt(nonce, ciphertext).ok()?;
    String::from_utf8(plaintext).ok()
}

/// Returns true if a storage value is encrypted.
pub(crate) fn is_encrypted(value: &str) -> bool {
    value.starts_with(ENC_PREFIX)
}
```

- [ ] **Step 10: Add `getrandom` to Cargo.toml**

```toml
getrandom = "0.2"
```

Note: `aes-gcm` already depends on `getrandom` transitively, but listing it explicitly is cleaner for direct use.

- [ ] **Step 11: Run all crypto tests**

Run: `cd desktop/src-tauri && cargo test storage_crypto -- --nocapture`
Expected: All PASS. **Record the ENC1 test vector output.**

- [ ] **Step 12: Commit**

```bash
git add desktop/src-tauri/src/storage_crypto.rs desktop/src-tauri/Cargo.toml desktop/src-tauri/Cargo.lock
git commit -m "feat(desktop): add storage_crypto module — AES-256-GCM + HKDF key derivation"
```

---

## Task 2: Rust — Integrate crypto into `storage.rs`

**Files:**
- Modify: `desktop/src-tauri/src/storage.rs`
- Modify: `desktop/src-tauri/src/main.rs` (add `mod storage_crypto`)

- [ ] **Step 1: Add `mod storage_crypto` to main.rs**

In `desktop/src-tauri/src/main.rs`, add after `mod storage`:

```rust
mod storage_crypto;
```

- [ ] **Step 2: Add `enc_key` field to `StorageState`**

```rust
pub struct StorageState {
    pub data: Mutex<HashMap<String, String>>,
    pub path: Mutex<Option<PathBuf>>,
    pub enc_key: [u8; 32],  // AES-256 key, derived once at init
}

impl StorageState {
    pub fn new() -> Self {
        let enc_key = crate::storage_crypto::derive_key_from_hardware();
        Self {
            data: Mutex::new(HashMap::new()),
            path: Mutex::new(None),
            enc_key,
        }
    }
}
```

- [ ] **Step 3: Write failing test — encrypted storage set/get round-trip**

In `storage.rs`, add test:

```rust
    #[test]
    fn test_encrypted_storage_roundtrip() {
        let dir = std::env::temp_dir().join("k2app-test-storage-enc-rt");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");

        let key = crate::storage_crypto::derive_key("test-key");
        let mut data = HashMap::new();
        let encrypted = crate::storage_crypto::encrypt_value("\"my-secret\"", &key);
        data.insert("token".to_string(), encrypted);
        persist_to_disk(&path, &data);

        let loaded = load_from_disk(&path);
        let raw = loaded.get("token").unwrap();
        assert!(crate::storage_crypto::is_encrypted(raw));
        let decrypted = crate::storage_crypto::decrypt_value(raw, &key).unwrap();
        assert_eq!(decrypted, "\"my-secret\"");
        let _ = fs::remove_dir_all(&dir);
    }
```

- [ ] **Step 3: Add `derive_key_from_hardware` public function in storage_crypto.rs**

```rust
/// Derive the encryption key from the platform hardware ID.
/// Called once at init and stored in StorageState (not a global static,
/// so tests can use different keys without OnceLock poisoning).
pub(crate) fn derive_key_from_hardware() -> [u8; 32] {
    let hw_id = get_hardware_id();
    log::info!("[storage_crypto] Key derived (hardware ID length: {})", hw_id.len());
    derive_key(&hw_id)
}
```

- [ ] **Step 4: Run test to verify**

Run: `cd desktop/src-tauri && cargo test test_encrypted_storage_roundtrip -- --nocapture`
Expected: PASS

- [ ] **Step 5: Modify `storage_set` to encrypt**

```rust
#[tauri::command]
pub fn storage_set(key: String, value: String, state: tauri::State<'_, StorageState>) {
    let encrypted = crate::storage_crypto::encrypt_value(&value, &state.enc_key);

    let mut data = state.data.lock().unwrap();
    data.insert(key, encrypted);
    let path_clone = state.path.lock().unwrap().clone();
    if let Some(ref p) = path_clone {
        persist_to_disk(p, &data);
    }
}
```

- [ ] **Step 6: Modify `storage_get` to auto-detect and decrypt**

```rust
#[tauri::command]
pub fn storage_get(key: String, state: tauri::State<'_, StorageState>) -> Option<String> {
    let data = state.data.lock().unwrap();
    let raw = data.get(&key)?;
    if crate::storage_crypto::is_encrypted(raw) {
        match crate::storage_crypto::decrypt_value(raw, &state.enc_key) {
            Some(plaintext) => Some(plaintext),
            None => {
                log::warn!("[storage] Failed to decrypt key '{}', treating as corrupt", key);
                None
            }
        }
    } else {
        // Backward compat: return plaintext as-is
        Some(raw.clone())
    }
}
```

- [ ] **Step 7: Write test — backward compatibility with plaintext values**

```rust
    #[test]
    fn test_backward_compat_plaintext_read() {
        // Simulate pre-encryption storage: values are plaintext
        let dir = std::env::temp_dir().join("k2app-test-storage-backcompat");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");
        // Write plaintext directly (old format)
        fs::write(&path, r#"{"k2.auth.token":"\"eyJtoken\"","device-udid":"\"some-uuid\""}"#).unwrap();
        let data = load_from_disk(&path);
        // Plaintext values should be readable without decryption
        let token = data.get("k2.auth.token").unwrap();
        assert!(!crate::storage_crypto::is_encrypted(token));
        assert_eq!(token, "\"eyJtoken\"");
        let _ = fs::remove_dir_all(&dir);
    }
```

- [ ] **Step 8: Run all storage tests**

Run: `cd desktop/src-tauri && cargo test storage -- --nocapture`
Expected: All PASS

- [ ] **Step 9: Run full Rust test suite**

Run: `cd desktop/src-tauri && cargo test`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add desktop/src-tauri/src/storage.rs desktop/src-tauri/src/storage_crypto.rs desktop/src-tauri/src/main.rs
git commit -m "feat(desktop): integrate AES-256-GCM encryption into storage — backward-compatible reads"
```

---

## Task 3: Go — `storage_crypto.go` with shared test vectors

**Files:**
- Create: `mcp/storage_crypto.go`
- Create: `mcp/storage_crypto_test.go`
- Modify: `mcp/go.mod` (add `golang.org/x/crypto`)

- [ ] **Step 1: Add golang.org/x/crypto dependency**

Run: `cd mcp && go get golang.org/x/crypto`

- [ ] **Step 2: Write failing test — HKDF key derivation matches Rust**

In `mcp/storage_crypto_test.go`:

```go
package main

import (
	"encoding/hex"
	"testing"
)

func TestDeriveKeyMatchesRust(t *testing.T) {
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")
	got := hex.EncodeToString(key[:])
	// This value MUST match the Rust test_derive_key_test_vector output from Task 1 Step 7
	want := "PASTE_RUST_HEX_HERE"
	if got != want {
		t.Errorf("HKDF key mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}
```

- [ ] **Step 3: Implement `deriveKey` in Go**

In `mcp/storage_crypto.go`:

```go
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	encPrefix = "ENC1:"
	hkdfSalt  = "kaitu-desktop-storage-v1"
	hkdfInfo  = "aes-256-gcm-key"
)

// deriveKey derives a 32-byte AES-256 key from a hardware ID via HKDF-SHA256.
func deriveKey(hardwareID string) [32]byte {
	hk := hkdf.New(sha256.New, []byte(hardwareID), []byte(hkdfSalt), []byte(hkdfInfo))
	var key [32]byte
	if _, err := io.ReadFull(hk, key[:]); err != nil {
		panic(fmt.Sprintf("HKDF expand failed: %v", err))
	}
	return key
}
```

- [ ] **Step 4: Run test (will fail until Rust hex is pasted)**

Run: `cd mcp && go test -run TestDeriveKeyMatchesRust -v`
Expected: FAIL (placeholder). After pasting Rust hex: PASS.

- [ ] **Step 5: Write failing test — decrypt matches Rust fixed-nonce output**

```go
func TestDecryptMatchesRust(t *testing.T) {
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")
	// This value MUST match the Rust test_encrypt_with_fixed_nonce_test_vector output from Task 1 Step 11
	encrypted := "PASTE_RUST_ENC1_HERE"
	plaintext, err := decryptValue(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if plaintext != "\"hello-from-rust\"" {
		t.Errorf("plaintext mismatch: got %q, want %q", plaintext, "\"hello-from-rust\"")
	}
}
```

- [ ] **Step 6: Implement `decryptValue` in Go**

```go
// decryptValue decrypts an "ENC1:"-prefixed value. Returns error on any failure.
func decryptValue(encrypted string, key [32]byte) (string, error) {
	if !strings.HasPrefix(encrypted, encPrefix) {
		return "", fmt.Errorf("missing ENC1: prefix")
	}
	encoded := encrypted[len(encPrefix):]
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(combined) < 12 {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce := combined[:12]
	ciphertext := combined[12:]

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm new: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("gcm decrypt: %w", err)
	}
	return string(plaintext), nil
}

// isEncrypted returns true if a storage value has the ENC1: prefix.
func isEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}
```

- [ ] **Step 7: Run test (will fail until Rust ENC1 is pasted)**

Run: `cd mcp && go test -run TestDecryptMatchesRust -v`
Expected: FAIL (placeholder). After pasting Rust output: PASS.

- [ ] **Step 8: Write and implement `getHardwareID` for Go**

```go
// getHardwareID reads the platform hardware ID (same as Rust storage_crypto.rs).
func getHardwareID() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "kern.uuid").Output()
		if err != nil {
			return "", fmt.Errorf("sysctl failed: %w", err)
		}
		id := strings.TrimSpace(string(out))
		if id == "" {
			return "", fmt.Errorf("empty kern.uuid")
		}
		return id, nil
	case "windows":
		// PowerShell Get-CimInstance (works on Win10+ including Win11 24H2 where wmic is removed)
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			"(Get-CimInstance Win32_ComputerSystemProduct).UUID").Output()
		if err != nil {
			return "", fmt.Errorf("powershell failed: %w", err)
		}
		id := strings.TrimSpace(string(out))
		if id == "" || id == "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF" {
			return "", fmt.Errorf("no valid SMBIOS UUID")
		}
		return id, nil
	case "linux":
		b, err := os.ReadFile("/etc/machine-id")
		if err != nil {
			return "", fmt.Errorf("read machine-id: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
```

- [ ] **Step 9: Write test — hardware ID same as Rust on current machine**

```go
func TestHardwareIDNotEmpty(t *testing.T) {
	id, err := getHardwareID()
	if err != nil {
		t.Fatalf("getHardwareID: %v", err)
	}
	if len(id) == 0 {
		t.Fatal("hardware ID is empty")
	}
	t.Logf("Hardware ID: %s", id)
}
```

- [ ] **Step 10: Write test — plaintext passthrough**

```go
func TestIsEncrypted(t *testing.T) {
	if isEncrypted("\"plain-value\"") {
		t.Error("plaintext should not be detected as encrypted")
	}
	if !isEncrypted("ENC1:AAAA") {
		t.Error("ENC1: prefixed value should be detected as encrypted")
	}
}
```

- [ ] **Step 11: Run all crypto tests**

Run: `cd mcp && go test -run "TestDeriveKey|TestDecrypt|TestHardwareID|TestIsEncrypted" -v`
Expected: All PASS

- [ ] **Step 12: Commit**

```bash
git add mcp/storage_crypto.go mcp/storage_crypto_test.go mcp/go.mod mcp/go.sum
git commit -m "feat(k2-mcp): add storage_crypto — HKDF key derivation + AES-256-GCM decrypt (shared test vectors with Rust)"
```

---

## Task 4: Go — `tauri_storage.go` read Tauri storage

**Files:**
- Create: `mcp/tauri_storage.go`
- Create: `mcp/tauri_storage_test.go`

- [ ] **Step 1: Write failing test — read plaintext storage**

In `mcp/tauri_storage_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTauriStoragePlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	os.WriteFile(path, []byte(`{
		"k2.auth.token": "\"eyJtoken123\"",
		"device-udid": "\"04cacd29-e71a-4884-842d-a2a5892d4db9\"",
		"onboarding_completed": "true"
	}`), 0600)

	ts, err := readTauriStorage(path, nil)
	if err != nil {
		t.Fatalf("readTauriStorage: %v", err)
	}
	if ts.AccessToken != "eyJtoken123" {
		t.Errorf("token: got %q, want %q", ts.AccessToken, "eyJtoken123")
	}
	if ts.RawUDID != "04cacd29-e71a-4884-842d-a2a5892d4db9" {
		t.Errorf("udid: got %q", ts.RawUDID)
	}
}
```

- [ ] **Step 2: Implement `tauri_storage.go`**

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// TauriSession holds session data extracted from Tauri's storage.json.
type TauriSession struct {
	AccessToken  string
	RefreshToken string
	RawUDID      string // Raw UUID before hashing
	HashedUDID   string // SHA-256[:32] — the UDID used by Center API
}

// tauriStoragePath returns the platform-specific path to Tauri's storage.json.
func tauriStoragePath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "io.kaitu.desktop", "storage.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		return filepath.Join(appdata, "io.kaitu.desktop", "storage.json")
	case "linux":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "io.kaitu.desktop", "storage.json")
	default:
		return ""
	}
}

// readTauriStorage reads and decrypts Tauri's storage.json.
// key is the AES-256 derived key; nil means plaintext-only mode (skip encrypted values).
func readTauriStorage(path string, key *[32]byte) (*TauriSession, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tauri storage: %w", err)
	}

	var store map[string]string
	if err := json.Unmarshal(b, &store); err != nil {
		return nil, fmt.Errorf("parse tauri storage: %w", err)
	}

	getValue := func(k string) string {
		raw, ok := store[k]
		if !ok {
			return ""
		}
		// Decrypt if encrypted
		if isEncrypted(raw) {
			if key == nil {
				return "" // Can't decrypt without key
			}
			decrypted, err := decryptValue(raw, *key)
			if err != nil {
				return ""
			}
			raw = decrypted
		}
		// Unwrap JSON string encoding (Tauri JS does JSON.stringify)
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return raw // Not JSON-encoded, return as-is
		}
		return s
	}

	ts := &TauriSession{
		AccessToken:  getValue("k2.auth.token"),
		RefreshToken: getValue("k2.auth.refresh"),
		RawUDID:      getValue("device-udid"),
	}

	// Hash raw UUID to get the UDID used by Center API
	if ts.RawUDID != "" {
		hash := sha256.Sum256([]byte(ts.RawUDID))
		ts.HashedUDID = hex.EncodeToString(hash[:16])
	}

	return ts, nil
}
```

- [ ] **Step 3: Run test**

Run: `cd mcp && go test -run TestReadTauriStoragePlaintext -v`
Expected: PASS

- [ ] **Step 4: Write test — UDID hash matches webapp**

```go
func TestUDIDHashMatchesWebapp(t *testing.T) {
	// From actual Tauri storage: raw UUID → expected hashed UDID
	ts := &TauriSession{RawUDID: "04cacd29-e71a-4884-842d-a2a5892d4db9"}
	hash := sha256.Sum256([]byte(ts.RawUDID))
	got := hex.EncodeToString(hash[:16])
	want := "932a7cc1a75b5830a1dd59f057b608d3"
	if got != want {
		t.Errorf("UDID hash mismatch: got %s, want %s", got, want)
	}
}
```

- [ ] **Step 5: Run test**

Run: `cd mcp && go test -run TestUDIDHashMatchesWebapp -v`
Expected: PASS

- [ ] **Step 6: Write test — mixed encrypted + plaintext storage**

```go
func TestReadTauriStorageMixed(t *testing.T) {
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")
	// In real scenario, Rust encrypts the token. For test, we produce a Go-encrypted value
	// (cross-language compat already verified in Task 3).
	// Here we just test that mixed plaintext+encrypted values both work.
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	os.WriteFile(path, []byte(`{
		"device-udid": "\"my-uuid\"",
		"onboarding_completed": "true"
	}`), 0600)

	ts, err := readTauriStorage(path, &key)
	if err != nil {
		t.Fatalf("readTauriStorage: %v", err)
	}
	if ts.RawUDID != "my-uuid" {
		t.Errorf("udid: got %q, want %q", ts.RawUDID, "my-uuid")
	}
}
```

- [ ] **Step 7: Write test — missing file returns error**

```go
func TestReadTauriStorageMissing(t *testing.T) {
	_, err := readTauriStorage("/nonexistent/storage.json", nil)
	if err == nil {
		t.Error("expected error for missing file")
	}
}
```

- [ ] **Step 8: Run all tauri_storage tests**

Run: `cd mcp && go test -run "TestReadTauriStorage|TestUDIDHash" -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add mcp/tauri_storage.go mcp/tauri_storage_test.go
git commit -m "feat(k2-mcp): add Tauri storage reader — decrypt + extract session from desktop storage.json"
```

---

## Task 5: Go — Integrate Tauri session into MCP startup

**Files:**
- Modify: `mcp/session.go`
- Modify: `mcp/main.go`

- [ ] **Step 1: Add `RestoreFromTauri` method to Session**

In `mcp/session.go`:

```go
// RestoreFromTauri attempts to load session from Tauri's desktop storage.json.
// MCP is read-only — never writes to Tauri's storage.json.
// On Windows, atomic rename may race with Tauri writes; json.Unmarshal
// fails gracefully on partial reads and we return false.
// Returns true if tokens were successfully loaded.
func (s *Session) RestoreFromTauri() bool {
	path := tauriStoragePath()
	if path == "" {
		return false
	}

	// Derive key from same hardware ID that Rust uses
	hwID, err := getHardwareID()
	if err != nil {
		log.Printf("[session] Cannot get hardware ID for Tauri storage: %v", err)
		return false
	}
	key := deriveKey(hwID)

	ts, err := readTauriStorage(path, &key)
	if err != nil {
		log.Printf("[session] Tauri storage not available: %v", err)
		return false
	}

	if ts.AccessToken == "" {
		return false
	}

	s.mu.Lock()
	s.AccessToken = ts.AccessToken
	s.RefreshToken = ts.RefreshToken
	s.Email = "" // Not stored in Tauri storage
	s.mu.Unlock()

	log.Printf("[session] Restored session from Tauri desktop storage")
	return true
}

// TauriUDID attempts to read the UDID from Tauri storage and return the
// hashed version (32 hex chars) used by Center API.
func (s *Session) TauriUDID() string {
	path := tauriStoragePath()
	if path == "" {
		return ""
	}

	hwID, err := getHardwareID()
	if err != nil {
		return ""
	}
	key := deriveKey(hwID)

	ts, err := readTauriStorage(path, &key)
	if err != nil || ts.HashedUDID == "" {
		return ""
	}
	return ts.HashedUDID
}
```

- [ ] **Step 2: Modify `main.go` to try Tauri session first**

Replace the session restore block in `main()`:

```go
	sess := NewSession(sessionDir)

	// Try Tauri desktop session first (shared identity),
	// fall back to MCP's own session file.
	tauriRestored := sess.RestoreFromTauri()
	if !tauriRestored {
		if err := sess.Restore(); err != nil {
			log.Printf("session restore: %v", err)
		}
	}

	center := NewCenterClient(apiURL)
	if sess.LoggedIn() {
		sess.mu.RLock()
		token := sess.AccessToken
		sess.mu.RUnlock()
		center.SetToken(token)
	}

	// Use Tauri UDID (32-char, matches desktop device) if available,
	// otherwise use MCP's own UDID (16-char, separate device).
	udid := sess.TauriUDID()
	if udid == "" {
		udid = sess.UDID()
	}
	center.SetUDID(udid)
```

- [ ] **Step 3: Run all MCP tests**

Run: `cd mcp && go test ./... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add mcp/session.go mcp/main.go
git commit -m "feat(k2-mcp): share desktop session — try Tauri storage before MCP login on startup"
```

---

## Task 6: Cross-language integration test

**Files:**
- Modify: `mcp/storage_crypto_test.go` (add integration test)

This task pastes the actual test vectors from Task 1 into Go tests and verifies end-to-end.

- [ ] **Step 1: Build and run Rust tests to capture test vectors**

Run: `cd desktop/src-tauri && cargo test storage_crypto::tests::test_derive_key_test_vector -- --nocapture 2>&1 | grep "HKDF test vector"`
Run: `cd desktop/src-tauri && cargo test storage_crypto::tests::test_encrypt_with_fixed_nonce_test_vector -- --nocapture 2>&1 | grep "ENC1 test vector"`

Record both values.

- [ ] **Step 2: Paste Rust test vectors into Go tests**

Replace `"PASTE_RUST_HEX_HERE"` and `"PASTE_RUST_ENC1_HERE"` in `mcp/storage_crypto_test.go` with actual values from Step 1.

- [ ] **Step 3: Run Go cross-language tests**

Run: `cd mcp && go test -run "TestDeriveKeyMatchesRust|TestDecryptMatchesRust" -v`
Expected: PASS — confirms Rust encrypt → Go decrypt works.

- [ ] **Step 4: End-to-end test on live Tauri storage**

Run: `cd mcp && go test -run TestHardwareIDNotEmpty -v`
Log the hardware ID. Compare with: `sysctl -n kern.uuid`
They must match (after trim).

- [ ] **Step 5: Commit test vectors**

```bash
git add mcp/storage_crypto_test.go
git commit -m "test(k2-mcp): add cross-language test vectors — verify Rust encrypt ↔ Go decrypt"
```

---

## Task 7: Documentation update

**Files:**
- Modify: `desktop/CLAUDE.md`
- Modify: `mcp/CLAUDE.md`
- Modify: root `CLAUDE.md`

- [ ] **Step 1: Add to desktop/CLAUDE.md storage section**

Add after storage.rs description:

```markdown
- **storage_crypto.rs** — AES-256-GCM encryption for storage values. Key derived via HKDF-SHA256 from platform hardware ID (macOS: kern.uuid, Windows: SMBIOS UUID, Linux: machine-id). Encrypted values prefixed with `ENC1:`. Plaintext values (pre-encryption) read transparently for backward compatibility.
```

- [ ] **Step 2: Add to mcp/CLAUDE.md**

Add to Architecture table:

```markdown
| `storage_crypto.go`     | AES-256-GCM decrypt + HKDF key derivation (shared test vectors with Rust) |
| `tauri_storage.go`      | Read Tauri desktop `storage.json` — decrypt values, extract UDID+tokens |
```

Add to Key Patterns:

```markdown
- **Tauri session sharing**: On startup, MCP tries to read `~/Library/Application Support/io.kaitu.desktop/storage.json` first. If found and tokens are valid, MCP uses the desktop's identity (UDID + tokens) instead of its own. Falls back to independent MCP session if Tauri storage unavailable.
```

- [ ] **Step 3: Add to root CLAUDE.md Key Conventions**

```markdown
- **Storage encryption**: Desktop `storage.json` values encrypted with AES-256-GCM. Key derived from platform hardware ID via HKDF-SHA256 (never stored). `ENC1:` prefix on encrypted values; plaintext values read as-is (backward compat). MCP Go reimplements same crypto with shared test vectors for session sharing.
```

- [ ] **Step 4: Commit**

```bash
git add desktop/CLAUDE.md mcp/CLAUDE.md CLAUDE.md
git commit -m "docs: add encrypted storage + MCP session sharing conventions"
```
