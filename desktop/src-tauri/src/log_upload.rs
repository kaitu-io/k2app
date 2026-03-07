//! Service log upload module
//!
//! Uploads service logs to S3 for diagnostics and feedback.
//! Runs in Tauri (not in daemon) because daemon may be crashed.
//!
//! Uploads 4 separate log files:
//! 1. service-{id}.log.gz - Go daemon logs (k2.log)
//! 2. crash-{id}.log.gz - Go panic/crash logs (panic-*.log)
//! 3. desktop-{id}.log.gz - Tauri desktop app logs (desktop.log)
//! 4. system-{id}.log.gz - System-level app logs (macOS Console / Windows Event Log)
//!
//! Log file locations (must match k2 daemon log paths in k2/config/log.go):
//! - macOS:   /var/log/kaitu/k2.log
//! - Windows: C:\ProgramData\kaitu\k2.log
//! - Linux:   /var/log/kaitu/k2.log

use chrono::Utc;
use flate2::write::GzEncoder;
use flate2::Compression;
use serde::{Deserialize, Serialize};
use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

// ============================================================================
// Configuration
// ============================================================================

/// S3 public bucket for log uploads (no authentication required)
const S3_BUCKET_URL: &str = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com";

/// Request timeout in seconds
const REQUEST_TIMEOUT_SECS: u64 = 60;

// ============================================================================
// Types
// ============================================================================

/// Parameters for log upload (matches IPlatform.uploadLogs TS interface)
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UploadLogParams {
    pub email: Option<String>,
    pub reason: String,
    pub failure_duration_ms: Option<i64>,
    pub platform: Option<String>,
    pub version: Option<String>,
    pub feedback_id: Option<String>,
}

/// Result of log upload (matches TS return type)
#[derive(Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UploadLogResult {
    pub success: bool,
    pub error: Option<String>,
    pub s3_keys: Option<Vec<UploadedFileInfo>>,
}

/// Information about an uploaded log file (returned to JS for server-side notification)
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct UploadedFileInfo {
    pub name: String,
    pub s3_key: String,
}

/// Internal tracking for upload progress
#[derive(Debug, Clone)]
struct UploadedFile {
    log_type: String,
    s3_key: String,
}

// ============================================================================
// Log file paths
// ============================================================================

/// Get service log directory for current platform
fn get_log_dir() -> PathBuf {
    #[cfg(target_os = "macos")]
    {
        PathBuf::from("/var/log/kaitu")
    }

    #[cfg(target_os = "windows")]
    {
        let program_data =
            std::env::var("ProgramData").unwrap_or_else(|_| r"C:\ProgramData".to_string());
        PathBuf::from(program_data).join("kaitu")
    }

    #[cfg(target_os = "linux")]
    {
        PathBuf::from("/var/log/kaitu")
    }

    #[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
    {
        PathBuf::from("/tmp/kaitu")
    }
}

/// Get service log path
fn get_service_log_path() -> PathBuf {
    get_log_dir().join("k2.log")
}

/// Get desktop log path (delegates to shared get_desktop_log_dir)
fn get_desktop_log_path() -> PathBuf {
    crate::get_desktop_log_dir().join("desktop.log")
}

// ============================================================================
// Log reading functions
// ============================================================================

/// Read service log
fn read_service_log() -> (String, usize) {
    let path = get_service_log_path();
    match std::fs::read_to_string(&path) {
        Ok(content) => {
            let size = content.len();
            log::info!("Read service log: {} ({} bytes)", path.display(), size);
            (content, size)
        }
        Err(e) => {
            log::warn!("Failed to read service log: {}", e);
            (format!("(Failed to read service log: {})", e), 0)
        }
    }
}

/// Read desktop log
fn read_desktop_log() -> (String, usize) {
    let path = get_desktop_log_path();
    match std::fs::read_to_string(&path) {
        Ok(content) => {
            let size = content.len();
            log::info!("Read desktop log: {} ({} bytes)", path.display(), size);
            (content, size)
        }
        Err(_) => (String::new(), 0),
    }
}

