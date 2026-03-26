//! AES-256-GCM encryption for storage values.
//!
//! Derives an encryption key from the machine's hardware ID using HKDF-SHA256,
//! then encrypts/decrypts storage values with AES-256-GCM. Encrypted values are
//! prefixed with `ENC1:` followed by base64(nonce || ciphertext || tag).
//!
//! Hardware ID sourced from `machine-uid` crate (same as Go `denisbrodbeck/machineid`):
//! - macOS: IOPlatformUUID via `ioreg`
//! - Windows: Registry `MachineGuid` (no PowerShell, instant)
//! - Linux: `/var/lib/dbus/machine-id` → `/etc/machine-id`

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use base64::Engine as _;
use hkdf::Hkdf;
use sha2::Sha256;

const ENC_PREFIX: &str = "ENC1:";
const HKDF_SALT: &[u8] = b"kaitu-desktop-storage-v1";
const HKDF_INFO: &[u8] = b"aes-256-gcm-key";

/// Derive an AES-256 key from a hardware ID string using HKDF-SHA256.
pub(crate) fn derive_key(hardware_id: &str) -> [u8; 32] {
    let hk = Hkdf::<Sha256>::new(Some(HKDF_SALT), hardware_id.as_bytes());
    let mut key = [0u8; 32];
    hk.expand(HKDF_INFO, &mut key)
        .expect("HKDF expand should not fail for 32 bytes");
    key
}

/// Derive an AES-256 key from this machine's hardware ID.
pub(crate) fn derive_key_from_hardware() -> [u8; 32] {
    let hw_id = get_hardware_id();
    derive_key(&hw_id)
}

/// Encrypt a plaintext value. Returns `"ENC1:" + base64(nonce || ciphertext || tag)`.
pub(crate) fn encrypt_value(plaintext: &str, key: &[u8; 32]) -> String {
    let mut nonce_bytes = [0u8; 12];
    getrandom::getrandom(&mut nonce_bytes).expect("getrandom failed");
    encrypt_value_with_nonce(plaintext, key, &nonce_bytes)
}

/// Decrypt an encrypted value. Returns `None` on any failure (wrong key, corrupt data, etc.).
/// Non-encrypted values (no `ENC1:` prefix) are not handled here — callers should check
/// `is_encrypted()` first.
pub(crate) fn decrypt_value(encrypted: &str, key: &[u8; 32]) -> Option<String> {
    let encoded = encrypted.strip_prefix(ENC_PREFIX)?;
    let raw = base64::engine::general_purpose::STANDARD
        .decode(encoded)
        .ok()?;
    if raw.len() < 12 + 16 {
        return None; // too short for nonce + tag
    }
    let (nonce_bytes, ciphertext_with_tag) = raw.split_at(12);
    let nonce = Nonce::from_slice(nonce_bytes);
    let cipher = Aes256Gcm::new_from_slice(key).ok()?;
    let plaintext_bytes = cipher.decrypt(nonce, ciphertext_with_tag).ok()?;
    String::from_utf8(plaintext_bytes).ok()
}

/// Check if a value is encrypted (has the `ENC1:` prefix).
pub(crate) fn is_encrypted(value: &str) -> bool {
    value.starts_with(ENC_PREFIX)
}

/// Encrypt with a specific nonce (for deterministic test vectors only).
fn encrypt_value_with_nonce(plaintext: &str, key: &[u8; 32], nonce_bytes: &[u8; 12]) -> String {
    let nonce = Nonce::from_slice(nonce_bytes);
    let cipher =
        Aes256Gcm::new_from_slice(key).expect("AES-256-GCM key init should not fail for 32 bytes");
    let ciphertext_with_tag = cipher
        .encrypt(nonce, plaintext.as_bytes())
        .expect("AES-256-GCM encryption should not fail");

    let mut combined = Vec::with_capacity(12 + ciphertext_with_tag.len());
    combined.extend_from_slice(nonce_bytes);
    combined.extend_from_slice(&ciphertext_with_tag);

    let encoded = base64::engine::general_purpose::STANDARD.encode(&combined);
    format!("{}{}", ENC_PREFIX, encoded)
}

