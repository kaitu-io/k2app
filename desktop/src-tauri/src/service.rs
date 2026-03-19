//! Service communication module for k2 daemon

use serde::{Deserialize, Serialize};
use sha2::{Sha256, Digest};
use std::process::Command;
use tauri::Manager;

/// Prevent visible console windows when spawning child processes from GUI app on Windows.
#[cfg(target_os = "windows")]
use std::os::windows::process::CommandExt;
#[cfg(target_os = "windows")]
const CREATE_NO_WINDOW: u32 = 0x08000000;

const DEFAULT_DAEMON_PORT: u16 = 1777;
const TIMEOUT_SECS_DEFAULT: u64 = 5;    // status, version, log-level
const TIMEOUT_SECS_LIFECYCLE: u64 = 30;  // up, down (engine start can take 8s+, stop 5s+)

fn service_base_url() -> String {
    let port = std::env::var("K2_DAEMON_PORT")
        .ok()
        .and_then(|v| v.parse::<u16>().ok())
        .unwrap_or(DEFAULT_DAEMON_PORT);
    format!("http://127.0.0.1:{}", port)
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ServiceResponse {
    pub code: i32,
    pub message: String,
    #[serde(default)]
    pub data: serde_json::Value,
}

#[derive(Debug, Serialize)]
struct CoreRequest {
    action: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
}

/// Call k2 helper API: POST /api/helper with JSON body
pub fn helper_action(action: &str, params: Option<serde_json::Value>) -> Result<ServiceResponse, String> {
    let url = format!("{}/api/helper", service_base_url());
    let body = CoreRequest {
        action: action.to_string(),
        params,
    };

    log::debug!("Calling helper action: {} (timeout={}s)", action, TIMEOUT_SECS_DEFAULT);

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(TIMEOUT_SECS_DEFAULT))
        .no_proxy()
        .build()
        .map_err(|e| format!("Failed to create HTTP client: {}", e))?;

    let response = client
        .post(&url)
        .json(&body)
        .send()
        .map_err(|e| format!("Failed to call helper action {}: {}", action, e))?;

    let status = response.status();
    if !status.is_success() {
        return Err(format!("Helper action {} failed with status: {}", action, status));
    }

    response
        .json::<ServiceResponse>()
        .map_err(|e| format!("Failed to parse response: {}", e))
}

/// Call k2 core API: POST /api/core with JSON body
pub fn core_action(action: &str, params: Option<serde_json::Value>) -> Result<ServiceResponse, String> {
    let url = format!("{}/api/core", service_base_url());
    let body = CoreRequest {
        action: action.to_string(),
        params,
    };

    let timeout_secs = match action {
        "up" | "down" => TIMEOUT_SECS_LIFECYCLE,
        _ => TIMEOUT_SECS_DEFAULT,
    };
    log::debug!("Calling core action: {} (timeout={}s)", action, timeout_secs);

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(timeout_secs))
        .no_proxy()
        .build()
        .map_err(|e| format!("Failed to create HTTP client: {}", e))?;

    let response = client
        .post(&url)
        .json(&body)
        .send()
        .map_err(|e| format!("Failed to call action {}: {}", action, e))?;

    let status = response.status();
    if !status.is_success() {
        return Err(format!("Action {} failed with status: {}", action, status));
    }

    response
        .json::<ServiceResponse>()
        .map_err(|e| format!("Failed to parse response: {}", e))
}

/// Send `down` action with a short timeout for app exit.
/// Uses 3s instead of the normal 30s lifecycle timeout to avoid
/// blocking exit when the daemon is already dead.
fn exit_down_action() -> Result<ServiceResponse, String> {
    let url = format!("{}/api/core", service_base_url());
    let body = CoreRequest {
        action: "down".to_string(),
        params: None,
    };
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(3))
        .no_proxy()
        .build()
        .map_err(|e| format!("Failed to create HTTP client: {}", e))?;
    client
        .post(&url)
        .json(&body)
        .send()
        .map_err(|e| format!("Failed to call action down: {}", e))?
        .json::<ServiceResponse>()
        .map_err(|e| format!("Failed to parse response: {}", e))
}

/// Stop VPN before app exit (best-effort, sync, short timeout).
/// Guarded by AtomicBool — only the first call executes. Subsequent calls
/// (e.g., from RunEvent::ExitRequested after tray quit) are skipped to
/// prevent sending duplicate HTTP `down` requests to the daemon.
pub fn stop_vpn() {
    use std::sync::atomic::{AtomicBool, Ordering};
    static VPN_STOPPING: AtomicBool = AtomicBool::new(false);

    if VPN_STOPPING.swap(true, Ordering::SeqCst) {
        log::debug!("stop_vpn: already stopping, skip duplicate call");
        return;
    }
    log::info!("Stopping VPN before exit...");
    // Use a short timeout for exit — don't block app exit for 30s if daemon is dead
    match exit_down_action() {
        Ok(resp) => {
            if resp.code == 0 {
                log::info!("VPN stopped successfully");
            } else {
                log::warn!("VPN stop returned code {}: {}", resp.code, resp.message);
            }
        }
        Err(e) => log::warn!("VPN stop failed (may not be running): {}", e),
    }
}

