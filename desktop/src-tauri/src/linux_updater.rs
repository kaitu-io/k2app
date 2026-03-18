//! Linux tar.gz Updater
//!
//! Replaces tauri-plugin-updater on Linux. Downloads tar.gz bundles (~6-8MB vs 85MB AppImage),
//! verifies minisign signatures, and extracts to /opt/kaitu/ via pkexec.
//!
//! Flow:
//! 1. Fetch latest.json from CDN endpoints (same as tauri-plugin-updater)
//! 2. Compare versions using semver
//! 3. Download tar.gz bundle to temp location
//! 4. Verify minisign signature
//! 5. Extract via pkexec to /opt/kaitu/ (two-phase staging)
//! 6. Relaunch via helper process that polls PID then nohup

use base64::Engine as _;
use minisign_verify::{PublicKey, Signature};
use semver::Version;
use serde::Deserialize;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::sync::atomic::Ordering;
use tauri::AppHandle;
use tauri::Emitter;

use crate::channel;
use crate::updater::{UpdateInfo, CHANNEL_SWITCH_PENDING, INSTALL_FAILED, UPDATE_INFO, UPDATE_READY};

/// Minisign public key for verifying update signatures
const MINISIGN_PUBLIC_KEY: &str = "RWSD3s7XX1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE";

/// latest.json schema (Tauri-compatible subset)
#[derive(Debug, Deserialize)]
struct LatestJson {
    version: String,
    notes: Option<String>,
    platforms: std::collections::HashMap<String, PlatformEntry>,
}

#[derive(Debug, Deserialize)]
struct PlatformEntry {
    url: String,
    signature: String,
}

/// Determine the platform key for latest.json lookup
fn platform_key() -> &'static str {
    #[cfg(target_arch = "x86_64")]
    {
        "linux-x86_64"
    }
    #[cfg(target_arch = "aarch64")]
    {
        "linux-aarch64"
    }
    #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
    {
        "linux-x86_64" // fallback
    }
}

/// Fetch and parse latest.json from CDN endpoints (tries each in order)
async fn fetch_latest_json(endpoints: &[url::Url]) -> Option<LatestJson> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .ok()?;

    for endpoint in endpoints {
        log::debug!("[linux-updater] Trying endpoint: {}", endpoint.as_str());
        match client.get(endpoint.as_str()).send().await {
            Ok(resp) if resp.status().is_success() => {
                match resp.json::<LatestJson>().await {
                    Ok(latest) => {
                        log::info!(
                            "[linux-updater] Got latest.json: version={}",
                            latest.version
                        );
                        return Some(latest);
                    }
                    Err(e) => {
                        log::warn!("[linux-updater] Failed to parse latest.json from {}: {}", endpoint, e);
                    }
                }
            }
            Ok(resp) => {
                log::warn!(
                    "[linux-updater] Endpoint {} returned status {}",
                    endpoint,
                    resp.status()
                );
            }
            Err(e) => {
                log::warn!("[linux-updater] Failed to fetch {}: {}", endpoint, e);
            }
        }
    }

    log::error!("[linux-updater] All endpoints failed");
    None
}