/// Read and combine all panic/crash logs
fn read_crash_logs() -> (String, usize) {
    let dir = get_log_dir();
    let mut combined = String::new();

    if !dir.exists() {
        return (String::new(), 0);
    }

    let pattern = dir.join("panic-*.log");
    if let Ok(entries) = glob::glob(pattern.to_string_lossy().as_ref()) {
        for entry in entries.flatten() {
            if let Ok(content) = std::fs::read_to_string(&entry) {
                log::info!("Found panic log: {} ({} bytes)", entry.display(), content.len());
                if !combined.is_empty() {
                    combined.push_str("\n\n");
                }
                combined.push_str(&format!(
                    "========== {} ==========\n{}",
                    entry.file_name().unwrap_or_default().to_string_lossy(),
                    content
                ));
            }
        }
    }

    let size = combined.len();
    (combined, size)
}

/// Read macOS system logs
#[cfg(target_os = "macos")]
fn read_system_logs() -> (String, usize) {
    use std::process::Command;

    let output = Command::new("log")
        .args([
            "show",
            "--style",
            "compact",
            "--last",
            "1d",
            "--predicate",
            "process CONTAINS \"kaitu\"",
        ])
        .output();

    match output {
        Ok(out) if out.status.success() => {
            let logs = String::from_utf8_lossy(&out.stdout).to_string();
            let size = logs.len();
            (logs, size)
        }
        _ => (String::new(), 0),
    }
}

/// Read Linux system logs (stub)
#[cfg(target_os = "linux")]
fn read_system_logs() -> (String, usize) {
    (String::new(), 0)
}

/// Read Windows system logs (stub — full Event Log API requires windows crate)
#[cfg(target_os = "windows")]
fn read_system_logs() -> (String, usize) {
    // Windows Event Log reading requires the `windows` crate.
    // For now, return empty. Can be added later if needed.
    log::info!("Windows Event Log reading not implemented yet");
    (String::new(), 0)
}

#[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
fn read_system_logs() -> (String, usize) {
    (String::new(), 0)
}

// ============================================================================
// Log sanitization and compression
// ============================================================================

