//! AES-256-GCM encryption for storage values.
//!
//! Derives an encryption key from the machine's hardware ID using HKDF-SHA256,
//! then encrypts/decrypts storage values with AES-256-GCM. Encrypted values are
//! prefixed with `ENC1:` followed by base64(nonce || ciphertext || tag).
//!
//! # Threat model / intended scope
//!
//! This module provides **落盘混淆 + 硬件特征绑定**, *not* protection against
//! a local attacker running as the same user:
//!
//! - A same-user process can trivially recompute the key by running `ioreg` /
//!   reading the registry / reading `/etc/machine-id` itself. We do **not**
//!   try to defeat local malware.
//! - The protection target is "storage.json is copied off-machine as a single
//!   file": on another machine the hardware ID is different, HKDF derives a
//!   different key, decryption fails.
//! - Losing access after logic-board swap / OS reinstall / VM clone is
//!   acceptable — the user simply re-logs in.
//!
//! # Why `machine-uid` and not `sysctl kern.uuid`
//!
//! The pre-v0.4.1 UDID code used `sysctl -n kern.uuid` and hit a production
//! collision between two Macs (see commit `d4ebdd6`). Root cause: **`kern.uuid`
//! is not the hardware UUID.** xnu generates it via
//! `uuid_create_md5_from_name(namespace, hostname + boot_args)` — i.e. it is
//! a UUIDv3 derived from the hostname. Two machines with the same default
//! hostname (corporate images, fresh installs, or VM clones) will produce
//! the same `kern.uuid`.
//!
//! `machine-uid` reads the correct firmware-level source on each platform:
//! - **macOS**: `ioreg -rd1 -c IOPlatformExpertDevice` → `IOPlatformUUID`
//!   (set at manufacture by firmware / SMC / Secure Enclave)
//! - **Windows**: `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`
//!   (written once at OS install by CryptoAPI; native registry read, no WMI)
//! - **Linux**: `/var/lib/dbus/machine-id` → `/etc/machine-id`
//!   (systemd-machine-id-setup, stable for the life of the install)
//!
//! All three are stable across reboots, OS minor updates, and app reinstalls.
//! They only change on legitimate "new machine" events (hardware swap,
//! OS reinstall, VM clone), which is the desired behavior.
//!
//! # Empty-string fallback
//!
//! If `machine-uid::get()` fails on an unusual system (corrupted binaries,
//! locked-down registry, no systemd machine-id), `get_hardware_id()` logs
//! an `error!` and returns an empty string. We intentionally continue rather
//! than panic so app startup is never blocked on an infrastructural lookup.
//! The side-effect is that multiple devices hitting the fallback would share
//! the same derived key. CI has `test_machine_uid_not_empty` gating normal
//! environments, so this path should not fire in production.

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
/// Sources (firmware-level, see module doc for rationale vs `kern.uuid`):
/// - macOS: `IOPlatformUUID` via `ioreg -rd1 -c IOPlatformExpertDevice`
/// - Windows: Registry `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`
/// - Linux: `/var/lib/dbus/machine-id` → `/etc/machine-id`
///
/// On lookup failure returns `""` (see module-level "Empty-string fallback").
fn get_hardware_id() -> String {
    match machine_uid::get() {
        Ok(id) if !id.is_empty() => id,
        Ok(_) => {
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