/// Check for updates and download if available.
/// Called from updater.rs check_download_and_install on Linux.
pub async fn check_and_download(app: &AppHandle, force_downgrade: bool) {
    let ch = channel::get_channel(app);
    let endpoints = match channel::endpoints_for_channel(&ch) {
        Ok(eps) => eps,
        Err(e) => {
            log::error!("[linux-updater] Failed to build endpoints for channel {}: {}", ch, e);
            return;
        }
    };

    let current_version = app.package_info().version.to_string();
    let force_different = force_downgrade && ch == "stable" && current_version.contains("-beta");

    log::info!(
        "[linux-updater] Checking for updates (channel={}, current={}, force={})",
        ch, current_version, force_different
    );

    let latest = match fetch_latest_json(&endpoints).await {
        Some(l) => l,
        None => return,
    };

    // Version comparison
    let remote_version = &latest.version;
    let should_update = if force_different {
        remote_version != &current_version
    } else {
        match (
            Version::parse(&current_version),
            Version::parse(remote_version),
        ) {
            (Ok(current), Ok(remote)) => remote > current,
            _ => {
                log::warn!(
                    "[linux-updater] Failed to parse versions: current={}, remote={}",
                    current_version,
                    remote_version
                );
                false
            }
        }
    };

    if !should_update {
        log::info!("[linux-updater] No update needed (current={}, remote={})", current_version, remote_version);
        return;
    }

    log::info!(
        "[linux-updater] Update available: {} -> {}",
        current_version,
        remote_version
    );

    // Look up platform entry
    let key = platform_key();
    let platform = match latest.platforms.get(key) {
        Some(p) => p,
        None => {
            log::error!("[linux-updater] No platform entry for '{}' in latest.json", key);
            return;
        }
    };

    // Download to temp location
    let download_dir = std::env::temp_dir().join("kaitu-update");
    if let Err(e) = std::fs::create_dir_all(&download_dir) {
        log::error!("[linux-updater] Failed to create download dir: {}", e);
        return;
    }

    let filename = platform
        .url
        .rsplit('/')
        .next()
        .unwrap_or("update.tar.gz");
    let dest = download_dir.join(filename);
    let tmp_path = dest.with_file_name(format!(
        "{}.tmp",
        dest.file_name().unwrap().to_string_lossy()
    ));

    log::info!("[linux-updater] Downloading {} -> {}", platform.url, dest.display());

    match download_file(&platform.url, &tmp_path).await {
        Ok(()) => {}
        Err(e) => {
            log::error!("[linux-updater] Download failed: {}", e);
            let _ = std::fs::remove_file(&tmp_path);
            return;
        }
    }

    // Atomic rename
    if let Err(e) = std::fs::rename(&tmp_path, &dest) {
        log::error!("[linux-updater] Failed to rename tmp to final: {}", e);
        let _ = std::fs::remove_file(&tmp_path);
        return;
    }

    // Verify signature
    if let Err(e) = verify_signature(&dest, &platform.signature) {
        log::error!("[linux-updater] Signature verification failed: {}", e);
        let _ = std::fs::remove_file(&dest);
        return;
    }

    log::info!("[linux-updater] Signature verified OK");

    // Guard: discard stale download if channel changed during download
    let current_ch = channel::get_channel(app);
    if current_ch != ch {
        log::info!(
            "[linux-updater] Channel changed during download ({} -> {}), discarding",
            ch, current_ch
        );
        let _ = std::fs::remove_file(&dest);
        return;
    }

    // Handle channel switch auto-apply
    if CHANNEL_SWITCH_PENDING
        .compare_exchange(true, false, Ordering::SeqCst, Ordering::SeqCst)
        .is_ok()
    {
        log::info!(
            "[linux-updater] Channel switch complete ({}), auto-applying...",
            remote_version
        );
        match extract_via_pkexec(&dest) {
            Ok(()) => {
                log::info!("[linux-updater] Extraction successful, relaunching...");
                let _ = std::fs::remove_file(&dest);
                relaunch_app(app);
                return;
            }
            Err(e) => {
                log::error!("[linux-updater] Extraction failed: {}", e);
                let _ = std::fs::remove_file(&dest);
                INSTALL_FAILED.store(true, Ordering::SeqCst);
                return;
            }
        }
    }

    // Normal update: store info and notify frontend
    let info = UpdateInfo {
        current_version: current_version.clone(),
        new_version: remote_version.clone(),
        release_notes: latest.notes.clone(),
    };

    *UPDATE_INFO.lock().unwrap() = Some(info);
    UPDATE_READY.store(true, Ordering::SeqCst);

    let _ = app.emit(
        "update-ready",
        UpdateInfo {
            current_version,
            new_version: remote_version.clone(),
            release_notes: latest.notes,
        },
    );

    log::info!("[linux-updater] Frontend notified via update-ready event");
}