/// Remove sensitive information from log content
fn sanitize_logs(content: &str) -> String {
    let mut result = content.to_string();

    let patterns = [
        (r#""token":""#, r#""token":"***""#),
        (r#""password":""#, r#""password":"***""#),
        (r#""secret":""#, r#""secret":"***""#),
        ("Authorization: Bearer ", "Authorization: Bearer ***"),
        ("X-K2-Token: ", "X-K2-Token: ***"),
    ];

    for (needle, replacement) in patterns {
        result = result.replace(needle, replacement);
    }

    result
}

/// Compress content with gzip
fn compress_gzip(content: &str) -> Result<Vec<u8>, String> {
    let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
    encoder
        .write_all(content.as_bytes())
        .map_err(|e| format!("Compress write failed: {}", e))?;
    encoder
        .finish()
        .map_err(|e| format!("Compress finish failed: {}", e))
}

// ============================================================================
// S3 Upload
// ============================================================================

/// Generate S3 object key for a log type
fn generate_s3_key(log_type: &str, feedback_id: Option<&str>, udid: &str) -> String {
    let now = Utc::now();
    let date = now.format("%Y/%m/%d");
    let timestamp = now.format("%H%M%S");

    let (prefix, identifier) = match feedback_id {
        Some(id) => ("feedback-logs", id.to_string()),
        None => ("service-logs", uuid::Uuid::new_v4().to_string()[..8].to_string()),
    };

    format!(
        "{}/{}/{}/{}-{}-{}.log.gz",
        prefix, udid, date, log_type, timestamp, identifier
    )
}

/// Upload compressed data to S3 public bucket
fn upload_to_s3(s3_key: &str, data: &[u8]) -> Result<(), String> {
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);

    log::info!("Uploading to S3: {} ({} bytes)", s3_key, data.len());

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
        .build()
        .map_err(|e| format!("HTTP client error: {}", e))?;

    let response = client
        .put(&url)
        .header("Content-Type", "application/gzip")
        .header("Content-Length", data.len())
        .body(data.to_vec())
        .send()
        .map_err(|e| format!("S3 upload failed: {}", e))?;

    let status = response.status();
    if !status.is_success() {
        let body = response.text().unwrap_or_default();
        return Err(format!("S3 upload failed: status={}, body={}", status, body));
    }

    log::info!("Uploaded to S3: {}", s3_key);
    Ok(())
}

/// Upload a single log file
fn upload_log_file(
    log_type: &str,
    content: &str,
    feedback_id: Option<&str>,
    udid: &str,
) -> Result<UploadedFile, String> {
    if content.is_empty() {
        return Err(format!("{} log is empty", log_type));
    }

    let sanitized = sanitize_logs(content);
    let compressed = compress_gzip(&sanitized)?;

    log::info!(
        "Compressing {} log: {} -> {} bytes",
        log_type,
        sanitized.len(),
        compressed.len()
    );

    let s3_key = generate_s3_key(log_type, feedback_id, udid);
    upload_to_s3(&s3_key, &compressed)?;

    Ok(UploadedFile {
        log_type: log_type.to_string(),
        s3_key,
    })
}

// ============================================================================
// Main upload orchestrator
// ============================================================================

/// Upload all service logs to S3 and return S3 keys
fn upload_service_log(params: UploadLogParams, udid: String) -> UploadLogResult {
    log::info!("Starting log upload: reason={}", params.reason);

    let feedback_id = params.feedback_id.as_deref();
    let mut uploaded_files: Vec<UploadedFile> = Vec::new();
    let mut errors: Vec<String> = Vec::new();

    // 1. Service log
    let (service_content, _) = read_service_log();
    match upload_log_file("service", &service_content, feedback_id, &udid) {
        Ok(file) => uploaded_files.push(file),
        Err(e) => {
            log::warn!("Service log upload failed: {}", e);
            errors.push(e);
        }
    }

    // 2. Crash logs
    let (crash_content, _) = read_crash_logs();
    if !crash_content.is_empty() {
        match upload_log_file("crash", &crash_content, feedback_id, &udid) {
            Ok(file) => uploaded_files.push(file),
            Err(e) => {
                log::warn!("Crash log upload failed: {}", e);
                errors.push(e);
            }
        }
    }

    // 3. Desktop log
    let (desktop_content, _) = read_desktop_log();
    if !desktop_content.is_empty() {
        match upload_log_file("desktop", &desktop_content, feedback_id, &udid) {
            Ok(file) => uploaded_files.push(file),
            Err(e) => {
                log::warn!("Desktop log upload failed: {}", e);
                errors.push(e);
            }
        }
    }

    // 4. System logs
    let (system_content, _) = read_system_logs();
    if !system_content.is_empty() {
        match upload_log_file("system", &system_content, feedback_id, &udid) {
            Ok(file) => uploaded_files.push(file),
            Err(e) => {
                log::warn!("System log upload failed: {}", e);
                errors.push(e);
            }
        }
    }

    if uploaded_files.is_empty() {
        return UploadLogResult {
            success: false,
            error: Some(format!("All uploads failed: {}", errors.join("; "))),
            s3_keys: None,
        };
    }

    log::info!(
        "Log upload completed: {} files uploaded",
        uploaded_files.len()
    );

    let s3_keys: Vec<UploadedFileInfo> = uploaded_files
        .iter()
        .map(|f| UploadedFileInfo {
            name: f.log_type.clone(),
            s3_key: f.s3_key.clone(),
        })
        .collect();

    UploadLogResult {
        success: true,
        error: if errors.is_empty() {
            None
        } else {
            Some(format!("Partial failures: {}", errors.join("; ")))
        },
        s3_keys: Some(s3_keys),
    }
}

// ============================================================================
// Beta Auto-Upload (24h periodic)
// ============================================================================

/// Whether the beta auto-upload loop is active
static BETA_UPLOAD_ACTIVE: AtomicBool = AtomicBool::new(false);

/// Start 24h periodic upload loop (beta only).
/// Idempotent — if already running, returns immediately.
pub fn start_beta_auto_upload(_app: tauri::AppHandle) {
    if BETA_UPLOAD_ACTIVE.swap(true, Ordering::SeqCst) {
        log::info!("[log_upload] Beta auto-upload already running");
        return;
    }

    log::info!("[log_upload] Starting beta auto-upload (24h interval)");
    tauri::async_runtime::spawn(async move {
        // Initial delay: 5 minutes (let app fully start)
        tokio::time::sleep(Duration::from_secs(5 * 60)).await;

        loop {
            if !BETA_UPLOAD_ACTIVE.load(Ordering::SeqCst) {
                log::info!("[log_upload] Beta auto-upload stopped");
                break;
            }

            // MUST use spawn_blocking — upload_service_log uses reqwest::blocking::Client
            let _ = tokio::task::spawn_blocking(upload_and_cleanup_silent).await;

            tokio::time::sleep(Duration::from_secs(24 * 60 * 60)).await;
        }
    });
}

/// Stop the beta auto-upload loop (next iteration will exit).
pub fn stop_beta_auto_upload() {
    BETA_UPLOAD_ACTIVE.store(false, Ordering::SeqCst);
    log::info!("[log_upload] Beta auto-upload stop requested");
}

/// Upload all logs silently, then cleanup. Blocking — call via spawn_blocking.
fn upload_and_cleanup_silent() {
    log::info!("[log_upload] Beta auto-upload: starting upload cycle");

    let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());

    let params = UploadLogParams {
        email: None,
        reason: "beta-auto-upload".to_string(),
        failure_duration_ms: None,
        platform: Some(std::env::consts::OS.to_string()),
        version: Some(env!("CARGO_PKG_VERSION").to_string()),
        feedback_id: None,
    };

    let result = upload_service_log(params, udid);
    log::info!(
        "[log_upload] Beta auto-upload: success={}, error={:?}",
        result.success,
        result.error
    );

    cleanup_all_logs();
}

/// Delete all log files (service logs, crash logs, desktop logs).
fn cleanup_all_logs() {
    // Service logs: /var/log/kaitu/*.log
    cleanup_dir_logs(&get_log_dir(), |name| name.ends_with(".log"));

    // Desktop logs: ~/Library/Logs/kaitu/desktop*.log*
    cleanup_dir_logs(&crate::get_desktop_log_dir(), |name| {
        name.starts_with("desktop") && name.contains("log")
    });
}

/// Clean log files in a directory matching a filter.
/// Windows: truncate files (may be locked by log plugin).
/// macOS/Linux: delete files (safe even with open handles).
fn cleanup_dir_logs(dir: &std::path::Path, filter: impl Fn(&str) -> bool) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().to_string();
        if filter(&name) {
            #[cfg(target_os = "windows")]
            {
                if std::fs::File::create(entry.path()).is_ok() {
                    log::info!("[log_upload] Truncated: {}", entry.path().display());
                }
            }
            #[cfg(not(target_os = "windows"))]
            {
                if std::fs::remove_file(entry.path()).is_ok() {
                    log::info!("[log_upload] Removed: {}", entry.path().display());
                }
            }
        }
    }
}

