//! SSE client for daemon's GET /api/events endpoint.
//!
//! Daemon mode only (not compiled in ne-mode).
//! Maintains a persistent SSE connection and emits Tauri events:
//! - `service-state-changed { available: bool }` — SSE connection state
//! - `vpn-status-changed { ...engine.Status }` — VPN status from SSE events

use tauri::{AppHandle, Emitter};

/// Start the SSE status stream listener.
/// Runs indefinitely in a background tokio task.
pub fn start(app_handle: AppHandle) {
    let port = std::env::var("K2_DAEMON_PORT")
        .ok()
        .and_then(|v| v.parse::<u16>().ok())
        .unwrap_or(1777);
    let url = format!("http://127.0.0.1:{}/api/events", port);

    tauri::async_runtime::spawn(async move {
        loop {
            log::info!("[sse] Connecting to {}", url);
            match connect_and_stream(&app_handle, &url).await {
                Ok(()) => log::info!("[sse] Stream ended normally"),
                Err(e) => log::warn!("[sse] Stream error: {}", e),
            }
            emit_service_state(&app_handle, false);
            log::info!("[sse] Reconnecting in 3s...");
            tokio::time::sleep(std::time::Duration::from_secs(3)).await;
        }
    });
}

fn emit_service_state(app_handle: &AppHandle, available: bool) {
    let _ = app_handle.emit(
        "service-state-changed",
        serde_json::json!({ "available": available }),
    );
}

fn emit_vpn_status(app_handle: &AppHandle, status_json: &serde_json::Value) {
    let _ = app_handle.emit("vpn-status-changed", status_json);
}

async fn connect_and_stream(app_handle: &AppHandle, url: &str) -> Result<(), String> {
    let client = reqwest::Client::builder()
        .no_proxy()
        .build()
        .map_err(|e| format!("HTTP client error: {}", e))?;

    let response = client
        .get(url)
        .header("Accept", "text/event-stream")
        .send()
        .await
        .map_err(|e| format!("SSE connect failed: {}", e))?;

    if !response.status().is_success() {
        return Err(format!("SSE HTTP {}", response.status()));
    }

    // Connected — service available
    emit_service_state(app_handle, true);
    log::info!("[sse] Connected, streaming events");

    // Parse SSE stream
    use futures_util::StreamExt;
    let mut stream = response.bytes_stream();
    let mut buf = String::new();
    let mut event_type = String::new();
    let mut data_buf = String::new();

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e: reqwest::Error| format!("SSE read error: {}", e))?;
        buf.push_str(&String::from_utf8_lossy(&chunk));

        // Process complete lines (terminated by \n)
        while let Some(pos) = buf.find('\n') {
            let line = buf[..pos].trim_end_matches('\r').to_string();
            buf = buf[pos + 1..].to_string();

            if line.is_empty() {
                // Empty line = end of event
                if !data_buf.is_empty() {
                    if event_type == "status" || event_type.is_empty() {
                        if let Ok(parsed) =
                            serde_json::from_str::<serde_json::Value>(&data_buf)
                        {
                            emit_vpn_status(app_handle, &parsed);
                        }
                    }
                    event_type.clear();
                    data_buf.clear();
                }
            } else if let Some(rest) = line.strip_prefix("event:") {
                event_type = rest.trim().to_string();
            } else if let Some(rest) = line.strip_prefix("data:") {
                if !data_buf.is_empty() {
                    data_buf.push('\n');
                }
                data_buf.push_str(rest.trim());
            }
            // Lines starting with ':' are comments (heartbeat) — ignore
        }
    }

    Ok(())
}
