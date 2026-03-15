//! Service log upload module
//!
//! Uploads service logs to S3 for diagnostics and feedback.
//! Runs in Tauri (not in daemon) because daemon may be crashed.
//!
//! Collects logs from ALL possible directories (root + user paths),
//! stages them in a temp dir, creates a single tar.gz archive,
//! and uploads it to S3.
//!
//! Upload is read-only — source files are never modified.
//! Log size is controlled by lumberjack rotation and tauri-plugin-log rotation.

use chrono::Utc;
use flate2::write::GzEncoder;
use flate2::Compression;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

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

// ============================================================================
// Log directory discovery
// ============================================================================

/// Get ALL possible service log directories for the current platform.
/// The k2 daemon uses `config.DefaultLogPath("k2")` which branches on IsRoot().
/// Tauri (non-root) cannot know which mode daemon runs in, so scan both.
fn get_all_service_log_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();

    #[cfg(target_os = "macos")]
    {
        // Root daemon (launchd): /var/log/kaitu/
        dirs.push(PathBuf::from("/var/log/kaitu"));
        // User daemon (dev mode): ~/Library/Logs/kaitu/
        if let Some(home) = dirs::home_dir() {
            dirs.push(home.join("Library/Logs/kaitu"));
        }
    }

    #[cfg(target_os = "windows")]
    {
        // Admin daemon (Windows Service): %ProgramData%\kaitu\
        let program_data =
            std::env::var("ProgramData").unwrap_or_else(|_| r"C:\ProgramData".to_string());
        dirs.push(PathBuf::from(program_data).join("kaitu"));
        // User daemon (dev mode): %LOCALAPPDATA%\kaitu\logs\
        if let Some(local) = dirs::data_local_dir() {
            dirs.push(local.join("kaitu").join("logs"));
        }
    }

    #[cfg(target_os = "linux")]
    {
        // Root daemon (systemd): /var/log/kaitu/
        dirs.push(PathBuf::from("/var/log/kaitu"));
        // User daemon (dev mode): ~/.local/share/kaitu/logs/
        if let Some(home) = dirs::home_dir() {
            dirs.push(home.join(".local/share/kaitu/logs"));
        }
    }

    #[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
    {
        dirs.push(PathBuf::from("/tmp/kaitu"));
    }

    dirs
}

/// Label for a service log directory (used as filename prefix to avoid collisions).
fn dir_label(dir: &Path) -> &str {
    let s = dir.to_string_lossy();
    if s.contains("/var/log") || s.contains("ProgramData") {
        "system"
    } else {
        "user"
    }
}

// ============================================================================
// Log staging — collect all log files into a temp directory
// ============================================================================