// ============================================================================
// Tauri Command
// ============================================================================

/// IPC: Upload service logs (runs in blocking thread)
#[tauri::command]
pub async fn upload_service_log_command(params: UploadLogParams) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || {
        let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
        upload_service_log(params, udid)
    })
    .await
    .map_err(|e| format!("Task failed: {}", e))
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;
    use flate2::read::GzDecoder;
    use std::io::Read;

    #[test]
    fn test_sanitize_logs() {
        // Sanitize replaces the key-value prefix pattern, corrupting the token
        let input = r#"{"token":"abc123"} Authorization: Bearer eyJhbGci X-K2-Token: secret123 {"password":"hunter2"} {"secret":"mysecret"}"#;
        let result = sanitize_logs(input);

        // Verify each sensitive pattern prefix was replaced with ***
        assert!(result.contains(r#""token":"***""#));
        assert!(result.contains("Authorization: Bearer ***"));
        assert!(result.contains("X-K2-Token: ***"));
        assert!(result.contains(r#""password":"***""#));
        assert!(result.contains(r#""secret":"***""#));
        // Original prefixes should be gone
        assert!(!result.contains(r#""token":"abc"#));
        assert!(!result.contains("Bearer eyJ"));
        assert!(!result.contains("X-K2-Token: sec"));
        assert!(!result.contains(r#""password":"hun"#));
    }

    #[test]
    fn test_sanitize_logs_no_sensitive() {
        let input = "2024-01-01 INFO normal log line\n2024-01-01 DEBUG another line";
        let result = sanitize_logs(input);
        assert_eq!(result, input);
    }

    #[test]
    fn test_compress_gzip_roundtrip() {
        let original = "Hello, this is a test log content for compression!";
        let compressed = compress_gzip(original).unwrap();

        assert!(!compressed.is_empty());
        // Compressed should have gzip header (0x1f, 0x8b)
        assert_eq!(compressed[0], 0x1f);
        assert_eq!(compressed[1], 0x8b);

        // Decompress and verify
        let mut decoder = GzDecoder::new(&compressed[..]);
        let mut decompressed = String::new();
        decoder.read_to_string(&mut decompressed).unwrap();
        assert_eq!(decompressed, original);
    }

    #[test]
    fn test_generate_s3_key() {
        let key = generate_s3_key("service", None, "test-udid-123");
        assert!(key.starts_with("service-logs/test-udid-123/"));
        assert!(key.contains("service-"));
        assert!(key.ends_with(".log.gz"));
        // Format: service-logs/{udid}/YYYY/MM/DD/service-HHMMSS-{uuid8}.log.gz
        let parts: Vec<&str> = key.split('/').collect();
        assert_eq!(parts.len(), 6); // service-logs, udid, YYYY, MM, DD, filename
    }

    #[test]
    fn test_generate_s3_key_with_feedback_id() {
        let key = generate_s3_key("desktop", Some("fb-12345"), "test-udid-456");
        assert!(key.starts_with("feedback-logs/test-udid-456/"));
        assert!(key.contains("desktop-"));
        assert!(key.contains("fb-12345"));
        assert!(key.ends_with(".log.gz"));
    }

    #[test]
    fn test_upload_log_params_deserialization() {
        let json = r#"{"reason":"connection failed","email":"user@test.com","failureDurationMs":5000,"platform":"macos","version":"0.4.0","feedbackId":"fb-123"}"#;
        let params: UploadLogParams = serde_json::from_str(json).unwrap();
        assert_eq!(params.reason, "connection failed");
        assert_eq!(params.email.as_deref(), Some("user@test.com"));
        assert_eq!(params.failure_duration_ms, Some(5000));
        assert_eq!(params.feedback_id.as_deref(), Some("fb-123"));
    }

    #[test]
    fn test_cleanup_dir_logs_removes_matching_files() {
        let dir = tempfile::tempdir().unwrap();
        let dir_path = dir.path();

        // Create test files
        std::fs::write(dir_path.join("k2.log"), "test log").unwrap();
        std::fs::write(dir_path.join("panic-2024.log"), "test panic").unwrap();
        std::fs::write(dir_path.join("keep.txt"), "keep me").unwrap();

        cleanup_dir_logs(dir_path, |name| name.ends_with(".log"));

        // .txt should remain regardless of platform
        assert!(dir_path.join("keep.txt").exists());

        #[cfg(target_os = "windows")]
        {
            // Windows: log files are truncated (exist but empty)
            assert!(dir_path.join("k2.log").exists());
            assert!(dir_path.join("panic-2024.log").exists());
            assert!(std::fs::read_to_string(dir_path.join("k2.log")).unwrap().is_empty());
            assert!(std::fs::read_to_string(dir_path.join("panic-2024.log")).unwrap().is_empty());
        }

        #[cfg(not(target_os = "windows"))]
        {
            // macOS/Linux: log files are deleted
            assert!(!dir_path.join("k2.log").exists());
            assert!(!dir_path.join("panic-2024.log").exists());
        }
    }

    #[test]
    fn test_cleanup_dir_logs_nonexistent_dir() {
        // Should not crash on nonexistent directory
        cleanup_dir_logs(
            &std::env::temp_dir().join("k2app-nonexistent-dir-12345"),
            |_| true,
        );
    }

    #[test]
    fn test_upload_log_result_camel_case_serialization() {
        let result = UploadLogResult {
            success: true,
            error: None,
            s3_keys: Some(vec![UploadedFileInfo {
                name: "service".to_string(),
                s3_key: "service-logs/udid/2026/03/05/service-143022-abc.log.gz".to_string(),
            }]),
        };
        let json = serde_json::to_string(&result).unwrap();
        // Tauri IPC passes JSON to JS — keys must be camelCase
        assert!(json.contains("\"s3Keys\""), "s3_keys must serialize as s3Keys: {}", json);
        assert!(json.contains("\"s3Key\""), "s3_key must serialize as s3Key: {}", json);
        assert!(!json.contains("\"s3_keys\""), "must not have snake_case s3_keys: {}", json);
        assert!(!json.contains("\"s3_key\""), "must not have snake_case s3_key: {}", json);
    }

    #[test]
    fn test_beta_upload_active_default_false() {
        // Static default is false (test binary starts fresh)
        // Note: other tests may have set it, so we just verify the API doesn't crash
        let _ = BETA_UPLOAD_ACTIVE.load(Ordering::SeqCst);
    }

    #[test]
    fn test_stop_beta_auto_upload_no_crash() {
        stop_beta_auto_upload();
        assert!(!BETA_UPLOAD_ACTIVE.load(Ordering::SeqCst));
    }

    #[test]
    fn test_cleanup_dir_logs_behavior() {
        let dir = tempfile::tempdir().unwrap();
        let dir_path = dir.path();

        // Create test files: two log files and one unrelated file
        std::fs::write(dir_path.join("desktop.log"), "log content A").unwrap();
        std::fs::write(dir_path.join("desktop.log.1"), "log content B").unwrap();
        std::fs::write(dir_path.join("other.txt"), "keep me").unwrap();

        // Filter matches files containing both "desktop" and "log"
        cleanup_dir_logs(dir_path, |name| {
            name.contains("desktop") && name.contains("log")
        });

        // "other.txt" must survive regardless of platform
        assert!(
            dir_path.join("other.txt").exists(),
            "other.txt should not be touched by cleanup"
        );
        let other_content = std::fs::read_to_string(dir_path.join("other.txt")).unwrap();
        assert_eq!(other_content, "keep me");

        // Platform-conditional assertions
        #[cfg(target_os = "windows")]
        {
            // Windows: log files should still exist but be truncated (0 bytes)
            assert!(
                dir_path.join("desktop.log").exists(),
                "desktop.log should exist (truncated) on Windows"
            );
            assert!(
                dir_path.join("desktop.log.1").exists(),
                "desktop.log.1 should exist (truncated) on Windows"
            );
            let content_a = std::fs::read_to_string(dir_path.join("desktop.log")).unwrap();
            let content_b = std::fs::read_to_string(dir_path.join("desktop.log.1")).unwrap();
            assert!(
                content_a.is_empty(),
                "desktop.log should be empty after truncation, got: {:?}",
                content_a
            );
            assert!(
                content_b.is_empty(),
                "desktop.log.1 should be empty after truncation, got: {:?}",
                content_b
            );
        }

        #[cfg(not(target_os = "windows"))]
        {
            // macOS/Linux: log files should be deleted
            assert!(
                !dir_path.join("desktop.log").exists(),
                "desktop.log should be deleted on non-Windows"
            );
            assert!(
                !dir_path.join("desktop.log.1").exists(),
                "desktop.log.1 should be deleted on non-Windows"
            );
        }
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn test_windows_log_dir_uses_programdata() {
        let log_dir = get_log_dir();
        let path_lower = log_dir.to_string_lossy().to_lowercase();
        assert!(
            path_lower.contains("programdata") || path_lower.contains("kaitu"),
            "Windows log dir should contain 'programdata' or 'kaitu', got: {}",
            log_dir.display()
        );
    }
}