#[derive(Debug, Clone, PartialEq)]
pub enum VersionCheckResult {
    VersionMatch,
    VersionMismatch {
        service_version: String,
        app_version: String,
    },
    ServiceNotRunning,
}

/// Compare versions ignoring build metadata after '+'.
/// Dev builds (version == "dev") always match — never trigger reinstall.
pub fn versions_match(app_version: &str, service_version: &str) -> bool {
    if app_version == "dev" || service_version == "dev" {
        return true;
    }
    let app_base = app_version.split('+').next().unwrap_or(app_version);
    let svc_base = service_version.split('+').next().unwrap_or(service_version);
    app_base == svc_base
}

/// Check service version via action:version
pub fn check_service_version(app_version: &str) -> VersionCheckResult {
    match core_action("version", None) {
        Ok(response) => {
            if let Some(version) = response.data.get("version").and_then(|v| v.as_str()) {
                if versions_match(app_version, version) {
                    VersionCheckResult::VersionMatch
                } else {
                    VersionCheckResult::VersionMismatch {
                        service_version: version.to_string(),
                        app_version: app_version.to_string(),
                    }
                }
            } else {
                VersionCheckResult::ServiceNotRunning
            }
        }
        Err(_) => VersionCheckResult::ServiceNotRunning,
    }
}

/// IPC command: proxy VPN action to k2 daemon or NE bridge (macOS + ne-mode).
///
/// In NE mode (macOS + `ne-mode` feature) the call is routed through the Swift NE helper.
/// Otherwise (default, all platforms) the call goes to the k2 daemon HTTP API at :1777.
/// Called from webapp as window.__TAURI__.core.invoke('daemon_exec', {action, params})
#[tauri::command]
pub async fn daemon_exec(
    action: String,
    params: Option<serde_json::Value>,
) -> Result<ServiceResponse, String> {
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        tokio::task::spawn_blocking(move || crate::ne::ne_action(&action, params))
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
    #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
    {
        tokio::task::spawn_blocking(move || core_action(&action, params))
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
}

/// IPC command: proxy helper action to k2 daemon POST /api/helper.
/// Called from webapp as window.__TAURI__.core.invoke('daemon_helper_exec', {action, params})
#[tauri::command]
pub async fn daemon_helper_exec(
    action: String,
    params: Option<serde_json::Value>,
) -> Result<ServiceResponse, String> {
    tokio::task::spawn_blocking(move || helper_action(&action, params))
        .await
        .map_err(|e| format!("Task join error: {}", e))?
}

/// Internal: set daemon log level via HTTP (no IPC, no channel check).
/// Blocking — call from sync context or wrap in `spawn_blocking`.
#[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
pub fn set_log_level_internal(level: &str) -> Result<ServiceResponse, String> {
    let url = format!("{}/api/log-level", service_base_url());
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(TIMEOUT_SECS_DEFAULT))
        .no_proxy()
        .build()
        .map_err(|e| format!("Failed to create HTTP client: {}", e))?;
    client
        .post(&url)
        .json(&serde_json::json!({"level": level}))
        .send()
        .map_err(|e| format!("Failed to set log level: {}", e))?
        .json::<ServiceResponse>()
        .map_err(|e| format!("Failed to parse response: {}", e))
}

/// NE mode stub: no daemon to hot-switch.
#[cfg(all(target_os = "macos", feature = "ne-mode"))]
pub fn set_log_level_internal(_level: &str) -> Result<ServiceResponse, String> {
    Ok(ServiceResponse {
        code: 0,
        message: "ok".to_string(),
        data: serde_json::Value::Null,
    })
}

/// IPC command: hot-switch daemon log level.
///
/// Beta channel forces debug level — any other level is rejected.
/// Daemon mode: POST /api/log-level to k2 daemon.
/// NE mode: no-op (level applies on next connect via config).
#[tauri::command]
pub async fn set_log_level(app: tauri::AppHandle, level: String) -> Result<ServiceResponse, String> {
    let effective_level = if crate::channel::get_channel(&app) == "beta" {
        log::info!("[service] Beta channel: forcing debug log level (requested: {})", level);
        "debug".to_string()
    } else {
        level
    };

    tokio::task::spawn_blocking(move || set_log_level_internal(&effective_level))
        .await
        .map_err(|e| format!("Task join error: {}", e))?
}

/// SHA-256 hash a raw platform ID to 32 lowercase hex chars (128 bit).
/// All platforms use this to produce a uniform UDID format.
fn hash_to_udid(raw: &str) -> String {
    let hash = Sha256::digest(raw.as_bytes());
    // Take first 16 bytes (128 bit) → 32 hex chars
    hash.iter().take(16).map(|b| format!("{:02x}", b)).collect()
}

