//! Update Channel Management
//!
//! Persists the user's update channel preference (stable/beta) to disk.
//! The updater reads this on each check cycle to determine which endpoints to use.
//!
//! Two read paths:
//! - `get_channel(app)` — uses Tauri's AppHandle for normal runtime use
//! - `get_channel_early()` — uses `dirs` crate directly, for pre-AppHandle contexts
//!   (e.g., log plugin configuration in builder chain before `.setup()`)

use std::fs;
use std::path::PathBuf;
use tauri::AppHandle;
use tauri::Manager;
use url::Url;

const CHANNEL_FILE: &str = "update-channel";
const STABLE: &str = "stable";
const BETA: &str = "beta";

/// Tauri app identifier — must match `identifier` in tauri.conf.json.
/// Used by `get_channel_early()` to resolve the app data directory without AppHandle.
const APP_IDENTIFIER: &str = "io.kaitu.desktop";

// Stable endpoints (same as tauri.conf.json)
const STABLE_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/cloudfront.latest.json",
    "https://d0.all7.cc/kaitu/desktop/d0.latest.json",
];

// Beta endpoints (beta/ subdirectory)
const BETA_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/beta/cloudfront.latest.json",
    "https://d0.all7.cc/kaitu/desktop/beta/d0.latest.json",
];

fn channel_file_path(app: &AppHandle) -> Option<PathBuf> {
    app.path().app_data_dir().ok().map(|dir| dir.join(CHANNEL_FILE))
}

/// Read channel from disk without AppHandle (for pre-setup use).
/// Uses `dirs::data_dir()` + hardcoded app identifier to match Tauri's `app_data_dir()`.
/// Platform resolution: macOS ~/Library/Application Support, Windows %APPDATA%, Linux $XDG_DATA_HOME.
pub fn get_channel_early() -> String {
    let path = dirs::data_dir().map(|d| d.join(APP_IDENTIFIER).join(CHANNEL_FILE));
    match path {
        Some(p) => read_channel_from_file(&p),
        None => STABLE.to_string(),
    }
}

/// Whether the channel file exists on disk (user has explicitly set a channel preference).
pub fn has_channel_preference(app: &AppHandle) -> bool {
    channel_file_path(app)
        .map(|p| p.exists())
        .unwrap_or(false)
}

/// Whether the current channel is beta (pre-AppHandle version).
pub fn is_beta_early() -> bool {
    get_channel_early() == BETA
}

fn read_channel_from_file(path: &PathBuf) -> String {
    match fs::read_to_string(path) {
        Ok(content) => {
            let ch = content.trim();
            if ch == BETA {
                BETA.to_string()
            } else {
                STABLE.to_string()
            }
        }
        Err(_) => STABLE.to_string(),
    }
}

/// Read the current update channel. Returns "stable" if file doesn't exist or is unreadable.
pub fn get_channel(app: &AppHandle) -> String {
    let Some(path) = channel_file_path(app) else {
        return STABLE.to_string();
    };
    read_channel_from_file(&path)
}

/// Save the update channel to disk.
pub fn save_channel(app: &AppHandle, channel: &str) -> Result<(), String> {
    let path = channel_file_path(app).ok_or("Cannot resolve app data directory")?;

    // Ensure parent directory exists
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("Failed to create app data dir: {}", e))?;
    }

    let ch = if channel == BETA { BETA } else { STABLE };
    fs::write(&path, ch).map_err(|e| format!("Failed to write channel file: {}", e))?;

    log::info!("[channel] Saved update channel: {}", ch);
    Ok(())
}

/// Build endpoint URLs for the given channel.
pub fn endpoints_for_channel(channel: &str) -> Result<Vec<Url>, String> {
    let raw = if channel == BETA {
        BETA_ENDPOINTS
    } else {
        STABLE_ENDPOINTS
    };

    raw.iter()
        .map(|s| Url::parse(s).map_err(|e| format!("Invalid endpoint URL: {}", e)))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_endpoints_for_stable() {
        let urls = endpoints_for_channel("stable").unwrap();
        assert_eq!(urls.len(), 2);
        assert!(urls[0].as_str().contains("cloudfront.latest.json"));
        assert!(!urls[0].as_str().contains("/beta/"));
    }

    #[test]
    fn test_endpoints_for_beta() {
        let urls = endpoints_for_channel("beta").unwrap();
        assert_eq!(urls.len(), 2);
        assert!(urls[0].as_str().contains("/beta/cloudfront.latest.json"));
    }

    #[test]
    fn test_endpoints_unknown_defaults_to_stable() {
        let urls = endpoints_for_channel("unknown").unwrap();
        assert!(!urls[0].as_str().contains("/beta/"));
    }

    #[test]
    fn test_read_channel_from_file_beta() {
        let dir = std::env::temp_dir().join("k2app-test-channel-beta");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("update-channel");
        fs::write(&path, "beta").unwrap();
        assert_eq!(read_channel_from_file(&path), "beta");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_read_channel_from_file_stable() {
        let dir = std::env::temp_dir().join("k2app-test-channel-stable");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("update-channel");
        fs::write(&path, "stable").unwrap();
        assert_eq!(read_channel_from_file(&path), "stable");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_read_channel_from_file_unknown_defaults_stable() {
        let dir = std::env::temp_dir().join("k2app-test-channel-unknown");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("update-channel");
        fs::write(&path, "unknown").unwrap();
        assert_eq!(read_channel_from_file(&path), "stable");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_read_channel_from_file_missing() {
        let path = std::env::temp_dir().join("k2app-test-nonexistent-channel");
        assert_eq!(read_channel_from_file(&path), "stable");
    }

    #[test]
    fn test_read_channel_from_file_whitespace() {
        let dir = std::env::temp_dir().join("k2app-test-channel-ws");
        let _ = fs::create_dir_all(&dir);
        let path = dir.join("update-channel");
        fs::write(&path, "  beta\n").unwrap();
        assert_eq!(read_channel_from_file(&path), "beta");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_get_channel_early_no_crash() {
        // Should not crash even without the channel file
        let ch = get_channel_early();
        assert!(ch == "stable" || ch == "beta");
    }

    #[test]
    fn test_is_beta_early_returns_bool() {
        // Just verify it doesn't crash
        let _ = is_beta_early();
    }
}
