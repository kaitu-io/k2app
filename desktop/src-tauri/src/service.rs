//! Service communication module for k2 daemon

use serde::{Deserialize, Serialize};
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

/// IPC command: proxy VPN action to the k2 daemon HTTP API at :1777.
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

/// IPC command: hot-switch daemon log level.
///
/// Beta channel forces debug level — any other level is rejected.
/// POST /api/log-level to k2 daemon.
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

    #[cfg(target_os = "macos")]
    {
        tokio::task::spawn_blocking(admin_reinstall_service_macos)
            .await
            .map_err(|e| format!("Task join error: {}", e))?
    }

    #[cfg(target_os = "windows")]
    {
        admin_reinstall_service_windows().await
    }

    // Linux does not ship the Tauri shell — cmd/k2 handles everything
    // through the install.sh path. If cargo test ever compiles this on
    // Linux (CI hwid gate job), return a clear error rather than
    // pretending an admin reinstall path exists.
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        Err("admin_reinstall_service is not supported on this platform".to_string())
    }
}

/// Sentinel exit code: elevation itself failed (UAC declined / launch error).
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
const ELEVATION_FAILED_EXIT: i32 = 997;
/// Sentinel exit code: elevated process ran but its exit code was unreadable
/// (rare `-Verb RunAs` handle-rights quirk / constrained language mode).
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
const EXIT_CODE_UNREADABLE_EXIT: i32 = 998;

/// Build the PowerShell command that elevates `k2.exe service install` and
/// propagates its real exit code back to the (hidden) outer powershell.
///
/// Window-safety invariant: the ONLY process launched via `-Verb RunAs` is
/// k2.exe with `-WindowStyle Hidden` — byte-identical to the long-shipped
/// invocation except for `-PassThru` (which only returns a Process object).
/// No nested powershell/cmd is ever elevated: console hosts launched through
/// ShellExecute can flash a console window before `-WindowStyle` applies.
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
fn build_admin_install_ps_script(service_path: &str) -> String {
    format!(
        r#"try {{ $p = Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden -PassThru; if ($null -ne $p.ExitCode) {{ exit $p.ExitCode }} else {{ exit {} }} }} catch {{ Write-Error $_; exit {} }}"#,
        service_path, EXIT_CODE_UNREADABLE_EXIT, ELEVATION_FAILED_EXIT
    )
}

