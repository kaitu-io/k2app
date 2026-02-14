//! Service communication module for k2 daemon

use serde::{Deserialize, Serialize};
use std::process::Command;

const SERVICE_BASE_URL: &str = "http://127.0.0.1:1777";
const REQUEST_TIMEOUT_SECS: u64 = 5;

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
    let url = format!("{}/api/core", SERVICE_BASE_URL);
    let body = CoreRequest {
        action: action.to_string(),
        params,
    };

    log::debug!("Calling core action: {}", action);

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
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
    let url = format!("{}/ping", SERVICE_BASE_URL);
    let client = match reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(2))
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

/// Stop VPN synchronously (for app exit)
pub fn stop_vpn() {
    log::info!("Stopping VPN before exit...");
    match core_action("down", None) {
        Ok(resp) if resp.code == 0 => log::info!("VPN stopped"),
        Ok(resp) => log::warn!("VPN stop code {}: {}", resp.code, resp.message),
        Err(e) => log::warn!("Failed to stop VPN: {}", e),
    }
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
        r#"do shell script "{} run --install" with administrator privileges"#,
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
        r#"Start-Process -FilePath '{}' -ArgumentList 'run','--install' -Verb RunAs -Wait -WindowStyle Hidden"#,
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
    const WAIT_MS: u64 = 5000;
    const POLL_MS: u64 = 500;

    log::info!("[service] Ensuring service running (v{})", app_version);

    cleanup_old_kaitu_service();

    match check_service_version(&app_version) {
        VersionCheckResult::VersionMatch => return Ok(()),
        VersionCheckResult::VersionMismatch {
            service_version, ..
        } => {
            log::info!(
                "[service] Mismatch: service={}, app={}",
                service_version,
                app_version
            );
        }
        VersionCheckResult::ServiceNotRunning => {
            log::info!("[service] Not running");
        }
    }

    admin_reinstall_service().await?;

    if wait_for_service(WAIT_MS, POLL_MS) {
        if let VersionCheckResult::VersionMatch = check_service_version(&app_version) {
            return Ok(());
        }
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
}
