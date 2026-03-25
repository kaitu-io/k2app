//! App-private key-value storage.
//!
//! Persists a JSON file (`storage.json`) in the Tauri app data directory.
//! Values are opaque JSON strings — the JS layer handles serialization.
//!
//! Uses atomic write (write to `.tmp`, then `fs::rename`) to prevent
//! corruption on crash. single-instance plugin guarantees no concurrent
//! writers across processes.

use std::collections::HashMap;
use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;

use tauri::AppHandle;
use tauri::Manager;

const STORAGE_FILE: &str = "storage.json";

/// Tauri managed state — in-memory mirror of storage.json.
pub struct StorageState {
    pub data: Mutex<HashMap<String, String>>,
    pub path: Mutex<Option<PathBuf>>,
    pub enc_key: [u8; 32],
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

/// Initialize storage: resolve path, load from disk.
/// Called in `.setup()` where AppHandle is available.
pub fn init(app: &AppHandle, state: &StorageState) {
    let Some(dir) = app.path().app_data_dir().ok() else {
        log::warn!("[storage] Cannot resolve app data directory");
        return;
    };
    if let Err(e) = fs::create_dir_all(&dir) {
        log::warn!("[storage] Cannot create app data dir: {}", e);
        return;
    }
    let file_path = dir.join(STORAGE_FILE);
    let data = load_from_disk(&file_path);
    *state.data.lock().unwrap() = data;
    *state.path.lock().unwrap() = Some(file_path);
    log::info!("[storage] Initialized");
}

fn load_from_disk(path: &PathBuf) -> HashMap<String, String> {
    match fs::read_to_string(path) {
        Ok(content) => serde_json::from_str(&content).unwrap_or_default(),
        Err(_) => HashMap::new(),
    }
}

fn persist_to_disk(path: &PathBuf, data: &HashMap<String, String>) {
    let tmp = path.with_extension("json.tmp");
    let json = match serde_json::to_string_pretty(data) {
        Ok(j) => j,
        Err(e) => {
            log::error!("[storage] Failed to serialize: {}", e);
            return;
        }
    };
    if let Err(e) = fs::write(&tmp, &json) {
        log::error!("[storage] Failed to write tmp file: {}", e);
        return;
    }
    if let Err(e) = fs::rename(&tmp, path) {
        log::error!("[storage] Failed to rename tmp → storage.json: {}", e);
    }
}

#[tauri::command]
pub fn storage_get(key: String, state: tauri::State<'_, StorageState>) -> Option<String> {
    let data = state.data.lock().unwrap();
    let value = data.get(&key)?;
    if crate::storage_crypto::is_encrypted(value) {
        match crate::storage_crypto::decrypt_value(value, &state.enc_key) {
            Some(plaintext) => Some(plaintext),
            None => {
                log::warn!("[storage] Failed to decrypt value for key '{}', returning None", key);
                None
            }
        }
    } else {
        // Backward compatibility: return plaintext as-is
        Some(value.clone())
    }
}

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

#[tauri::command]
pub fn storage_remove(key: String, state: tauri::State<'_, StorageState>) {
    let mut data = state.data.lock().unwrap();
    data.remove(&key);
    let path_clone = state.path.lock().unwrap().clone();
    if let Some(ref p) = path_clone {
        persist_to_disk(p, &data);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_load_missing_file() {
        let path = std::env::temp_dir().join("k2app-test-storage-missing.json");
        let data = load_from_disk(&path);
        assert!(data.is_empty());
    }

    #[test]
    fn test_load_valid_file() {
        let dir = std::env::temp_dir().join("k2app-test-storage-valid");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");
        fs::write(&path, r#"{"key1":"val1","key2":"val2"}"#).unwrap();
        let data = load_from_disk(&path);
        assert_eq!(data.get("key1").unwrap(), "val1");
        assert_eq!(data.get("key2").unwrap(), "val2");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_load_corrupt_file() {
        let dir = std::env::temp_dir().join("k2app-test-storage-corrupt");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");
        fs::write(&path, "not valid json!!!").unwrap();
        let data = load_from_disk(&path);
        assert!(data.is_empty());
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_persist_and_reload() {
        let dir = std::env::temp_dir().join("k2app-test-storage-persist");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");

        let mut data = HashMap::new();
        data.insert("token".to_string(), "abc123".to_string());
        data.insert("udid".to_string(), "xyz".to_string());
        persist_to_disk(&path, &data);

        let loaded = load_from_disk(&path);
        assert_eq!(loaded.get("token").unwrap(), "abc123");
        assert_eq!(loaded.get("udid").unwrap(), "xyz");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_persist_atomic_no_tmp_leftover() {
        let dir = std::env::temp_dir().join("k2app-test-storage-atomic");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");

        let data = HashMap::new();
        persist_to_disk(&path, &data);

        // tmp file should be gone after rename
        let tmp = path.with_extension("json.tmp");
        assert!(!tmp.exists());
        assert!(path.exists());
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_encrypted_storage_roundtrip() {
        let dir = std::env::temp_dir().join("k2app-test-storage-enc-roundtrip");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");

        let key = crate::storage_crypto::derive_key("test-enc-roundtrip");

        // Encrypt and persist
        let plaintext = "\"my-secret-token\"";
        let encrypted = crate::storage_crypto::encrypt_value(plaintext, &key);
        let mut data = HashMap::new();
        data.insert("token".to_string(), encrypted);
        persist_to_disk(&path, &data);

        // Reload from disk and decrypt
        let loaded = load_from_disk(&path);
        let stored = loaded.get("token").unwrap();
        assert!(crate::storage_crypto::is_encrypted(stored));
        let decrypted = crate::storage_crypto::decrypt_value(stored, &key);
        assert_eq!(decrypted, Some(plaintext.to_string()));

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_backward_compat_plaintext_read() {
        // Simulate pre-encryption plaintext data already on disk
        let dir = std::env::temp_dir().join("k2app-test-storage-compat");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("storage.json");
        fs::write(&path, r#"{"old_key":"plain-value","other":"data"}"#).unwrap();

        let loaded = load_from_disk(&path);
        let value = loaded.get("old_key").unwrap();

        // Not encrypted — should be returned as-is
        assert!(!crate::storage_crypto::is_encrypted(value));
        assert_eq!(value, "plain-value");

        let _ = fs::remove_dir_all(&dir);
    }
}