/// IPC command: get device UDID.
///
/// Each platform reads a single stable OS/firmware-level ID, then SHA-256 hashes
/// it to a uniform 32 hex char string. No hardware fingerprint composition.
///
/// macOS: `sysctl -n kern.uuid` (IOPlatformUUID, firmware-level).
/// Windows: WMI `Win32_ComputerSystemProduct.UUID` (SMBIOS firmware, via COM API).
/// Linux: `/etc/machine-id` (generated at OS install).
#[tauri::command]
pub async fn get_udid() -> Result<ServiceResponse, String> {
    tokio::task::spawn_blocking(|| get_udid_native())
        .await
        .map_err(|e| format!("Task join error: {}", e))?
}

fn get_udid_native() -> Result<ServiceResponse, String> {
    let udid = get_hardware_uuid()?;
    Ok(ServiceResponse {
        code: 0,
        message: "ok".to_string(),
        data: serde_json::json!({ "udid": udid }),
    })
}

pub(crate) fn get_hardware_uuid() -> Result<String, String> {
    let raw = get_raw_hardware_id()?;
    Ok(hash_to_udid(&raw))
}

/// Read the raw platform-specific hardware/machine ID (before hashing).
fn get_raw_hardware_id() -> Result<String, String> {
    #[cfg(target_os = "macos")]
    {
        let output = Command::new("sysctl")
            .args(["-n", "kern.uuid"])
            .output()
            .map_err(|e| format!("sysctl failed: {}", e))?;
        let uuid = String::from_utf8_lossy(&output.stdout).trim().to_string();
        if uuid.is_empty() {
            return Err("Empty UUID from sysctl".to_string());
        }
        Ok(uuid)
    }
    #[cfg(target_os = "windows")]
    {
        use wmi::{COMLibrary, WMIConnection};

        #[derive(serde::Deserialize)]
        #[serde(rename = "Win32_ComputerSystemProduct")]
        struct CsProduct {
            #[serde(rename = "UUID")]
            uuid: String,
        }

        let com = COMLibrary::new()
            .map_err(|e| format!("COM init failed: {}", e))?;
        let wmi_conn = WMIConnection::new(com)
            .map_err(|e| format!("WMI connection failed: {}", e))?;
        let results: Vec<CsProduct> = wmi_conn
            .query()
            .map_err(|e| format!("WMI query failed: {}", e))?;
        let uuid = results
            .first()
            .map(|r| r.uuid.clone())
            .unwrap_or_default();
        if uuid.is_empty() || uuid == "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF" {
            return Err("No valid SMBIOS UUID available".to_string());
        }
        Ok(uuid)
    }
    #[cfg(target_os = "linux")]
    {
        std::fs::read_to_string("/etc/machine-id")
            .map(|s| s.trim().to_string())
            .map_err(|e| format!("Failed to read machine-id: {}", e))
    }
}

/// IPC command: toggle WebView devtools (debug builds only).
#[tauri::command]
pub async fn set_dev_enabled(enabled: bool, app: tauri::AppHandle) -> Result<(), String> {
    #[cfg(debug_assertions)]
    {
        if enabled {
            if let Some(window) = app.get_webview_window("main") {
                window.open_devtools();
            }
        } else {
            if let Some(window) = app.get_webview_window("main") {
                window.close_devtools();
            }
        }
    }
    #[cfg(not(debug_assertions))]
    {
        let _ = (enabled, &app);
    }
    Ok(())
}

/// IPC command: get current process PID
#[tauri::command]
pub fn get_pid() -> u32 {
    std::process::id()
}

/// IPC command: get platform info (no HTTP, pure local)
#[tauri::command]
pub fn get_platform_info() -> serde_json::Value {
    let arch = match std::env::consts::ARCH {
        "aarch64" => "arm64",
        "x86_64" => "amd64",
        other => other,
    };
    serde_json::json!({
        "os": std::env::consts::OS,
        "version": env!("CARGO_PKG_VERSION"),
        "arch": arch,
        "commit": option_env!("K2_COMMIT").unwrap_or(""),
    })
}

#[tauri::command]
pub async fn admin_reinstall_service() -> Result<String, String> {
    log::info!("[service] Admin service install requested");

    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        // On macOS NE mode: delegate to NE helper (Swift static library)
        tokio::task::spawn_blocking(|| crate::ne::admin_reinstall_ne())
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }

    #[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
    {
        tokio::task::spawn_blocking(admin_reinstall_service_macos)
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }

    #[cfg(target_os = "windows")]
    {
        admin_reinstall_service_windows().await
    }

    #[cfg(target_os = "linux")]
    {
        tokio::task::spawn_blocking(admin_reinstall_service_linux)
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }
}