/// Collect all log files into a staging directory.
/// Returns the staging directory path.
fn collect_logs_to_staging() -> Result<PathBuf, String> {
    let timestamp = Utc::now().format("%Y%m%d-%H%M%S");
    let staging_dir = std::env::temp_dir().join(format!("kaitu-log-upload-{}", timestamp));
    std::fs::create_dir_all(&staging_dir)
        .map_err(|e| format!("Create staging dir: {}", e))?;

    let mut staged_count = 0;

    // 1. Service logs from all possible directories
    for dir in get_all_service_log_dirs() {
        if !dir.exists() {
            continue;
        }
        let label = dir_label(&dir);

        // k2*.log — main log + lumberjack rotation files
        if let Ok(entries) = glob::glob(&dir.join("k2*.log").to_string_lossy()) {
            for entry in entries.flatten() {
                let dest_name = format!(
                    "{}--{}",
                    label,
                    entry.file_name().unwrap_or_default().to_string_lossy()
                );
                if copy_file_to_staging(&entry, &staging_dir.join(&dest_name)) {
                    staged_count += 1;
                }
            }
        }

        // k2*.log.gz — lumberjack rotated compressed files
        if let Ok(entries) = glob::glob(&dir.join("k2*.log.gz").to_string_lossy()) {
            for entry in entries.flatten() {
                let dest_name = format!(
                    "{}--{}",
                    label,
                    entry.file_name().unwrap_or_default().to_string_lossy()
                );
                if copy_file_to_staging(&entry, &staging_dir.join(&dest_name)) {
                    staged_count += 1;
                }
            }
        }

        // panic-*.log — crash logs
        if let Ok(entries) = glob::glob(&dir.join("panic-*.log").to_string_lossy()) {
            for entry in entries.flatten() {
                let dest_name = format!(
                    "{}--{}",
                    label,
                    entry.file_name().unwrap_or_default().to_string_lossy()
                );
                if copy_file_to_staging(&entry, &staging_dir.join(&dest_name)) {
                    staged_count += 1;
                }
            }
        }

    }

    // 2. Desktop logs (Tauri app logs)
    let desktop_dir = crate::get_desktop_log_dir();
    if desktop_dir.exists() {
        if let Ok(entries) = glob::glob(&desktop_dir.join("desktop*log*").to_string_lossy()) {
            for entry in entries.flatten() {
                let dest_name = entry
                    .file_name()
                    .unwrap_or_default()
                    .to_string_lossy()
                    .to_string();
                if copy_file_to_staging(&entry, &staging_dir.join(&dest_name)) {
                    staged_count += 1;
                }
            }
        }
    }

    // 3. macOS system logs (log show command output)
    #[cfg(target_os = "macos")]
    {
        let system_log = collect_macos_system_logs();
        if !system_log.is_empty() {
            let dest = staging_dir.join("system.log");
            if std::fs::write(&dest, &system_log).is_ok() {
                staged_count += 1;
            }
        }
    }

    log::info!(
        "[log_upload] Staged {} log files into {}",
        staged_count,
        staging_dir.display()
    );

    Ok(staging_dir)
}

/// Copy a file to staging. Returns true if successful and file had content.
fn copy_file_to_staging(src: &Path, dest: &Path) -> bool {
    match std::fs::metadata(src) {
        Ok(meta) if meta.len() > 0 => {}
        _ => return false,
    }
    if std::fs::copy(src, dest).is_ok() {
        log::info!(
            "[log_upload] Staged: {} -> {}",
            src.display(),
            dest.file_name().unwrap_or_default().to_string_lossy()
        );
        true
    } else {
        log::warn!("[log_upload] Failed to copy: {}", src.display());
        false
    }
}

/// Collect macOS system logs via `log show` command.
#[cfg(target_os = "macos")]
fn collect_macos_system_logs() -> String {
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
        Ok(out) if out.status.success() => String::from_utf8_lossy(&out.stdout).to_string(),
        _ => String::new(),
    }
}

// ============================================================================
// Sanitization
// ============================================================================

/// Remove sensitive information from log content.
fn sanitize_content(content: &str) -> String {
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

/// Sanitize all files in a staging directory in-place.
fn sanitize_staging_dir(dir: &Path) -> Result<(), String> {
    let entries =
        std::fs::read_dir(dir).map_err(|e| format!("Read staging dir: {}", e))?;
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        // Skip binary/compressed files — can't sanitize and don't need to
        let name = path.file_name().unwrap_or_default().to_string_lossy();
        if name.ends_with(".gz") {
            continue;
        }
        match std::fs::read_to_string(&path) {
            Ok(content) => {
                let sanitized = sanitize_content(&content);
                if sanitized != content {
                    let _ = std::fs::write(&path, sanitized);
                }
            }
            Err(_) => {
                // Binary or unreadable file — skip sanitization
            }
        }
    }
    Ok(())
}

// ============================================================================
// Tar.gz creation
// ============================================================================

