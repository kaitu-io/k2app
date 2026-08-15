//! Update Channel Management
//!
//! Persists the user's update channel preference (stable/beta) to disk.
//! The updater reads this on each check cycle to determine which endpoints to use.

use std::fs;
use std::path::PathBuf;
use tauri::AppHandle;
use tauri::Manager;
use url::Url;

const CHANNEL_FILE: &str = "update-channel";
const STABLE: &str = "stable";
const BETA: &str = "beta";

// Stable/Beta endpoints (same as the active tauri.conf). Brand is baked at
// compile time via cfg(brand_overleap) so the other brand's URLs never enter
// the binary (desktop purity guard greps for them).
#[cfg(not(brand_overleap))]
const STABLE_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/cloudfront.latest.json",
    "https://d0.all7.cc/kaitu/desktop/d0.latest.json",
];
#[cfg(not(brand_overleap))]
const BETA_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/beta/cloudfront.latest.json",
    "https://d0.all7.cc/kaitu/desktop/beta/d0.latest.json",
];
#[cfg(brand_overleap)]
const STABLE_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/cloudfront.latest.json",
    "https://d0.all7.cc/overleap/desktop/d0.latest.json",
];
#[cfg(brand_overleap)]
const BETA_ENDPOINTS: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/beta/cloudfront.latest.json",
    "https://d0.all7.cc/overleap/desktop/beta/d0.latest.json",
];

// Web OTA (UI bundle) CDN bases — same brand fork discipline as the
// updater endpoints above. Manifest path shape mirrors mobile
// K2PluginUtils.webManifestEndpoints: {base}/web[/beta]/latest.json.
#[cfg(not(brand_overleap))]
const WEB_OTA_BASES: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu",
    "https://d0.all7.cc/kaitu",
];
#[cfg(brand_overleap)]
const WEB_OTA_BASES: &[&str] = &[
    "https://d13jc1jqzlg4yt.cloudfront.net/overleap",
    "https://d0.all7.cc/overleap",
];

/// One Web-OTA source: where to fetch the manifest, and the base that
/// relative manifest `url` fields resolve against. download_base is the
/// manifest's own directory — the authoritative semantics of the shipped
/// mobile native (K2Plugin.kt:1013 `baseURL = endpoint.substringBeforeLast("/")`),
/// so a manifest url like "0.4.9.1300/web.zip" resolves under web/ (or
/// web/beta/, where CI mirrors the zips for the beta channel).
pub struct WebOtaSource {
    pub manifest_url: String,
    pub download_base: String,
}

fn source_for(base: &str, manifest_path: &str) -> WebOtaSource {
    let manifest_url = format!("{}/{}", base.trim_end_matches('/'), manifest_path);
    let download_base = manifest_url
        .rsplit_once('/')
        .map(|(dir, _file)| dir.to_string())
        .unwrap_or_else(|| manifest_url.clone());
    WebOtaSource {
        manifest_url,
        download_base,
    }
}

/// Brand CDN sources for the given channel.
pub fn web_ota_sources(channel: &str) -> Vec<WebOtaSource> {
    ota_sources_for(channel, None)
}

/// Pure variant with an optional base override (K2_WEB_OTA_BASE env, UAT only —
/// sha256+minisign gates still apply, so this cannot inject unsigned bundles).
pub fn ota_sources_for(channel: &str, override_base: Option<String>) -> Vec<WebOtaSource> {
    let path = if channel == BETA {
        "web/beta/latest.json"
    } else {
        "web/latest.json"
    };
    if let Some(base) = override_base {
        return vec![source_for(&base, path)];
    }
    WEB_OTA_BASES.iter().map(|base| source_for(base, path)).collect()
}

fn channel_file_path(app: &AppHandle) -> Option<PathBuf> {
    app.path().app_data_dir().ok().map(|dir| dir.join(CHANNEL_FILE))
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
    fn test_endpoints_match_brand() {
        let urls = endpoints_for_channel("stable").unwrap();
        #[cfg(brand_overleap)]
        {
            assert!(urls[0].as_str().contains("/overleap/desktop/"));
            assert!(!urls.iter().any(|u| u.as_str().contains("/kaitu/")));
        }
        #[cfg(not(brand_overleap))]
        {
            assert!(urls[0].as_str().contains("/kaitu/desktop/"));
            assert!(!urls.iter().any(|u| u.as_str().contains("/overleap/")));
        }
    }

    #[test]
    fn test_web_ota_sources_stable_and_beta() {
        let stable = web_ota_sources("stable");
        assert_eq!(stable.len(), 2);
        assert!(stable[0].manifest_url.ends_with("/web/latest.json"));
        assert!(!stable[0].manifest_url.contains("/beta/"));
        // download_base = manifest directory (mobile fetchManifest substringBeforeLast("/"))
        assert!(stable[0].download_base.ends_with("/web"));
        assert_eq!(
            stable[0].manifest_url,
            format!("{}/latest.json", stable[0].download_base)
        );

        let beta = web_ota_sources("beta");
        assert!(beta[0].manifest_url.ends_with("/web/beta/latest.json"));
        assert!(beta[0].download_base.ends_with("/web/beta"));
        assert_eq!(
            beta[0].manifest_url,
            format!("{}/latest.json", beta[0].download_base)
        );
    }

    #[test]
    fn test_web_ota_sources_match_brand() {
        let urls = web_ota_sources("stable");
        #[cfg(brand_overleap)]
        {
            assert!(urls.iter().all(|s| s.manifest_url.contains("/overleap/")));
            assert!(!urls.iter().any(|s| s.manifest_url.contains("/kaitu/")));
        }
        #[cfg(not(brand_overleap))]
        {
            assert!(urls.iter().all(|s| s.manifest_url.contains("/kaitu/")));
            assert!(!urls.iter().any(|s| s.manifest_url.contains("/overleap/")));
        }
    }

    #[test]
    fn test_ota_sources_override() {
        let v = ota_sources_for("stable", Some("http://127.0.0.1:8899/".to_string()));
        assert_eq!(v.len(), 1);
        assert_eq!(v[0].manifest_url, "http://127.0.0.1:8899/web/latest.json");
        assert_eq!(v[0].download_base, "http://127.0.0.1:8899/web");
        // beta channel still honored under override
        let vb = ota_sources_for("beta", Some("http://127.0.0.1:8899".to_string()));
        assert_eq!(vb[0].manifest_url, "http://127.0.0.1:8899/web/beta/latest.json");
        assert_eq!(vb[0].download_base, "http://127.0.0.1:8899/web/beta");
        // no override → brand CDN list
        assert_eq!(ota_sources_for("stable", None).len(), 2);
    }

}