/// Get a stable hardware identifier for this machine via `machine-uid` crate.
///
/// Sources (same as Go `denisbrodbeck/machineid`):
/// - macOS: IOPlatformUUID via `ioreg -rd1 -c IOPlatformExpertDevice`
/// - Windows: Registry `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`
/// - Linux: `/var/lib/dbus/machine-id` → `/etc/machine-id`
fn get_hardware_id() -> String {
    match machine_uid::get() {
        Ok(id) if !id.is_empty() => id,
        Ok(_) => {
            // KNOWN LIMITATION: empty ID produces a deterministic key shared across all
            // devices where hardware ID lookup fails. Encryption still prevents casual
            // file reads but not cross-device decryption. We continue rather than fail
            // to avoid blocking app startup.
            log::error!("[storage_crypto] machine-uid returned empty ID — encryption key is not machine-specific");
            String::new()
        }
        Err(e) => {
            log::error!("[storage_crypto] machine-uid failed: {} — encryption key is not machine-specific", e);
            String::new()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_get_hardware_id_not_empty() {
        let id = get_hardware_id();
        assert!(!id.is_empty(), "Hardware ID should not be empty");
        println!("[test] hardware_id = {}", id);
    }

    #[test]
    fn test_derive_key_deterministic() {
        let key1 = derive_key("test-hardware-id");
        let key2 = derive_key("test-hardware-id");
        assert_eq!(key1, key2, "Same input must produce same key");
    }

    #[test]
    fn test_derive_key_test_vector() {
        // Fixed hardware ID for cross-language verification with Go
        let key = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        let hex = key.iter().map(|b| format!("{:02x}", b)).collect::<String>();
        println!("[test_vector] HKDF key hex = {}", hex);
    }

    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let key = derive_key("roundtrip-test-id");
        let plaintext = "\"secret-token-value\"";
        let encrypted = encrypt_value(plaintext, &key);
        assert!(is_encrypted(&encrypted));
        let decrypted = decrypt_value(&encrypted, &key);
        assert_eq!(decrypted, Some(plaintext.to_string()));
    }

    #[test]
    fn test_decrypt_wrong_key_fails() {
        let key1 = derive_key("key-one");
        let key2 = derive_key("key-two");
        let encrypted = encrypt_value("secret", &key1);
        let result = decrypt_value(&encrypted, &key2);
        assert_eq!(result, None, "Decryption with wrong key must return None");
    }

    #[test]
    fn test_encrypt_with_fixed_nonce_test_vector() {
        // Fixed inputs for cross-language verification with Go
        let key = derive_key("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B");
        let nonce: [u8; 12] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11];
        let plaintext = "\"hello-from-rust\"";
        let encrypted = encrypt_value_with_nonce(plaintext, &key, &nonce);
        println!("[test_vector] ENC1 output = {}", encrypted);
    }

    // -----------------------------------------------------------------------
    // Platform hardware ID gate tests via machine-uid crate.
    // CI runs cargo test on macOS, Windows, Linux.
    // -----------------------------------------------------------------------

    #[test]
    fn test_machine_uid_not_empty() {
        let id = machine_uid::get().expect("machine-uid must succeed on CI");
        assert!(!id.is_empty(), "machine-uid must return a non-empty ID");
        println!("[platform_gate] machine-uid = {}", id);
    }

    #[test]
    #[cfg(target_os = "macos")]
    fn test_machine_uid_macos_format() {
        let id = machine_uid::get().unwrap();
        // macOS IOPlatformUUID: 36 chars, UUID format (8-4-4-4-12)
        assert_eq!(id.len(), 36, "macOS IOPlatformUUID must be 36 chars, got: {}", id);
        assert_eq!(id.chars().filter(|c| *c == '-').count(), 4,
            "macOS IOPlatformUUID must have 4 dashes, got: {}", id);
    }

    #[test]
    #[cfg(target_os = "windows")]
    fn test_machine_uid_windows_format() {
        let id = machine_uid::get().unwrap();
        // Windows MachineGuid: 36 chars, GUID format
        assert_eq!(id.len(), 36, "Windows MachineGuid must be 36 chars, got: {}", id);
        assert_ne!(id, "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
            "Windows MachineGuid must not be all-F sentinel");
    }

    #[test]
    #[cfg(target_os = "linux")]
    fn test_machine_uid_linux_format() {
        let id = machine_uid::get().unwrap();
        // Linux /var/lib/dbus/machine-id or /etc/machine-id: 32 hex chars
        assert_eq!(id.len(), 32, "Linux machine-id must be 32 hex chars, got len={}: {}", id.len(), id);
        assert!(id.chars().all(|c| c.is_ascii_hexdigit()),
            "Linux machine-id must be hex only, got: {}", id);
    }

    /// Output hardware ID to stdout for CI cross-language gate.
    /// CI step captures this and compares with Go `denisbrodbeck/machineid` output.
    #[test]
    fn test_hardware_id_cross_lang_gate() {
        let id = get_hardware_id();
        assert!(!id.is_empty(), "hardware ID must not be empty for cross-lang gate");
        // CI parses this line: CROSS_LANG_GATE_HWID=<value>
        println!("CROSS_LANG_GATE_HWID={}", id);
    }
}