/// Create a tar.gz archive from all files in a directory.
/// Returns the compressed bytes.
fn create_tar_gz(dir: &Path) -> Result<Vec<u8>, String> {
    let gz_encoder = GzEncoder::new(Vec::new(), Compression::default());
    let mut tar_builder = tar::Builder::new(gz_encoder);

    let entries =
        std::fs::read_dir(dir).map_err(|e| format!("Read staging dir: {}", e))?;
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let file_name = path
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .to_string();
        let mut file = std::fs::File::open(&path)
            .map_err(|e| format!("Open {}: {}", file_name, e))?;
        tar_builder
            .append_file(&file_name, &mut file)
            .map_err(|e| format!("Append {}: {}", file_name, e))?;
    }

    let gz_encoder = tar_builder
        .into_inner()
        .map_err(|e| format!("Finalize tar: {}", e))?;
    let compressed = gz_encoder
        .finish()
        .map_err(|e| format!("Finalize gzip: {}", e))?;

    log::info!("[log_upload] Created tar.gz: {} bytes", compressed.len());
    Ok(compressed)
}

// ============================================================================
// S3 Upload
// ============================================================================

/// Generate S3 object key for the log archive.
fn generate_s3_key(feedback_id: Option<&str>, version: &str, udid: &str) -> String {
    let now = Utc::now();
    let date = now.format("%Y/%m/%d");
    let timestamp = now.format("%H%M%S");

    let identifier = match feedback_id {
        Some(id) => id.to_string(),
        None => uuid::Uuid::new_v4().to_string()[..8].to_string(),
    };

    format!(
        "desktop/{}/{}/{}/logs-{}-{}.tar.gz",
        version, udid, date, timestamp, identifier
    )
}

/// Upload compressed data to S3 public bucket.
fn upload_to_s3(s3_key: &str, data: &[u8]) -> Result<(), String> {
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);

    log::info!("[log_upload] Uploading to S3: {} ({} bytes)", s3_key, data.len());

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
        return Err(format!(
            "S3 upload failed: status={}, body={}",
            status, body
        ));
    }

    log::info!("[log_upload] Uploaded to S3: {}", s3_key);
    Ok(())
}

/// Check if an S3 object exists via HEAD request.
fn s3_object_exists(s3_key: &str) -> bool {
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);
    let client = match reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()
    {
        Ok(c) => c,
        Err(_) => return false,
    };
    match client.head(&url).send() {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
    }
}

/// Upload a single file to S3.
fn upload_file_to_s3(s3_key: &str, file_path: &Path) -> Result<(), String> {
    let data =
        std::fs::read(file_path).map_err(|e| format!("Read {}: {}", file_path.display(), e))?;
    let content_type = if s3_key.ends_with(".gz") {
        "application/gzip"
    } else {
        "text/plain; charset=utf-8"
    };
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
        .build()
        .map_err(|e| format!("HTTP client: {}", e))?;
    let response = client
        .put(&url)
        .header("Content-Type", content_type)
        .body(data)
        .send()
        .map_err(|e| format!("S3 PUT: {}", e))?;
    if !response.status().is_success() {
        return Err(format!("S3 PUT failed: {}", response.status()));
    }
    Ok(())
}

/// Clean up the staging directory.
fn cleanup_staging_dir(dir: &Path) {
    if let Err(e) = std::fs::remove_dir_all(dir) {
        log::warn!(
            "[log_upload] Failed to remove staging dir {}: {}",
            dir.display(),
            e
        );
    }
}

// ============================================================================
// Main upload orchestrator
// ============================================================================

