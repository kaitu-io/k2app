//! Service communication module for k2 daemon

use serde::{Deserialize, Serialize};
use std::process::Command;

const DEFAULT_DAEMON_PORT: u16 = 1777;
const REQUEST_TIMEOUT_SECS: u64 = 5;

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

/// Call k2 core API: POST /api/core with JSON body
pub fn core_action(action: &str, params: Option<serde_json::Value>) -> Result<ServiceResponse, String> {
    let url = format!("{}/api/core", service_base_url());
    let body = CoreRequest {
        action: action.to_string(),
        params,
    };

    log::debug!("Calling core action: {}", action);

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
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

/// Ping the k2 service
pub fn ping_service() -> bool {
    let url = format!("{}/ping", service_base_url());
    let client = match reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(2))
        .no_proxy()
        .build()
    {
        Ok(c) => c,
        Err(_) => return false,
    };
    match client.get(&url).send() {
        Ok(resp) => {
            if let Ok(body) = resp.json::<ServiceResponse>() {
                body.code == 0
            } else {
                false
            }
        }
        Err(_) => false,
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

/// Compare versions ignoring build metadata after '+'
pub fn versions_match(app_version: &str, service_version: &str) -> bool {
    let app_base = app_version.split('+').next().unwrap_or(app_version);
    let service_base = service_version.split('+').next().unwrap_or(service_version);
    app_base == service_base
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

/// IPC command: proxy VPN action to k2 daemon
/// Called from webapp as window.__TAURI__.core.invoke('daemon_exec', {action, params})
#[tauri::command]
pub async fn daemon_exec(
    action: String,
    params: Option<serde_json::Value>,
) -> Result<ServiceResponse, String> {
    tokio::task::spawn_blocking(move || core_action(&action, params))
        .await
        .map_err(|e| format!("Task join error: {}", e))?
}

/// IPC command: get device UDID from daemon
#[tauri::command]
pub async fn get_udid() -> Result<ServiceResponse, String> {
    tokio::task::spawn_blocking(|| {
        let url = format!("{}/api/device/udid", service_base_url());
        let client = reqwest::blocking::Client::builder()
            .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
            .no_proxy()
            .build()
            .map_err(|e| format!("Failed to create HTTP client: {}", e))?;

        let response = client
            .get(&url)
            .send()
            .map_err(|e| format!("Failed to get UDID: {}", e))?;

        response
            .json::<ServiceResponse>()
            .map_err(|e| format!("Failed to parse UDID response: {}", e))
    })
    .await
    .map_err(|e| format!("Task join error: {}", e))?
}

/// IPC command: get current process PID
#[tauri::command]
pub fn get_pid() -> u32 {
    std::process::id()
}

/// IPC command: get platform info (no HTTP, pure local)
#[tauri::command]
pub fn get_platform_info() -> serde_json::Value {
    serde_json::json!({
        "os": std::env::consts::OS,
        "version": env!("CARGO_PKG_VERSION"),
    })
}

#[tauri::command]
pub async fn admin_reinstall_service() -> Result<String, String> {
    log::info!("[service] Admin service install requested");

    #[cfg(target_os = "macos")]
    {
        admin_reinstall_service_macos().await
    }

    #[cfg(target_os = "windows")]
    {
        admin_reinstall_service_windows().await
    }

    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        Err("Not supported on this platform".to_string())
    }
}

#[cfg(target_os = "macos")]
async fn admin_reinstall_service_macos() -> Result<String, String> {
    let service_path = "/Applications/Kaitu.app/Contents/MacOS/k2";
    if !std::path::Path::new(service_path).exists() {
        return Err(format!("Service not found: {}", service_path));
    }

    let script = format!(
        r#"do shell script "{} service install" with administrator privileges"#,
        service_path
    );

    let output = Command::new("osascript")
        .arg("-e")
        .arg(&script)
        .output()
        .map_err(|e| format!("osascript failed: {}", e))?;

    if output.status.success() {
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        if stderr.contains("User canceled") || stderr.contains("-128") {
            Err("User cancelled".to_string())
        } else {
            Err(format!("Failed: {}", stderr))
        }
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
        .output()
        .map_err(|e| format!("PowerShell failed: {}", e))?;

    if output.status.success() {
        Ok("Service installed and started".to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        Err(format!("Failed: {}", stderr))
    }
}

/// Detect old kaitu-service
pub fn detect_old_kaitu_service() -> bool {
    #[cfg(target_os = "macos")]
    {
        std::path::Path::new("/Library/LaunchDaemons/io.kaitu.service.plist").exists()
            || std::path::Path::new("/Library/LaunchDaemons/com.kaitu.service.plist").exists()
    }
    #[cfg(target_os = "windows")]
    {
        Command::new("sc")
            .args(["query", "kaitu-service"])
            .output()
            .map(|o| o.status.success())
            .unwrap_or(false)
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        false
    }
}

/// Cleanup old kaitu-service
pub fn cleanup_old_kaitu_service() {
    if !detect_old_kaitu_service() {
        return;
    }
    log::info!("[service] Cleaning up old kaitu-service");

    #[cfg(target_os = "macos")]
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
        let _ = Command::new("sc").args(["stop", "kaitu-service"]).output();
        let _ = Command::new("sc")
            .args(["delete", "kaitu-service"])
            .output();
    }
}

/// Decision outcome for whether admin reinstall is needed.
#[derive(Debug, Clone, PartialEq)]
pub enum InstallAction {
    NotNeeded,
    Needed { reason: String },
}

/// Pure decision function: given initial check + optional post-wait check,
/// determine whether admin install is needed.
///
/// - `VersionMatch` initial → NotNeeded (no wait needed)
/// - `VersionMismatch` initial → Needed immediately (service IS running, wrong version)
/// - `ServiceNotRunning` initial + post_wait=Some(VersionMatch) → NotNeeded (service started on its own)
/// - `ServiceNotRunning` initial + post_wait=Some(other) → Needed
/// - `ServiceNotRunning` initial + post_wait=None (timeout) → Needed
pub fn should_install(
    initial: &VersionCheckResult,
    post_wait: Option<&VersionCheckResult>,
) -> InstallAction {
    match initial {
        VersionCheckResult::VersionMatch => InstallAction::NotNeeded,
        VersionCheckResult::VersionMismatch {
            service_version,
            app_version,
        } => InstallAction::Needed {
            reason: format!(
                "mismatch: service={}, app={}",
                service_version, app_version
            ),
        },
        VersionCheckResult::ServiceNotRunning => match post_wait {
            Some(VersionCheckResult::VersionMatch) => InstallAction::NotNeeded,
            Some(VersionCheckResult::VersionMismatch {
                service_version,
                app_version,
            }) => InstallAction::Needed {
                reason: format!(
                    "mismatch after wait: service={}, app={}",
                    service_version, app_version
                ),
            },
            Some(VersionCheckResult::ServiceNotRunning) => InstallAction::Needed {
                reason: "still not running after wait".to_string(),
            },
            None => InstallAction::Needed {
                reason: "not running and wait timed out".to_string(),
            },
        },
    }
}

/// Wait for service to be reachable
pub fn wait_for_service(timeout_ms: u64, poll_interval_ms: u64) -> bool {
    let start = std::time::Instant::now();
    let timeout = std::time::Duration::from_millis(timeout_ms);
    let interval = std::time::Duration::from_millis(poll_interval_ms);

    while start.elapsed() < timeout {
        if ping_service() {
            log::info!("[service] Running (waited {:?})", start.elapsed());
            return true;
        }
        std::thread::sleep(interval);
    }
    log::warn!("[service] Not started within {:?}", timeout);
    false
}

/// Main entry: ensure service running with correct version
#[tauri::command]
pub async fn ensure_service_running(app_version: String) -> Result<(), String> {
    const POST_INSTALL_WAIT_MS: u64 = 5000;
    const PRE_INSTALL_WAIT_MS: u64 = 8000;
    const POLL_MS: u64 = 500;

    log::info!("[service] Ensuring service running (v{})", app_version);

    // Run blocking operations (reqwest::blocking) in a blocking thread
    // to avoid tokio "Cannot drop a runtime in async context" panic
    let ver = app_version.clone();
    let initial_check = tokio::task::spawn_blocking(move || {
        cleanup_old_kaitu_service();
        check_service_version(&ver)
    })
    .await
    .map_err(|e| format!("spawn_blocking failed: {}", e))?;

    // For ServiceNotRunning: wait first (service may be starting after PKG install),
    // then re-check before deciding to show the admin password prompt.
    // For VersionMismatch: no wait needed — service IS running, just wrong version.
    let action = match &initial_check {
        VersionCheckResult::VersionMatch => {
            log::info!("[service] Version match, nothing to do");
            should_install(&initial_check, None)
        }
        VersionCheckResult::VersionMismatch {
            service_version, ..
        } => {
            log::info!(
                "[service] Mismatch: service={}, app={}",
                service_version,
                app_version
            );
            should_install(&initial_check, None)
        }
        VersionCheckResult::ServiceNotRunning => {
            log::info!("[service] Not running — waiting for startup...");
            let ver = app_version.clone();
            let post_wait_check = tokio::task::spawn_blocking(move || {
                let started = wait_for_service(PRE_INSTALL_WAIT_MS, POLL_MS);
                log::info!("[service] Wait result: started={}", started);
                if started {
                    let result = check_service_version(&ver);
                    log::info!("[service] Post-wait check: {:?}", result);
                    Some(result)
                } else {
                    None
                }
            })
            .await
            .map_err(|e| format!("spawn_blocking failed: {}", e))?;

            should_install(&initial_check, post_wait_check.as_ref())
        }
    };

    match action {
        InstallAction::NotNeeded => return Ok(()),
        InstallAction::Needed { reason } => {
            log::info!("[service] Install needed: {}", reason);
        }
    }

    admin_reinstall_service().await?;

    let ver = app_version.clone();
    let ok = tokio::task::spawn_blocking(move || {
        if wait_for_service(POST_INSTALL_WAIT_MS, POLL_MS) {
            matches!(check_service_version(&ver), VersionCheckResult::VersionMatch)
        } else {
            false
        }
    })
    .await
    .map_err(|e| format!("spawn_blocking failed: {}", e))?;

    if ok {
        return Ok(());
    }
    Err("Failed to start service with correct version".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

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

    // --- should_install decision logic tests ---

    #[test]
    fn test_should_install_version_match() {
        let result = should_install(&VersionCheckResult::VersionMatch, None);
        assert_eq!(result, InstallAction::NotNeeded);
    }

    #[test]
    fn test_should_install_mismatch_immediate() {
        let initial = VersionCheckResult::VersionMismatch {
            service_version: "0.3.0".to_string(),
            app_version: "0.4.0".to_string(),
        };
        let result = should_install(&initial, None);
        match result {
            InstallAction::Needed { reason } => {
                assert!(reason.contains("mismatch"), "reason should mention mismatch: {}", reason);
            }
            InstallAction::NotNeeded => panic!("Expected Needed, got NotNeeded"),
        }
    }

    #[test]
    fn test_should_install_not_running_then_match() {
        let initial = VersionCheckResult::ServiceNotRunning;
        let post_wait = VersionCheckResult::VersionMatch;
        let result = should_install(&initial, Some(&post_wait));
        assert_eq!(result, InstallAction::NotNeeded);
    }

    #[test]
    fn test_should_install_not_running_then_mismatch() {
        let initial = VersionCheckResult::ServiceNotRunning;
        let post_wait = VersionCheckResult::VersionMismatch {
            service_version: "0.3.0".to_string(),
            app_version: "0.4.0".to_string(),
        };
        let result = should_install(&initial, Some(&post_wait));
        match result {
            InstallAction::Needed { .. } => {}
            InstallAction::NotNeeded => panic!("Expected Needed, got NotNeeded"),
        }
    }

    #[test]
    fn test_should_install_not_running_wait_timeout() {
        // post_wait=None means wait_for_service timed out
        let initial = VersionCheckResult::ServiceNotRunning;
        let result = should_install(&initial, None);
        match result {
            InstallAction::Needed { reason } => {
                assert!(reason.len() > 0, "reason should not be empty");
            }
            InstallAction::NotNeeded => panic!("Expected Needed, got NotNeeded"),
        }
    }

    #[test]
    fn test_should_install_not_running_then_still_not_running() {
        let initial = VersionCheckResult::ServiceNotRunning;
        let post_wait = VersionCheckResult::ServiceNotRunning;
        let result = should_install(&initial, Some(&post_wait));
        match result {
            InstallAction::Needed { .. } => {}
            InstallAction::NotNeeded => panic!("Expected Needed, got NotNeeded"),
        }
    }
}