#[cfg(target_os = "windows")]
async fn admin_reinstall_service_windows() -> Result<String, String> {
    let exe_path = std::env::current_exe()
        .map_err(|e| format!("Failed to get exe path: {}", e))?;
    let app_dir = exe_path.parent().ok_or("Failed to get app directory")?;
    let service_path = app_dir.join("k2.exe");

    if !service_path.exists() {
        return Err(format!("Service not found: {:?}", service_path));
    }

    let ps_script = format!(
        r#"Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden"#,
        service_path.display()
    );

    let output = Command::new("powershell")
        .args(["-NoProfile", "-WindowStyle", "Hidden", "-Command", &ps_script])
        .creation_flags(CREATE_NO_WINDOW)
        .output()
        .map_err(|e| format!("PowerShell failed: {}", e))?;

    if output.status.success() {
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        Err(format!("Failed: {}", stderr))
    }
}

/// macOS daemon mode: install k2 service with admin privileges via osascript.
/// Uses the k2 sidecar binary at Contents/MacOS/k2 relative to the running app.
/// The osascript dialog prompts the user for their admin password.
#[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
fn admin_reinstall_service_macos() -> Result<String, String> {
    let exe_path = std::env::current_exe()
        .map_err(|e| format!("Failed to get exe path: {}", e))?;
    let app_dir = exe_path.parent().ok_or("Failed to get app directory")?;

    // Try plain `k2` first (production build), then look for `k2-*` (target-triple variant)
    let k2_path = if app_dir.join("k2").exists() {
        app_dir.join("k2")
    } else {
        // Find k2-{target-triple} in the same directory
        let found = std::fs::read_dir(app_dir)
            .map_err(|e| format!("Failed to read app dir: {}", e))?
            .filter_map(|e| e.ok())
            .find(|e| {
                let name = e.file_name();
                let name_str = name.to_string_lossy();
                name_str.starts_with("k2-") && !name_str.contains('.')
            })
            .map(|e| e.path());
        match found {
            Some(path) => path,
            None => return Err(format!("k2 binary not found in {:?}", app_dir)),
        }
    };

    let k2_str = k2_path.to_string_lossy();
    log::info!("[service] Installing via osascript: {}", k2_str);

    // Use osascript to run `k2 service install` with admin privileges.
    // This shows the macOS admin password dialog.
    let script = format!(
        r#"do shell script "{} service install" with administrator privileges"#,
        k2_str.replace('\\', "\\\\").replace('"', "\\\"")
    );

    let output = Command::new("osascript")
        .args(["-e", &script])
        .output()
        .map_err(|e| format!("osascript failed: {}", e))?;

    if output.status.success() {
        log::info!("[service] Service installed successfully via osascript");
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        let stderr_str = stderr.trim();
        // osascript returns -128 when user clicks Cancel
        if stderr_str.contains("User canceled") || stderr_str.contains("-128") {
            log::info!("[service] User cancelled admin prompt");
            Err("User cancelled".to_string())
        } else {
            log::error!("[service] osascript failed: {}", stderr_str);
            Err(format!("Failed to install service: {}", stderr_str))
        }
    }
}

/// Find k2 binary from Tauri sidecar (relative to current exe).
/// On Linux AppImage, this is inside the FUSE mount directory.
#[cfg(target_os = "linux")]
fn find_k2_from_sidecar() -> Result<std::path::PathBuf, String> {
    let exe_path = std::env::current_exe()
        .map_err(|e| format!("Failed to get exe path: {}", e))?;
    let app_dir = exe_path.parent().ok_or("Failed to get app directory")?;

    // Try plain `k2` first, then `k2-*` (target-triple variant)
    let k2_path = app_dir.join("k2");
    if k2_path.exists() {
        return Ok(k2_path);
    }

    // Find k2-{target-triple} in the same directory
    if let Ok(entries) = std::fs::read_dir(app_dir) {
        for entry in entries.flatten() {
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            if name_str.starts_with("k2-") && !name_str.contains('.') {
                return Ok(entry.path());
            }
        }
    }

    Err(format!("k2 sidecar not found in {:?}", app_dir))
}