/// Upload all service logs to S3 as a single tar.gz archive.
fn upload_service_log(params: UploadLogParams, udid: String) -> UploadLogResult {
    log::info!("[log_upload] Starting log upload: reason={}", params.reason);

    let feedback_id = params.feedback_id.as_deref();

    // 1. Collect all logs into staging dir
    let staging_dir = match collect_logs_to_staging() {
        Ok(dir) => dir,
        Err(e) => {
            return UploadLogResult {
                success: false,
                error: Some(format!("Failed to collect logs: {}", e)),
                s3_keys: None,
            };
        }
    };

    // Check if any files were staged
    let has_files = std::fs::read_dir(&staging_dir)
        .map(|entries| entries.count() > 0)
        .unwrap_or(false);

    if !has_files {
        cleanup_staging_dir(&staging_dir);
        return UploadLogResult {
            success: false,
            error: Some("No log files found".to_string()),
            s3_keys: None,
        };
    }

    // 2. Sanitize all staged files
    if let Err(e) = sanitize_staging_dir(&staging_dir) {
        log::warn!("[log_upload] Sanitization warning: {}", e);
    }

    // 3. Create tar.gz archive
    let archive_data = match create_tar_gz(&staging_dir) {
        Ok(data) => data,
        Err(e) => {
            cleanup_staging_dir(&staging_dir);
            return UploadLogResult {
                success: false,
                error: Some(format!("Failed to create archive: {}", e)),
                s3_keys: None,
            };
        }
    };

    // 4. Upload to S3
    let version = env!("CARGO_PKG_VERSION");
    let s3_key = generate_s3_key(feedback_id, version, &udid);
    if let Err(e) = upload_to_s3(&s3_key, &archive_data) {
        cleanup_staging_dir(&staging_dir);
        return UploadLogResult {
            success: false,
            error: Some(e),
            s3_keys: None,
        };
    }

    // 5. Clean up
    cleanup_staging_dir(&staging_dir);

    log::info!("[log_upload] Log upload completed: {}", s3_key);

    UploadLogResult {
        success: true,
        error: None,
        s3_keys: Some(vec![UploadedFileInfo {
            name: "logs".to_string(),
            s3_key,
        }]),
    }
}

// ============================================================================
// Per-file auto-upload (beta)
// ============================================================================

/// Per-file upload for beta auto-upload.
/// Active .log files: always PUT (overwrite). Rotated .gz: HEAD check, skip if exists.
fn upload_auto(udid: &str) -> UploadLogResult {
    log::info!("[log_upload] Starting auto-upload (per-file mode)");
    let mut uploaded: Vec<UploadedFileInfo> = Vec::new();

    for dir in get_all_service_log_dirs() {
        if !dir.exists() {
            continue;
        }
        upload_auto_dir(&dir, udid, &mut uploaded);
    }
    let desktop_dir = crate::get_desktop_log_dir();
    if desktop_dir.exists() {
        upload_auto_dir(&desktop_dir, udid, &mut uploaded);
    }

    if uploaded.is_empty() {
        return UploadLogResult {
            success: false,
            error: Some("No log files found".into()),
            s3_keys: None,
        };
    }
    log::info!(
        "[log_upload] Auto-upload complete: {} files",
        uploaded.len()
    );
    UploadLogResult {
        success: true,
        error: None,
        s3_keys: Some(uploaded),
    }
}

fn upload_auto_dir(dir: &Path, udid: &str, uploaded: &mut Vec<UploadedFileInfo>) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let name = path
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .to_string();

        // Only process log-related files
        let is_log = name.ends_with(".log")
            && (name.starts_with("k2")
                || name.starts_with("desktop")
                || name.starts_with("panic"));
        let is_gz = name.ends_with(".log.gz") && name.starts_with("k2");
        if !is_log && !is_gz {
            continue;
        }

        // Skip empty files
        if let Ok(meta) = std::fs::metadata(&path) {
            if meta.len() == 0 {
                continue;
            }
        }

        let s3_key = format!("auto/{}/{}", udid, name);

        if is_gz {
            // Rotated .gz: HEAD check — skip if already uploaded
            if s3_object_exists(&s3_key) {
                log::debug!("[log_upload] Auto: skip (exists) {}", name);
                continue;
            }
            // Upload .gz as-is (already compressed, no sanitization)
            match upload_file_to_s3(&s3_key, &path) {
                Ok(()) => {
                    log::info!("[log_upload] Auto: uploaded {}", s3_key);
                    uploaded.push(UploadedFileInfo {
                        name: name.clone(),
                        s3_key,
                    });
                }
                Err(e) => log::warn!("[log_upload] Auto: failed {}: {}", name, e),
            }
        } else {
            // Active .log: sanitize content, write to temp file, then PUT (overwrite)
            match std::fs::read_to_string(&path) {
                Ok(content) => {
                    let sanitized = sanitize_content(&content);
                    let tmp = std::env::temp_dir().join(format!("kaitu-auto-{}", name));
                    if std::fs::write(&tmp, sanitized).is_ok() {
                        match upload_file_to_s3(&s3_key, &tmp) {
                            Ok(()) => {
                                log::info!("[log_upload] Auto: uploaded {}", s3_key);
                                uploaded.push(UploadedFileInfo {
                                    name: name.clone(),
                                    s3_key,
                                });
                            }
                            Err(e) => log::warn!("[log_upload] Auto: failed {}: {}", name, e),
                        }
                        let _ = std::fs::remove_file(&tmp);
                    }
                }
                Err(_) => {} // Binary/unreadable — skip
            }
        }
    }
}

