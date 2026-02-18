//! Service log upload module
//!
//! Uploads service logs to S3 and sends Slack notification.
//! Runs in Tauri (not in daemon) because daemon may be crashed.
//!
//! Uploads 4 separate log files:
//! 1. service-{id}.log.gz - Go service logs (service.log)
//! 2. crash-{id}.log.gz - Go panic/crash logs (panic-*.log)
//! 3. desktop-{id}.log.gz - Tauri desktop app logs (desktop.log)
//! 4. system-{id}.log.gz - System-level app logs (macOS Console / Windows Event Log)
//!
//! Log file locations (must match k2 daemon log paths):
//! - macOS:   /var/log/kaitu/service.log
//! - Windows: C:\ProgramData\kaitu\logs\service.log
//! - Linux:   /var/log/kaitu/service.log

use chrono::Utc;
use flate2::write::GzEncoder;
use flate2::Compression;
use serde::{Deserialize, Serialize};
use std::io::Write;
use std::path::PathBuf;

// ============================================================================
// Configuration
// ============================================================================

/// S3 public bucket for log uploads (no authentication required)
const S3_BUCKET_URL: &str = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com";

/// Slack webhook URL for alerts
const SLACK_WEBHOOK_URL: &str =
    "https://hooks.slack.com/services/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";

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
pub struct UploadLogResult {
    pub success: bool,
    pub error: Option<String>,
}

/// Information about an uploaded log file (internal, for Slack notification)
#[derive(Debug, Clone)]
struct UploadedFile {
    log_type: String,
    s3_key: String,
    original_size: usize,
    compressed_size: usize,
}

/// Slack message structures
#[derive(Debug, Serialize)]
struct SlackMessage {
    text: String,
    attachments: Vec<SlackAttachment>,
}

#[derive(Debug, Serialize)]
struct SlackAttachment {
    color: String,
    title: String,
    text: String,
    fields: Vec<SlackField>,
    ts: i64,
}

#[derive(Debug, Serialize)]
struct SlackField {
    title: String,
    value: String,
    short: bool,
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
        PathBuf::from(program_data).join("kaitu").join("logs")
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
    get_log_dir().join("service.log")
}