/// Linux daemon mode: copy sidecar k2 to /opt/kaitu/k2 (persistent path),
/// then install systemd service via pkexec.
///
/// This two-step approach is necessary because AppImage contents are only
/// accessible via a transient FUSE mount (/tmp/.mount_XxxXXX/). The systemd
/// unit's ExecStart must reference a persistent path that survives reboots.
#[cfg(target_os = "linux")]
fn admin_reinstall_service_linux() -> Result<String, String> {
    let source_k2 = find_k2_from_sidecar()?;
    let source_str = source_k2.to_string_lossy();
    log::info!("[service] Linux: source k2 binary: {}", source_str);

    // Check if pkexec is available
    let pkexec_available = Command::new("which")
        .arg("pkexec")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false);

    if !pkexec_available {
        log::warn!("[service] pkexec not available");
        return Err("pkexec_unavailable: run 'sudo k2 service install' manually".to_string());
    }

    // AppImage FUSE mount is user-private (allow_other=false by default).
    // pkexec runs as root and cannot access /tmp/.mount_Kaitu_xxx/.
    // Fix: copy k2 binary to /tmp as current user first, then pkexec from there.
    let tmp_k2 = "/tmp/kaitu-k2-install";
    std::fs::copy(&source_k2, tmp_k2).map_err(|e| {
        format!("Failed to stage k2 binary to {}: {}", tmp_k2, e)
    })?;
    log::info!("[service] Linux: staged k2 binary to {}", tmp_k2);

    let script = format!(
        "mkdir -p /opt/kaitu && cp '{}' /opt/kaitu/k2 && chmod +x /opt/kaitu/k2 && \
         ln -sf /opt/kaitu/k2 /usr/local/bin/k2 && /opt/kaitu/k2 service install && rm -f '{}'",
        tmp_k2, tmp_k2
    );

    let output = Command::new("pkexec")
        .args(["bash", "-c", &script])
        .output()
        .map_err(|e| format!("pkexec failed: {}", e))?;

    if output.status.success() {
        log::info!("[service] Service installed successfully via pkexec");
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        let stderr_str = stderr.trim();
        // pkexec returns 126 when user dismisses the dialog
        if output.status.code() == Some(126) {
            log::info!("[service] User cancelled pkexec prompt");
            Err("User cancelled".to_string())
        } else {
            log::error!("[service] pkexec failed: {}", stderr_str);
            Err(format!("Failed to install service: {}", stderr_str))
        }
    }
}

/// Detect old kaitu-service
pub fn detect_old_kaitu_service() -> bool {
    #[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
    {
        std::path::Path::new("/Library/LaunchDaemons/io.kaitu.service.plist").exists()
            || std::path::Path::new("/Library/LaunchDaemons/com.kaitu.service.plist").exists()
    }
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        false
    }
    #[cfg(target_os = "windows")]
    {
        Command::new("sc")
            .args(["query", "kaitu-service"])
            .creation_flags(CREATE_NO_WINDOW)
            .output()
            .map(|o| o.status.success())
            .unwrap_or(false)
    }
    #[cfg(target_os = "linux")]
    {
        std::path::Path::new("/etc/systemd/system/kaitu-service.service").exists()
    }
}

/// Cleanup old kaitu-service
pub fn cleanup_old_kaitu_service() {
    if !detect_old_kaitu_service() {
        return;
    }
    log::info!("[service] Cleaning up old kaitu-service");

    #[cfg(all(target_os = "macos", not(feature = "ne-mode")))]
    {
        for plist in &[
            "/Library/LaunchDaemons/io.kaitu.service.plist",
            "/Library/LaunchDaemons/com.kaitu.service.plist",
        ] {
            if std::path::Path::new(plist).exists() {
                let _ = Command::new("launchctl").args(["unload", plist]).output();
                let _ = std::fs::remove_file(plist);
            }
        }
    }
    #[cfg(target_os = "windows")]
    {
        let _ = Command::new("sc")
            .args(["stop", "kaitu-service"])
            .creation_flags(CREATE_NO_WINDOW)
            .output();
        let _ = Command::new("sc")
            .args(["delete", "kaitu-service"])
            .creation_flags(CREATE_NO_WINDOW)
            .output();
    }
    #[cfg(target_os = "linux")]
    {
        let _ = Command::new("systemctl").args(["stop", "kaitu-service"]).output();
        let _ = Command::new("systemctl").args(["disable", "kaitu-service"]).output();
        let _ = std::fs::remove_file("/etc/systemd/system/kaitu-service.service");
    }
}

/// Poll `check_service_version` until VersionMatch or timeout.
/// Returns the last check result so the caller knows WHY it failed.
fn wait_for_version_match(app_version: &str, timeout_ms: u64, poll_ms: u64) -> VersionCheckResult {
    let start = std::time::Instant::now();
    let timeout = std::time::Duration::from_millis(timeout_ms);
    let interval = std::time::Duration::from_millis(poll_ms);

    let mut last = check_service_version(app_version);
    if matches!(last, VersionCheckResult::VersionMatch) {
        return last;
    }

    log::info!("[service] Waiting for version match (current: {:?})", last);
    let mut poll_count = 0u32;
    while start.elapsed() < timeout {
        std::thread::sleep(interval);
        last = check_service_version(app_version);
        poll_count += 1;
        log::debug!("[service] Poll #{}: {:?} (elapsed: {:?})", poll_count, last, start.elapsed());
        if matches!(last, VersionCheckResult::VersionMatch) {
            log::info!("[service] Version matched after {:?} ({} polls)", start.elapsed(), poll_count);
            return last;
        }
    }
    log::warn!("[service] No match within {:?} after {} polls (last: {:?})", timeout, poll_count, last);
    last
}