/// Manual update check -- returns status string for IPC.
pub async fn check_now(app: &AppHandle) -> Result<String, String> {
    // If update already ready, return status
    if crate::updater::is_update_ready() {
        if let Some(info) = UPDATE_INFO.lock().unwrap().as_ref() {
            return Ok(format!("Update {} already ready", info.new_version));
        }
    }

    let ch = channel::get_channel(app);
    let endpoints = channel::endpoints_for_channel(&ch)?;

    let current_version = app.package_info().version.to_string();

    let latest = fetch_latest_json(&endpoints)
        .await
        .ok_or("Failed to fetch latest.json from all endpoints")?;

    let remote_version = &latest.version;

    let has_update = match (
        Version::parse(&current_version),
        Version::parse(remote_version),
    ) {
        (Ok(current), Ok(remote)) => remote > current,
        _ => false,
    };

    if !has_update {
        log::info!("[linux-updater] No updates available");
        return Ok("Already on latest version".to_string());
    }

    log::info!(
        "[linux-updater] Update {} -> {}, downloading...",
        current_version,
        remote_version
    );

    let key = platform_key();
    let platform = latest
        .platforms
        .get(key)
        .ok_or_else(|| format!("No platform entry for '{}'", key))?;

    // Download
    let download_dir = std::env::temp_dir().join("kaitu-update");
    std::fs::create_dir_all(&download_dir)
        .map_err(|e| format!("Failed to create download dir: {}", e))?;

    let filename = platform.url.rsplit('/').next().unwrap_or("update.tar.gz");
    let dest = download_dir.join(filename);
    let tmp_path = dest.with_file_name(format!(
        "{}.tmp",
        dest.file_name().unwrap().to_string_lossy()
    ));

    download_file(&platform.url, &tmp_path)
        .await
        .map_err(|e| {
            let _ = std::fs::remove_file(&tmp_path);
            format!("Download failed: {}", e)
        })?;

    std::fs::rename(&tmp_path, &dest).map_err(|e| {
        let _ = std::fs::remove_file(&tmp_path);
        format!("Failed to rename: {}", e)
    })?;

    // Verify signature
    verify_signature(&dest, &platform.signature).map_err(|e| {
        let _ = std::fs::remove_file(&dest);
        format!("Signature verification failed: {}", e)
    })?;

    // Store info and notify
    let info = UpdateInfo {
        current_version: current_version.clone(),
        new_version: remote_version.clone(),
        release_notes: latest.notes.clone(),
    };
    *UPDATE_INFO.lock().unwrap() = Some(info);
    UPDATE_READY.store(true, Ordering::SeqCst);

    let _ = app.emit(
        "update-ready",
        UpdateInfo {
            current_version,
            new_version: remote_version.clone(),
            release_notes: latest.notes,
        },
    );

    Ok(format!("Update {} ready", remote_version))
}

/// Apply the downloaded update: extract via pkexec and relaunch.
/// Called from updater.rs apply_update_now on Linux.
pub fn apply_update(app: &AppHandle) {
    // Find the downloaded tar.gz
    let download_dir = std::env::temp_dir().join("kaitu-update");
    let tarball = match find_tarball(&download_dir) {
        Some(p) => p,
        None => {
            log::error!("[linux-updater] No tar.gz found in {}", download_dir.display());
            INSTALL_FAILED.store(true, Ordering::SeqCst);
            return;
        }
    };

    match extract_via_pkexec(&tarball) {
        Ok(()) => {
            log::info!("[linux-updater] Extraction successful, relaunching...");
            let _ = std::fs::remove_file(&tarball);
            UPDATE_READY.store(false, Ordering::SeqCst);
            relaunch_app(app);
        }
        Err(e) => {
            log::error!("[linux-updater] Extraction failed: {}", e);
            let _ = std::fs::remove_file(&tarball);
            INSTALL_FAILED.store(true, Ordering::SeqCst);
        }
    }
}

/// Download a file to the given path with streaming.
async fn download_file(url: &str, dest: &Path) -> Result<(), String> {
    use futures_util::StreamExt;

    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(300))
        .build()
        .map_err(|e| format!("Failed to create HTTP client: {}", e))?;

    let resp = client
        .get(url)
        .send()
        .await
        .map_err(|e| format!("HTTP request failed: {}", e))?;

    if !resp.status().is_success() {
        return Err(format!("HTTP {} for {}", resp.status(), url));
    }

    let total_size = resp.content_length();
    let mut stream = resp.bytes_stream();
    let mut file = std::fs::File::create(dest)
        .map_err(|e| format!("Failed to create file {}: {}", dest.display(), e))?;

    let mut downloaded: u64 = 0;
    let mut last_log_percent: u32 = 0;

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e| format!("Stream error: {}", e))?;
        std::io::Write::write_all(&mut file, &chunk)
            .map_err(|e| format!("Write error: {}", e))?;

        downloaded += chunk.len() as u64;
        if let Some(total) = total_size {
            let percent = (downloaded as f64 / total as f64 * 100.0) as u32;
            if percent >= last_log_percent + 10 {
                last_log_percent = percent;
                log::info!("[linux-updater] Download progress: {}%", percent);
            }
        }
    }

    log::info!(
        "[linux-updater] Downloaded {} bytes to {}",
        downloaded,
        dest.display()
    );
    Ok(())
}