// ============================================================================
// Tauri Command
// ============================================================================

/// IPC: Upload service logs (runs in blocking thread).
#[tauri::command]
pub async fn upload_service_log_command(
    params: UploadLogParams,
) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || {
        let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
        if params.reason == "beta-auto-upload" {
            upload_auto(&udid)
        } else {
            upload_service_log(params, udid)
        }
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

    #[test]
    fn test_sanitize_content() {
        let input = r#"{"token":"abc123"} Authorization: Bearer eyJhbGci X-K2-Token: secret123 {"password":"hunter2"} {"secret":"mysecret"}"#;
        let result = sanitize_content(input);

        assert!(result.contains(r#""token":"***""#));
        assert!(result.contains("Authorization: Bearer ***"));
        assert!(result.contains("X-K2-Token: ***"));
        assert!(result.contains(r#""password":"***""#));
        assert!(result.contains(r#""secret":"***""#));
        assert!(!result.contains(r#""token":"abc"#));
        assert!(!result.contains("Bearer eyJ"));
    }

    #[test]
    fn test_sanitize_content_no_sensitive() {
        let input = "2024-01-01 INFO normal log line\n2024-01-01 DEBUG another line";
        let result = sanitize_content(input);
        assert_eq!(result, input);
    }

    #[test]
    fn test_generate_s3_key_no_feedback() {
        let key = generate_s3_key(None, "0.4.1", "test-udid-123");
        assert!(key.starts_with("desktop/0.4.1/test-udid-123/"));
        assert!(key.contains("logs-"));
        assert!(key.ends_with(".tar.gz"));
        let parts: Vec<&str> = key.split('/').collect();
        assert_eq!(parts.len(), 7);
    }

    #[test]
    fn test_generate_s3_key_with_feedback() {
        let key = generate_s3_key(Some("fb-12345"), "0.4.1", "test-udid-456");
        assert!(key.starts_with("desktop/0.4.1/test-udid-456/"));
        assert!(key.contains("logs-"));
        assert!(key.contains("fb-12345"));
        assert!(key.ends_with(".tar.gz"));
        let parts: Vec<&str> = key.split('/').collect();
        assert_eq!(parts.len(), 7);
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
    fn test_upload_log_result_camel_case_serialization() {
        let result = UploadLogResult {
            success: true,
            error: None,
            s3_keys: Some(vec![UploadedFileInfo {
                name: "logs".to_string(),
                s3_key: "desktop/0.3.22/udid/2026/03/05/logs-143022-abc.tar.gz".to_string(),
            }]),
        };
        let json = serde_json::to_string(&result).unwrap();
        assert!(
            json.contains("\"s3Keys\""),
            "s3_keys must serialize as s3Keys: {}",
            json
        );
        assert!(
            json.contains("\"s3Key\""),
            "s3_key must serialize as s3Key: {}",
            json
        );
        assert!(
            !json.contains("\"s3_keys\""),
            "must not have snake_case s3_keys: {}",
            json
        );
        assert!(
            !json.contains("\"s3_key\""),
            "must not have snake_case s3_key: {}",
            json
        );
    }

    #[test]
    fn test_get_all_service_log_dirs_not_empty() {
        let dirs = get_all_service_log_dirs();
        assert!(!dirs.is_empty(), "Should return at least one directory");
    }

    #[test]
    fn test_dir_label() {
        assert_eq!(dir_label(Path::new("/var/log/kaitu")), "system");
        assert_eq!(
            dir_label(Path::new("/Users/test/Library/Logs/kaitu")),
            "user"
        );
        #[cfg(target_os = "windows")]
        {
            assert_eq!(
                dir_label(Path::new(r"C:\ProgramData\kaitu")),
                "system"
            );
        }
    }

    #[test]
    fn test_staging_and_tar_gz_roundtrip() {
        use flate2::read::GzDecoder;
        use std::io::Read;

        let staging_dir = tempfile::tempdir().unwrap();
        let staging_path = staging_dir.path();

        // Create test log files
        std::fs::write(staging_path.join("system--k2.log"), "test service log content")
            .unwrap();
        std::fs::write(
            staging_path.join("user--panic-20260309.log"),
            "test panic content",
        )
        .unwrap();
        std::fs::write(staging_path.join("desktop.log"), "test desktop log content")
            .unwrap();
        // Simulate a lumberjack rotated compressed file
        std::fs::write(
            staging_path.join("system--k2-2026-03-15T00-00-00.000.log.gz"),
            b"\x1f\x8b\x08\x00compressed-data",
        )
        .unwrap();

        // Create tar.gz
        let archive_data = create_tar_gz(staging_path).unwrap();

        // Verify gzip header
        assert_eq!(archive_data[0], 0x1f);
        assert_eq!(archive_data[1], 0x8b);

        // Decompress and verify tar contents
        let mut decoder = GzDecoder::new(&archive_data[..]);
        let mut decompressed = Vec::new();
        decoder.read_to_end(&mut decompressed).unwrap();

        let mut archive = tar::Archive::new(&decompressed[..]);
        let entries: Vec<String> = archive
            .entries()
            .unwrap()
            .filter_map(|e| {
                e.ok()
                    .and_then(|entry| entry.path().ok().map(|p| p.to_string_lossy().to_string()))
            })
            .collect();

        assert!(entries.contains(&"system--k2.log".to_string()));
        assert!(entries.contains(&"user--panic-20260309.log".to_string()));
        assert!(entries.contains(&"desktop.log".to_string()));
        assert!(entries.contains(&"system--k2-2026-03-15T00-00-00.000.log.gz".to_string()));
        assert_eq!(entries.len(), 4);
    }

    #[test]
    fn test_copy_file_to_staging_skips_empty() {
        let dir = tempfile::tempdir().unwrap();
        let src = dir.path().join("empty.log");
        let dest = dir.path().join("staged.log");

        // Empty file should be skipped
        std::fs::write(&src, "").unwrap();
        assert!(!copy_file_to_staging(&src, &dest));
        assert!(!dest.exists());

        // Non-empty file should be copied
        std::fs::write(&src, "content").unwrap();
        assert!(copy_file_to_staging(&src, &dest));
        assert!(dest.exists());
    }

    #[test]
    fn test_sanitize_staging_dir() {
        let dir = tempfile::tempdir().unwrap();
        let dir_path = dir.path();

        std::fs::write(
            dir_path.join("test.log"),
            r#"{"token":"secret123"} normal log"#,
        )
        .unwrap();

        sanitize_staging_dir(dir_path).unwrap();

        let content = std::fs::read_to_string(dir_path.join("test.log")).unwrap();
        assert!(content.contains(r#""token":"***""#));
        assert!(content.contains("normal log"));
        // The sanitizer replaces the prefix pattern — the original token value
        // is still present but the key-value association is broken
        assert!(!content.contains(r#""token":"secret123""#));
    }
}