/// Ensure service running with correct version.
///
/// NE mode (macOS + `ne-mode`): installs NE VPN profile via Swift helper.
/// Daemon mode (default): version check → wait → osascript install if needed.
#[tauri::command]
pub async fn ensure_service_running(app_version: String) -> Result<(), String> {
    #[cfg(all(target_os = "macos", feature = "ne-mode"))]
    {
        log::info!(
            "[service] macOS: ensuring NE installed (v{})",
            app_version
        );
        return tokio::task::spawn_blocking(|| crate::ne::ensure_ne_installed())
            .await
            .map_err(|e| format!("spawn_blocking failed: {}", e))?;
    }

    #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
    ensure_service_running_daemon(app_version).await
}

/// Daemon-based service lifecycle: version check as single source of truth.
///
/// Phase 1: cleanup legacy services + wait for version match (8s).
///          Covers: already running, cold start via launchd, dev bypass.
/// Phase 2: osascript admin install (only if phase 1 fails).
/// Phase 3: post-install verification — wait for version match (5s).
#[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
async fn ensure_service_running_daemon(app_version: String) -> Result<(), String> {
    const STARTUP_WAIT_MS: u64 = 15000;
    const POST_INSTALL_WAIT_MS: u64 = 5000;
    const POLL_MS: u64 = 500;

    log::info!("[service] Ensuring service running (v{})", app_version);

    // Phase 1: cleanup + wait for version match
    let ver = app_version.clone();
    let check = tokio::task::spawn_blocking(move || {
        cleanup_old_kaitu_service();
        wait_for_version_match(&ver, STARTUP_WAIT_MS, POLL_MS)
    })
    .await
    .map_err(|e| format!("spawn_blocking failed: {}", e))?;

    if matches!(check, VersionCheckResult::VersionMatch) {
        log::info!("[service] Service ready");
        return Ok(());
    }

    // Phase 2: install via admin elevation
    log::info!("[service] Install needed: {:?}", check);

    // Diagnostic: check if k2 binary exists
    let exe_path = std::env::current_exe().ok();
    if let Some(ref exe) = exe_path {
        let k2_name = if cfg!(windows) { "k2.exe" } else { "k2" };
        let k2_path = exe.parent().map(|d| d.join(k2_name));
        log::info!("[service] k2 binary exists: {:?} -> {}", k2_path, k2_path.as_ref().map_or(false, |p| p.exists()));
    }

    match admin_reinstall_service().await {
        Ok(msg) => log::info!("[service] Admin install succeeded: {}", msg),
        Err(ref e) => {
            log::error!("[service] Admin install failed: {}", e);
            return Err(e.clone());
        }
    }

    // Phase 3: verify post-install
    let ver = app_version.clone();
    let ok = tokio::task::spawn_blocking(move || {
        matches!(
            wait_for_version_match(&ver, POST_INSTALL_WAIT_MS, POLL_MS),
            VersionCheckResult::VersionMatch
        )
    })
    .await
    .map_err(|e| format!("spawn_blocking failed: {}", e))?;

    if ok {
        log::info!("[service] Service ready after install");
        Ok(())
    } else {
        Err("Service did not start with correct version after install".to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Test: daemon_exec routes correctly and does not panic.
    // Result depends on whether a local daemon is running — both Ok and Err are valid.
    #[tokio::test]
    async fn test_daemon_exec_no_panic() {
        let result = daemon_exec("status".to_string(), None).await;
        match &result {
            Ok(_resp) => {} // Daemon running — valid
            Err(e) => assert!(
                e.contains("Failed to call action")
                    || e.contains("connection refused")
                    || e.contains("os error"),
                "Err should indicate connection failure: {}",
                e
            ),
        }
    }

    #[test]
    fn test_versions_match_identical() {
        assert!(versions_match("0.4.0", "0.4.0"));
    }

    #[test]
    fn test_versions_match_with_build_metadata() {
        assert!(versions_match("0.4.0", "0.4.0+abc123"));
        assert!(versions_match("0.4.0+x", "0.4.0+y"));
    }

    #[test]
    fn test_versions_mismatch() {
        assert!(!versions_match("0.4.0", "0.3.22"));
        assert!(!versions_match("0.4.0", "0.4.1"));
    }

    #[test]
    fn test_detect_old_kaitu_service_no_crash() {
        // Should not crash even if service doesn't exist
        let _ = detect_old_kaitu_service();
    }

    #[test]
    fn test_versions_match_dev_bypass() {
        assert!(versions_match("0.4.0", "dev"));
        assert!(versions_match("dev", "0.4.0"));
        assert!(versions_match("dev", "dev"));
    }

    #[test]
    fn test_versions_match_dev_not_substring() {
        assert!(!versions_match("0.4.0", "dev-build"));
        assert!(!versions_match("0.4.0", "develop"));
        assert!(!versions_match("developer", "0.4.0"));
    }

    // -----------------------------------------------------------------------
    // REFACTOR [MUST] 1: ServiceResponse format is identical across NE and daemon paths
    // -----------------------------------------------------------------------
    // Both paths return the same ServiceResponse struct {code: i32, message: String, data: Value}.
    // The shared struct definition enforces this at compile time. This test verifies
    // that a ServiceResponse can be deserialized from both styles of JSON payload:
    // - With data field (NE path, daemon v2+)
    // - Without data field (old daemon — serde(default) = null)
    #[test]
    fn test_refactor_service_response_format_identical_across_paths() {
        // NE path: always includes data field
        let with_data = r#"{"code":0,"message":"ok","data":{"state":"disconnected"}}"#;
        let resp_with: ServiceResponse = serde_json::from_str(with_data).unwrap();
        assert_eq!(resp_with.code, 0);
        assert_eq!(resp_with.message, "ok");
        assert!(resp_with.data.get("state").is_some());

        // Daemon path: may omit data field → defaults to null
        let without_data = r#"{"code":0,"message":"ok"}"#;
        let resp_without: ServiceResponse = serde_json::from_str(without_data).unwrap();
        assert_eq!(resp_without.code, 0);
        assert_eq!(resp_without.message, "ok");
        assert!(resp_without.data.is_null(), "missing data should default to null");

        // Error response: code != 0
        let error_resp = r#"{"code":503,"message":"server unreachable","data":null}"#;
        let resp_error: ServiceResponse = serde_json::from_str(error_resp).unwrap();
        assert_eq!(resp_error.code, 503);
        assert!(!resp_error.message.is_empty());
    }

    // -----------------------------------------------------------------------
    // REFACTOR [MUST] 2: get_pid returns Tauri app's own PID (not daemon PID)
    // -----------------------------------------------------------------------
    #[test]
    fn test_refactor_get_pid_returns_own_process_id() {
        let pid = get_pid();
        // get_pid() must return std::process::id() — the current process PID
        assert_eq!(pid, std::process::id(), "get_pid() should return own process PID");
        // PID must be non-zero (the kernel never assigns PID 0 to user processes)
        assert!(pid > 0, "PID should be positive");
    }

    // -----------------------------------------------------------------------
    // REFACTOR [MUST] 3: ServiceResponse is shared — ne.rs imports from service
    // -----------------------------------------------------------------------
    // This is verified at compile time: ne.rs uses `crate::service::ServiceResponse`.
    // The test below confirms the struct is publicly accessible and the fields align.
    #[test]
    fn test_refactor_service_response_shared_type() {
        // Construct a ServiceResponse as ne.rs would (same type, same fields)
        let resp = ServiceResponse {
            code: 0,
            message: "ok".into(),
            data: serde_json::json!({ "version": "0.4.0", "os": "macos" }),
        };
        // Serialize to JSON and verify the canonical {code, message, data} shape
        let json = serde_json::to_string(&resp).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert!(parsed.get("code").is_some(), "must have 'code' field");
        assert!(parsed.get("message").is_some(), "must have 'message' field");
        assert!(parsed.get("data").is_some(), "must have 'data' field");
        // Verify round-trip
        let round_tripped: ServiceResponse = serde_json::from_str(&json).unwrap();
        assert_eq!(round_tripped.code, resp.code);
        assert_eq!(round_tripped.message, resp.message);
    }

    // -----------------------------------------------------------------------
    // hash_to_udid: uniform 32 hex char output from any raw platform ID
    // -----------------------------------------------------------------------
    #[test]
    fn test_get_platform_info_fields() {
        let info = get_platform_info();
        assert!(info.get("os").is_some(), "must have 'os' field");
        assert!(info.get("version").is_some(), "must have 'version' field");
        assert!(info.get("arch").is_some(), "must have 'arch' field");

        let os = info["os"].as_str().unwrap();
        assert!(
            ["macos", "windows", "linux"].contains(&os),
            "os must be macos/windows/linux, got: {}",
            os
        );

        let arch = info["arch"].as_str().unwrap();
        assert!(
            ["arm64", "amd64"].contains(&arch),
            "arch must be arm64/amd64, got: {}",
            arch
        );
    }

    #[test]
    fn test_service_base_url_default() {
        // Without K2_DAEMON_PORT env, should use default 1777
        std::env::remove_var("K2_DAEMON_PORT");
        let url = service_base_url();
        assert_eq!(url, "http://127.0.0.1:1777");
    }

    #[test]
    fn test_hash_to_udid_format() {
        let udid = hash_to_udid("test-input");
        assert_eq!(udid.len(), 32, "UDID must be 32 chars");
        assert!(udid.chars().all(|c| c.is_ascii_hexdigit() && !c.is_ascii_uppercase()),
            "UDID must be lowercase hex: {}", udid);
    }

    #[test]
    fn test_hash_to_udid_deterministic() {
        let a = hash_to_udid("same-input");
        let b = hash_to_udid("same-input");
        assert_eq!(a, b, "Same input must produce same UDID");
    }

    #[test]
    fn test_hash_to_udid_different_inputs() {
        let a = hash_to_udid("input-a");
        let b = hash_to_udid("input-b");
        assert_ne!(a, b, "Different inputs must produce different UDIDs");
    }

    #[test]
    fn test_hash_to_udid_uuid_input() {
        // Simulate macOS/Windows UUID format
        let udid = hash_to_udid("550E8400-E29B-41D4-A716-446655440000");
        assert_eq!(udid.len(), 32);
    }

    #[test]
    fn test_hash_to_udid_short_input() {
        // Simulate Android ANDROID_ID (16 hex chars)
        let udid = hash_to_udid("a1b2c3d4e5f6a7b8");
        assert_eq!(udid.len(), 32);
    }

    // -------------------------------------------------------------------
    // Windows-specific tests (only compiled + run on Windows)
    // -------------------------------------------------------------------
    #[cfg(target_os = "windows")]
    mod windows_tests {
        use super::super::*;

        /// WMI UDID: call get_hardware_uuid(), assert Ok, non-empty,
        /// and not the all-F sentinel that indicates missing SMBIOS data.
        #[test]
        fn test_windows_udid_wmi_available() {
            let result = get_hardware_uuid();
            assert!(result.is_ok(), "get_hardware_uuid() should succeed on Windows: {:?}", result);
            let udid = result.unwrap();
            assert!(!udid.is_empty(), "UDID must not be empty");
            // The raw UUID "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF" is rejected
            // inside get_raw_hardware_id, so we should never see its hash here.
            // Also verify the hashed output is 32 lowercase hex chars.
            assert_eq!(udid.len(), 32, "UDID must be 32 hex chars, got: {}", udid);
            assert!(
                udid.chars().all(|c| c.is_ascii_hexdigit() && !c.is_ascii_uppercase()),
                "UDID must be lowercase hex: {}",
                udid
            );
        }

        /// Verify that current_exe().parent().join("k2.exe") produces a
        /// Windows-style path with backslashes ending in "k2.exe".
        #[test]
        fn test_windows_service_path_has_backslashes() {
            let exe_path = std::env::current_exe().expect("current_exe() should succeed");
            let app_dir = exe_path.parent().expect("exe should have a parent dir");
            let service_path = app_dir.join("k2.exe");
            let path_str = service_path.to_string_lossy().to_string();

            assert!(
                path_str.contains('\\'),
                "Windows path should contain backslashes: {}",
                path_str
            );
            assert!(
                path_str.ends_with("k2.exe"),
                "Path should end with k2.exe: {}",
                path_str
            );
        }

        /// Verify the PowerShell command uses single quotes around the path
        /// (preventing injection). A path with spaces must not break the command.
        #[test]
        fn test_powershell_command_no_injection() {
            // Simulate a path with spaces — common on Windows (e.g. "Program Files")
            let fake_path = std::path::PathBuf::from(
                r"C:\Program Files\Kaitu VPN\k2.exe"
            );

            // This is the same format string used in admin_reinstall_service_windows()
            let ps_script = format!(
                r#"Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden"#,
                fake_path.display()
            );

            // Single quotes around the path prevent PowerShell injection
            assert!(
                ps_script.contains(&format!("'{}'", fake_path.display())),
                "Path must be wrapped in single quotes: {}",
                ps_script
            );

            // Must not contain double quotes around the path (vulnerable to injection)
            let path_str = fake_path.display().to_string();
            assert!(
                !ps_script.contains(&format!("\"{}\"", path_str)),
                "Path must NOT use double quotes: {}",
                ps_script
            );

            // Verify the full command looks correct
            assert!(ps_script.starts_with("Start-Process"));
            assert!(ps_script.contains("-Verb RunAs"));
            assert!(ps_script.contains("-WindowStyle Hidden"));
        }
    }

    #[cfg(target_os = "linux")]
    mod linux_tests {
        use super::super::*;

        /// Verify find_k2_from_sidecar() looks for k2 relative to current exe.
        #[test]
        fn test_linux_k2_sidecar_lookup() {
            let result = find_k2_from_sidecar();
            match result {
                Ok(path) => {
                    let path_str = path.to_string_lossy();
                    assert!(
                        path_str.contains("k2"),
                        "Path should contain 'k2': {}",
                        path_str
                    );
                }
                Err(e) => {
                    assert!(
                        e.contains("not found"),
                        "Error should indicate k2 not found: {}",
                        e
                    );
                }
            }
        }
    }

    #[test]
    fn test_stop_vpn_atomic_guard() {
        // The VPN_STOPPING AtomicBool is function-local static, so we test
        // the guard logic directly: swap(true) returns false on first call
        // (was false → proceed), true on subsequent calls (was true → skip).
        use std::sync::atomic::{AtomicBool, Ordering};
        let guard = AtomicBool::new(false);
        // First call: was false → should proceed
        assert!(!guard.swap(true, Ordering::SeqCst));
        // Second call: was true → should skip
        assert!(guard.swap(true, Ordering::SeqCst));
        // Third call: still true → still skip
        assert!(guard.swap(true, Ordering::SeqCst));
    }
}