/// Verify minisign signature of a file.
///
/// The signature in latest.json is base64-encoded minisign signature content.
/// We decode it, then parse as a minisign Signature.
fn verify_signature(file_path: &Path, sig_base64: &str) -> Result<(), String> {
    let pk = PublicKey::from_base64(MINISIGN_PUBLIC_KEY)
        .map_err(|e| format!("Failed to parse public key: {}", e))?;

    // Decode base64 to get minisign signature text
    let sig_bytes = base64::engine::general_purpose::STANDARD
        .decode(sig_base64)
        .map_err(|e| format!("Failed to decode signature base64: {}", e))?;

    let sig_text = String::from_utf8(sig_bytes)
        .map_err(|e| format!("Signature is not valid UTF-8: {}", e))?;

    let sig = Signature::decode(&sig_text)
        .map_err(|e| format!("Failed to parse minisign signature: {}", e))?;

    // Read the file
    let mut file = std::fs::File::open(file_path)
        .map_err(|e| format!("Failed to open file for verification: {}", e))?;
    let mut data = Vec::new();
    file.read_to_end(&mut data)
        .map_err(|e| format!("Failed to read file for verification: {}", e))?;

    pk.verify(&data, &sig, false)
        .map_err(|e| format!("Signature verification failed: {}", e))?;

    Ok(())
}

/// Extract tar.gz to /opt/kaitu/ via pkexec with two-phase staging.
///
/// 1. Extract to a temp directory (user-writable)
/// 2. Use pkexec to copy files to /opt/kaitu/
fn extract_via_pkexec(tarball: &Path) -> Result<(), String> {
    let staging_dir = std::env::temp_dir().join("kaitu-staging");

    // Clean up any leftover staging dir
    let _ = std::fs::remove_dir_all(&staging_dir);
    std::fs::create_dir_all(&staging_dir)
        .map_err(|e| format!("Failed to create staging dir: {}", e))?;

    // Extract tar.gz to staging dir
    let tar_gz = std::fs::File::open(tarball)
        .map_err(|e| format!("Failed to open tarball: {}", e))?;
    let gz = flate2::read::GzDecoder::new(tar_gz);
    let mut archive = tar::Archive::new(gz);
    archive
        .unpack(&staging_dir)
        .map_err(|e| format!("Failed to extract tar.gz: {}", e))?;

    log::info!("[linux-updater] Extracted to staging: {}", staging_dir.display());

    // Use pkexec to copy staging contents to /opt/kaitu/
    // First remove old installation, then copy new files
    let script = format!(
        "mkdir -p /opt/kaitu && cp -af '{}'/. /opt/kaitu/ && chmod +x /opt/kaitu/k2app /opt/kaitu/k2 && systemctl restart k2 2>/dev/null || true",
        staging_dir.display()
    );

    let output = std::process::Command::new("pkexec")
        .arg("sh")
        .arg("-c")
        .arg(&script)
        .output()
        .map_err(|e| format!("Failed to run pkexec: {}", e))?;

    // Clean up staging dir
    let _ = std::fs::remove_dir_all(&staging_dir);

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        return Err(format!(
            "pkexec extraction failed (exit {}): {}",
            output.status.code().unwrap_or(-1),
            stderr.trim()
        ));
    }

    log::info!("[linux-updater] Successfully installed to /opt/kaitu/");
    Ok(())
}

/// Find the most recent tar.gz file in the download directory.
fn find_tarball(dir: &Path) -> Option<PathBuf> {
    std::fs::read_dir(dir)
        .ok()?
        .filter_map(|entry| entry.ok())
        .map(|e| e.path())
        .filter(|p| {
            p.extension().map_or(false, |ext| ext == "gz")
                && p.to_string_lossy().contains(".tar.")
        })
        .max_by_key(|p| {
            std::fs::metadata(p)
                .and_then(|m| m.modified())
                .unwrap_or(std::time::SystemTime::UNIX_EPOCH)
        })
}