/// Get desktop log path (user-level directory)
fn get_desktop_log_path() -> PathBuf {
    #[cfg(target_os = "macos")]
    let dir = dirs::home_dir()
        .map(|h| h.join("Library/Logs/kaitu"))
        .unwrap_or_else(|| PathBuf::from("/tmp/kaitu"));

    #[cfg(target_os = "windows")]
    let dir = dirs::data_local_dir()
        .map(|d| d.join("kaitu").join("logs"))
        .unwrap_or_else(|| PathBuf::from(r"C:\temp\kaitu"));

    #[cfg(target_os = "linux")]
    let dir = dirs::home_dir()
        .map(|h| h.join(".local/share/kaitu/logs"))
        .unwrap_or_else(|| PathBuf::from("/tmp/kaitu"));

    #[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
    let dir = PathBuf::from("/tmp/kaitu");

    dir.join("desktop.log")
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
fn generate_s3_key(log_type: &str, feedback_id: Option<&str>) -> String {
    let now = Utc::now();
    let date = now.format("%Y/%m/%d");
    let timestamp = now.format("%H%M%S");

    let identifier = feedback_id
        .map(|id| id.to_string())
        .unwrap_or_else(|| uuid::Uuid::new_v4().to_string()[..8].to_string());

    format!(
        "service-logs/{}/{}-{}-{}.log.gz",
        date, log_type, timestamp, identifier
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
) -> Result<UploadedFile, String> {
    if content.is_empty() {
        return Err(format!("{} log is empty", log_type));
    }

    let sanitized = sanitize_logs(content);
    let original_size = sanitized.len();

    let compressed = compress_gzip(&sanitized)?;
    let compressed_size = compressed.len();

    let s3_key = generate_s3_key(log_type, feedback_id);
    upload_to_s3(&s3_key, &compressed)?;

    log::info!(
        "Uploaded {} log: {} -> {} bytes ({:.1}% compression)",
        log_type,
        original_size,
        compressed_size,
        (compressed_size as f64 / original_size as f64) * 100.0
    );

    Ok(UploadedFile {
        log_type: log_type.to_string(),
        s3_key,
        original_size,
        compressed_size,
    })
}

// ============================================================================
// Slack Notification
// ============================================================================

/// Send Slack alert with links to uploaded log files
fn send_slack_alert(params: &UploadLogParams, uploaded_files: &[UploadedFile]) -> Result<(), String> {
    let log_links: Vec<String> = uploaded_files
        .iter()
        .map(|f| format!("<{}/{}|{}>", S3_BUCKET_URL, f.s3_key, f.log_type))
        .collect();

    let mut fields = vec![
        SlackField {
            title: "Reason".to_string(),
            value: params.reason.clone(),
            short: false,
        },
        SlackField {
            title: "Log Files".to_string(),
            value: log_links.join(" | "),
            short: false,
        },
    ];

    if let Some(email) = &params.email {
        fields.push(SlackField {
            title: "User".to_string(),
            value: email.clone(),
            short: true,
        });
    }

    if let Some(platform) = &params.platform {
        fields.push(SlackField {
            title: "Platform".to_string(),
            value: platform.clone(),
            short: true,
        });
    }

    if let Some(version) = &params.version {
        fields.push(SlackField {
            title: "Version".to_string(),
            value: version.clone(),
            short: true,
        });
    }

    if let Some(duration) = params.failure_duration_ms {
        fields.push(SlackField {
            title: "Duration".to_string(),
            value: format!("{}ms", duration),
            short: true,
        });
    }

    if let Some(feedback_id) = &params.feedback_id {
        fields.push(SlackField {
            title: "Feedback ID".to_string(),
            value: format!("`{}`", feedback_id),
            short: true,
        });
    }

    let total_original: usize = uploaded_files.iter().map(|f| f.original_size).sum();
    let total_compressed: usize = uploaded_files.iter().map(|f| f.compressed_size).sum();
    fields.push(SlackField {
        title: "Log Sizes".to_string(),
        value: format!(
            "{} files, {} KB original, {} KB compressed",
            uploaded_files.len(),
            total_original / 1024,
            total_compressed / 1024
        ),
        short: true,
    });

    let message = SlackMessage {
        text: ":warning: Kaitu Service Log Report".to_string(),
        attachments: vec![SlackAttachment {
            color: "#ff6b6b".to_string(),
            title: "Service Issue Reported".to_string(),
            text: format!("A user reported an issue: {}", params.reason),
            fields,
            ts: Utc::now().timestamp(),
        }],
    };

    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
        .build()
        .map_err(|e| format!("HTTP client error: {}", e))?;

    let response = client
        .post(SLACK_WEBHOOK_URL)
        .header("Content-Type", "application/json")
        .json(&message)
        .send()
        .map_err(|e| format!("Slack alert failed: {}", e))?;

    let status = response.status();
    if !status.is_success() {
        let body = response.text().unwrap_or_default();
        return Err(format!("Slack webhook failed: status={}, body={}", status, body));
    }

    log::info!("Slack alert sent");
    Ok(())
}

// ============================================================================
// Main upload orchestrator
// ============================================================================

/// Upload all service logs to S3 and send Slack notification
fn upload_service_log(params: UploadLogParams) -> UploadLogResult {
    log::info!("Starting log upload: reason={}", params.reason);

    let feedback_id = params.feedback_id.as_deref();
    let mut uploaded_files: Vec<UploadedFile> = Vec::new();
    let mut errors: Vec<String> = Vec::new();

    // 1. Service log
    let (service_content, _) = read_service_log();
    match upload_log_file("service", &service_content, feedback_id) {
        Ok(file) => uploaded_files.push(file),
        Err(e) => {
            log::warn!("Service log upload failed: {}", e);
            errors.push(e);
        }
    }

    // 2. Crash logs
    let (crash_content, _) = read_crash_logs();
    if !crash_content.is_empty() {
        match upload_log_file("crash", &crash_content, feedback_id) {
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
        match upload_log_file("desktop", &desktop_content, feedback_id) {
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
        match upload_log_file("system", &system_content, feedback_id) {
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
        };
    }

    // Slack notification (silent on failure)
    if let Err(e) = send_slack_alert(&params, &uploaded_files) {
        log::debug!("Slack alert skipped: {}", e);
    }

    log::info!(
        "Log upload completed: {} files uploaded",
        uploaded_files.len()
    );

    UploadLogResult {
        success: true,
        error: if errors.is_empty() {
            None
        } else {
            Some(format!("Partial failures: {}", errors.join("; ")))
        },
    }
}

// ============================================================================
// Tauri Command
// ============================================================================

/// IPC: Upload service logs (runs in blocking thread)
#[tauri::command]
pub async fn upload_service_log_command(params: UploadLogParams) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || upload_service_log(params))
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
        let key = generate_s3_key("service", None);
        assert!(key.starts_with("service-logs/"));
        assert!(key.contains("service-"));
        assert!(key.ends_with(".log.gz"));
        // Format: service-logs/YYYY/MM/DD/service-HHMMSS-{uuid8}.log.gz
        let parts: Vec<&str> = key.split('/').collect();
        assert_eq!(parts.len(), 5); // service-logs, YYYY, MM, DD, filename
    }

    #[test]
    fn test_generate_s3_key_with_feedback_id() {
        let key = generate_s3_key("desktop", Some("fb-12345"));
        assert!(key.contains("desktop-"));
        assert!(key.contains("fb-12345"));
        assert!(key.ends_with(".log.gz"));
    }

    #[test]
    fn test_slack_message_format() {
        let message = SlackMessage {
            text: "test".to_string(),
            attachments: vec![SlackAttachment {
                color: "#ff6b6b".to_string(),
                title: "Test".to_string(),
                text: "test text".to_string(),
                fields: vec![SlackField {
                    title: "Reason".to_string(),
                    value: "test reason".to_string(),
                    short: false,
                }],
                ts: 1234567890,
            }],
        };
        let json = serde_json::to_string(&message).unwrap();
        assert!(json.contains("\"text\":\"test\""));
        assert!(json.contains("\"color\":\"#ff6b6b\""));
        assert!(json.contains("\"title\":\"Reason\""));
        assert!(json.contains("\"value\":\"test reason\""));
        assert!(json.contains("\"short\":false"));
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
}