/// Interpret the outer powershell exit code into the install result.
/// Only exit 0 is success; sentinels get dedicated messages so desktop.log
/// tells apart "UAC declined" / "code unreadable" / "k2 install failed".
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
fn interpret_admin_install_exit(code: Option<i32>, stderr: &str) -> Result<String, String> {
    match code {
        Some(0) => Ok("Service installed and started".to_string()),
        Some(ELEVATION_FAILED_EXIT) => Err(format!(
            "elevation failed (UAC declined or launch error): {}",
            stderr.trim()
        )),
        Some(EXIT_CODE_UNREADABLE_EXIT) => Err(
            "k2 service install ran but exit code was unreadable (see k2-service-install.log)"
                .to_string(),
        ),
        Some(c) => Err(format!(
            "k2 service install failed with exit code {} (see k2-service-install.log): {}",
            c,
            stderr.trim()
        )),
        None => Err(format!(
            "powershell terminated without exit code: {}",
            stderr.trim()
        )),
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

    let ps_script = build_admin_install_ps_script(&service_path.display().to_string());

    let output = Command::new("powershell")
        .args(["-NoProfile", "-WindowStyle", "Hidden", "-Command", &ps_script])
        .creation_flags(CREATE_NO_WINDOW)
        .output()
        .map_err(|e| format!("PowerShell failed: {}", e))?;

    let stderr = String::from_utf8_lossy(&output.stderr);
    interpret_admin_install_exit(output.status.code(), &stderr)
}

/// Best-effort tail of the k2-side install log (written by `k2 service
/// install` into the service log dir). Pure diagnostics — never affects
/// control flow.
#[cfg(target_os = "windows")]
fn read_install_log_tail() -> Option<String> {
    let program_data =
        std::env::var("ProgramData").unwrap_or_else(|_| r"C:\ProgramData".to_string());
    let path = std::path::PathBuf::from(program_data)
        .join("kaitu")
        .join("k2-service-install.log");
    let content = std::fs::read_to_string(&path).ok()?;
    const TAIL: usize = 2048;
    let start = content.len().saturating_sub(TAIL);
    // Avoid splitting a UTF-8 char at the cut point.
    let tail_start = (start..content.len())
        .find(|&i| content.is_char_boundary(i))
        .unwrap_or(content.len());
    Some(content[tail_start..].to_string())
}

#[cfg(not(target_os = "windows"))]
fn read_install_log_tail() -> Option<String> {
    None
}

/// macOS: install k2 service with admin privileges via osascript.
/// Uses the k2 sidecar binary at Contents/MacOS/k2 relative to the running app.
/// The osascript dialog prompts the user for their admin password.
#[cfg(target_os = "macos")]
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
            .creation_flags(CREATE_NO_WINDOW)
            .output()
            .map(|o| o.status.success())
            .unwrap_or(false)
    }
    // Tauri shell is macOS + Windows only; no legacy kaitu-service.service
    // ever shipped on Linux, so nothing to migrate.
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
        let _ = Command::new("sc")
            .args(["stop", "kaitu-service"])
            .creation_flags(CREATE_NO_WINDOW)
            .output();
        let _ = Command::new("sc")
            .args(["delete", "kaitu-service"])
            .creation_flags(CREATE_NO_WINDOW)
            .output();
    }
    // Tauri shell does not ship on Linux — detect_old_kaitu_service()
    // returned false above and this path is unreachable on Linux. The
    // compiler still needs a valid function body though.
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
/// Version check as single source of truth.
/// Phase 1: cleanup legacy services + wait for version match (8s).
///          Covers: already running, cold start via launchd, dev bypass.
/// Phase 2: osascript admin install (only if phase 1 fails).
/// Phase 3: post-install verification — wait for version match (5s).
#[tauri::command]
pub async fn ensure_service_running(app_version: String) -> Result<(), String> {
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

    // The reported install result is diagnostics only — phase 3 (actual
    // service state) is the sole arbiter. A spurious failure report (e.g.
    // `-Verb RunAs` exit-code quirk, constrained language mode) must not
    // fail startup when the service actually came up; conversely a
    // spurious success (as before -PassThru) must not skip verification.
    let install_error = match admin_reinstall_service().await {
        Ok(msg) => {
            log::info!("[service] Admin install succeeded: {}", msg);
            None
        }
        Err(e) => {
            log::error!("[service] Admin install reported failure: {}", e);
            Some(e)
        }
    };

    // Phase 3: verify post-install — ground truth.
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
        if install_error.is_some() {
            log::warn!(
                "[service] Install reported failure but service is running with the correct version — treating as success"
            );
        }
        log::info!("[service] Service ready after install");
        Ok(())
    } else {
        // Surface the k2-side install log so the failure cause lands in
        // desktop.log (and thus in feedback log uploads).
        if let Some(tail) = read_install_log_tail() {
            log::error!("[service] k2-service-install.log tail:\n{}", tail);
        }
        match install_error {
            Some(e) => Err(format!("Service did not start after install: {}", e)),
            None => Err(
                "Service did not start with correct version after install".to_string(),
            ),
        }
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

    // ServiceResponse serialization round-trip — verifies the canonical
    // {code, message, data} shape is preserved across JSON boundaries.
    #[test]
    fn test_service_response_serialization() {
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

    // -------------------------------------------------------------------
    // Admin install script + exit interpretation (cross-platform pure fns)
    // -------------------------------------------------------------------

    /// Window-safety contract: the only -Verb RunAs target is k2.exe itself.
    /// Elevating a console host (powershell/cmd) via ShellExecute can flash
    /// a console window before -WindowStyle applies — never allowed here.
    #[test]
    fn test_admin_install_script_never_elevates_console_host() {
        let script = build_admin_install_ps_script(r"C:\Program Files\Kaitu\k2.exe");
        assert!(script.contains(r"-FilePath 'C:\Program Files\Kaitu\k2.exe'"));
        assert!(!script.to_lowercase().contains("-filepath 'powershell"));
        assert!(!script.to_lowercase().contains("-filepath 'cmd"));
        // Exactly one process launch in the script
        assert_eq!(script.matches("Start-Process").count(), 1);
    }

    #[test]
    fn test_admin_install_script_shape() {
        let script = build_admin_install_ps_script(r"C:\Program Files\Kaitu\k2.exe");
        // Exit code must be captured and propagated
        assert!(script.contains("-PassThru"));
        assert!(script.contains("exit $p.ExitCode"));
        // Hidden window flag preserved from the shipped invocation
        assert!(script.contains("-WindowStyle Hidden"));
        assert!(script.contains("-Verb RunAs -Wait"));
        // Sentinels: null ExitCode → 998, launch/UAC failure → 997
        assert!(script.contains("exit 998"));
        assert!(script.contains("exit 997"));
        // UAC decline must be caught, not left as an unhandled error
        assert!(script.starts_with("try {"));
        assert!(script.contains("catch"));
        assert!(script.contains("'service','install'"));
    }

    #[test]
    fn test_interpret_admin_install_exit_success() {
        let r = interpret_admin_install_exit(Some(0), "");
        assert_eq!(r.unwrap(), "Service installed and started");
    }

    #[test]
    fn test_interpret_admin_install_exit_sentinels_and_failures() {
        // 997: UAC declined / launch error — stderr carries the reason
        let e = interpret_admin_install_exit(Some(997), "The operation was canceled by the user.")
            .unwrap_err();
        assert!(e.contains("UAC declined"));
        assert!(e.contains("canceled by the user"));

        // 998: ran but exit code unreadable — points at the k2-side log
        let e = interpret_admin_install_exit(Some(998), "").unwrap_err();
        assert!(e.contains("unreadable"));
        assert!(e.contains("k2-service-install.log"));

        // Real k2 exit code — surfaced verbatim
        let e = interpret_admin_install_exit(Some(1), "").unwrap_err();
        assert!(e.contains("exit code 1"));
        assert!(e.contains("k2-service-install.log"));

        // Killed / no exit code
        let e = interpret_admin_install_exit(None, "boom").unwrap_err();
        assert!(e.contains("without exit code"));
    }

    // -------------------------------------------------------------------
    // Windows-specific tests (only compiled + run on Windows)
    // -------------------------------------------------------------------
    #[cfg(target_os = "windows")]
    mod windows_tests {
        use super::super::*;

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