/// Relaunch the app via a helper process that polls for PID exit.
///
/// Spawns a background shell that waits for the current process to exit,
/// then launches /opt/kaitu/k2app via nohup.
fn relaunch_app(app: &AppHandle) {
    let pid = std::process::id();

    let script = format!(
        "i=0; while [ $i -lt 60 ]; do \
           if ! kill -0 {} 2>/dev/null; then \
             sleep 1; \
             nohup /opt/kaitu/k2app > /dev/null 2>&1 & \
             exit 0; \
           fi; \
           sleep 1; \
           i=$((i + 1)); \
         done",
        pid
    );

    log::info!(
        "[linux-updater] Spawning relaunch helper (pid={})",
        pid
    );

    match std::process::Command::new("sh")
        .arg("-c")
        .arg(&script)
        .spawn()
    {
        Ok(_) => {
            app.exit(0);
        }
        Err(e) => {
            log::error!("[linux-updater] Failed to spawn relaunch helper: {}", e);
            app.restart();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_platform_key_not_empty() {
        let key = platform_key();
        assert!(!key.is_empty());
        assert!(key.starts_with("linux-"));
    }

    #[test]
    fn test_latest_json_deserialization() {
        let json = r#"{
            "version": "0.4.0",
            "notes": "Bug fixes",
            "platforms": {
                "linux-x86_64": {
                    "url": "https://example.com/kaitu_0.4.0_amd64.tar.gz",
                    "signature": "dGVzdA=="
                }
            }
        }"#;

        let latest: LatestJson = serde_json::from_str(json).unwrap();
        assert_eq!(latest.version, "0.4.0");
        assert_eq!(latest.notes, Some("Bug fixes".to_string()));
        assert!(latest.platforms.contains_key("linux-x86_64"));

        let entry = &latest.platforms["linux-x86_64"];
        assert!(entry.url.contains("tar.gz"));
        assert_eq!(entry.signature, "dGVzdA==");
    }

    #[test]
    fn test_latest_json_missing_notes() {
        let json = r#"{
            "version": "0.4.0",
            "platforms": {
                "linux-x86_64": {
                    "url": "https://example.com/update.tar.gz",
                    "signature": "c2ln"
                }
            }
        }"#;

        let latest: LatestJson = serde_json::from_str(json).unwrap();
        assert_eq!(latest.version, "0.4.0");
        assert!(latest.notes.is_none());
    }

    #[test]
    fn test_find_tarball_empty_dir() {
        let dir = std::env::temp_dir().join("k2app-test-find-tarball-empty");
        let _ = std::fs::create_dir_all(&dir);
        assert!(find_tarball(&dir).is_none());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_find_tarball_with_file() {
        let dir = std::env::temp_dir().join("k2app-test-find-tarball");
        let _ = std::fs::create_dir_all(&dir);
        let tar_path = dir.join("Kaitu_0.4.0_amd64.tar.gz");
        std::fs::write(&tar_path, b"fake tarball").unwrap();

        let found = find_tarball(&dir);
        assert!(found.is_some());
        assert_eq!(found.unwrap().file_name().unwrap(), "Kaitu_0.4.0_amd64.tar.gz");

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_find_tarball_ignores_non_targz() {
        let dir = std::env::temp_dir().join("k2app-test-find-tarball-nontgz");
        let _ = std::fs::create_dir_all(&dir);
        std::fs::write(dir.join("readme.txt"), b"not a tarball").unwrap();
        std::fs::write(dir.join("update.zip"), b"not a tarball").unwrap();
        std::fs::write(dir.join("something.gz"), b"just gz, not tar.gz").unwrap();

        assert!(find_tarball(&dir).is_none());

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_tmp_path_for_tar_gz() {
        // Verify that .with_file_name() approach produces correct .tmp path
        let dest = PathBuf::from("/tmp/kaitu-update/Kaitu_0.4.0_amd64.tar.gz");
        let tmp_path = dest.with_file_name(format!(
            "{}.tmp",
            dest.file_name().unwrap().to_string_lossy()
        ));
        assert_eq!(
            tmp_path,
            PathBuf::from("/tmp/kaitu-update/Kaitu_0.4.0_amd64.tar.gz.tmp")
        );

        // Verify that .with_extension() would produce WRONG path
        let wrong = dest.with_extension("tar.gz.tmp");
        // .with_extension replaces only after last dot: .gz -> .tar.gz.tmp
        // This is NOT what we want
        assert_ne!(wrong, PathBuf::from("/tmp/kaitu-update/Kaitu_0.4.0_amd64.tar.gz.tmp"));
    }

    #[test]
    fn test_version_comparison() {
        let current = Version::parse("0.3.22").unwrap();
        let remote = Version::parse("0.4.0").unwrap();
        assert!(remote > current);

        let same = Version::parse("0.3.22").unwrap();
        assert!(!(same > current));
    }

    #[test]
    fn test_version_comparison_beta() {
        let current = Version::parse("0.4.0-beta.2").unwrap();
        let remote = Version::parse("0.4.0-beta.3").unwrap();
        assert!(remote > current);

        // Stable is greater than pre-release per semver
        let stable = Version::parse("0.4.0").unwrap();
        assert!(stable > current);
    }

    #[test]
    fn test_public_key_parse() {
        let pk = PublicKey::from_base64(MINISIGN_PUBLIC_KEY);
        assert!(pk.is_ok(), "Public key must parse successfully");
    }

    #[test]
    fn test_base64_decode_signature() {
        // A minisign signature is multi-line text, base64 encoded in latest.json
        let fake_sig_text = "untrusted comment: test\nRWSD3s7XX1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE";
        let encoded = base64::engine::general_purpose::STANDARD.encode(fake_sig_text);
        let decoded = base64::engine::general_purpose::STANDARD.decode(&encoded).unwrap();
        assert_eq!(decoded, fake_sig_text.as_bytes());
    }
}
