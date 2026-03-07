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

/// Parsed SSE event: event type + JSON data
#[derive(Debug, Clone, PartialEq)]
struct SseEvent {
    event_type: String,
    data: String,
}

/// Incremental SSE line parser.
/// Buffers raw bytes and yields complete events.
struct SseParser {
    buf: String,
    event_type: String,
    data_buf: String,
}

impl SseParser {
    fn new() -> Self {
        Self {
            buf: String::new(),
            event_type: String::new(),
            data_buf: String::new(),
        }
    }

    /// Feed raw bytes into the parser, returns any complete events.
    fn feed(&mut self, chunk: &str) -> Vec<SseEvent> {
        self.buf.push_str(chunk);
        let mut events = Vec::new();

        while let Some(pos) = self.buf.find('\n') {
            let line = self.buf[..pos].trim_end_matches('\r').to_string();
            self.buf = self.buf[pos + 1..].to_string();

            if line.is_empty() {
                // Empty line = end of event
                if !self.data_buf.is_empty() {
                    events.push(SseEvent {
                        event_type: self.event_type.clone(),
                        data: self.data_buf.clone(),
                    });
                    self.event_type.clear();
                    self.data_buf.clear();
                }
            } else if let Some(rest) = line.strip_prefix("event:") {
                self.event_type = rest.trim().to_string();
            } else if let Some(rest) = line.strip_prefix("data:") {
                if !self.data_buf.is_empty() {
                    self.data_buf.push('\n');
                }
                self.data_buf.push_str(rest.trim());
            }
            // Lines starting with ':' are comments (heartbeat) — ignore
        }

        events
    }
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

    use futures_util::StreamExt;
    let mut stream = response.bytes_stream();
    let mut parser = SseParser::new();

    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|e: reqwest::Error| format!("SSE read error: {}", e))?;
        let events = parser.feed(&String::from_utf8_lossy(&chunk));

        for event in events {
            if event.event_type == "status" || event.event_type.is_empty() {
                if let Ok(parsed) = serde_json::from_str::<serde_json::Value>(&event.data) {
                    emit_vpn_status(app_handle, &parsed);
                }
            }
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_status_event() {
        let mut parser = SseParser::new();
        let events = parser.feed("event: status\ndata: {\"state\":\"connected\"}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, "status");
        assert_eq!(events[0].data, "{\"state\":\"connected\"}");
    }

    #[test]
    fn test_parse_event_without_type() {
        let mut parser = SseParser::new();
        let events = parser.feed("data: {\"state\":\"stopped\"}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, "");
        assert_eq!(events[0].data, "{\"state\":\"stopped\"}");
    }

    #[test]
    fn test_parse_multiline_data() {
        let mut parser = SseParser::new();
        let events = parser.feed("data: line1\ndata: line2\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].data, "line1\nline2");
    }

    #[test]
    fn test_parse_comment_ignored() {
        let mut parser = SseParser::new();
        let events = parser.feed(": heartbeat\ndata: {\"ok\":true}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].data, "{\"ok\":true}");
    }

    #[test]
    fn test_parse_multiple_events() {
        let mut parser = SseParser::new();
        let events = parser.feed(
            "event: status\ndata: {\"state\":\"connecting\"}\n\nevent: status\ndata: {\"state\":\"connected\"}\n\n"
        );
        assert_eq!(events.len(), 2);
        assert_eq!(events[0].data, "{\"state\":\"connecting\"}");
        assert_eq!(events[1].data, "{\"state\":\"connected\"}");
    }

    #[test]
    fn test_parse_chunked_delivery() {
        let mut parser = SseParser::new();

        // First chunk: partial
        let events = parser.feed("event: status\nda");
        assert_eq!(events.len(), 0);

        // Second chunk: completes the event
        let events = parser.feed("ta: {\"state\":\"connected\"}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].data, "{\"state\":\"connected\"}");
    }

    #[test]
    fn test_parse_crlf_line_endings() {
        let mut parser = SseParser::new();
        let events = parser.feed("event: status\r\ndata: {\"ok\":true}\r\n\r\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, "status");
        assert_eq!(events[0].data, "{\"ok\":true}");
    }

    #[test]
    fn test_parse_empty_data_no_event() {
        let mut parser = SseParser::new();
        // Empty line without any data → no event emitted
        let events = parser.feed("\n");
        assert_eq!(events.len(), 0);
    }

    #[test]
    fn test_parse_non_status_event_type() {
        let mut parser = SseParser::new();
        let events = parser.feed("event: ping\ndata: {}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].event_type, "ping");
    }

    #[test]
    fn test_regression_reconnect_emits_status() {
        // Regression: after reconnect, status events must still parse correctly
        let mut parser = SseParser::new();

        // Simulate: connect → status → disconnect → reconnect → status
        let events = parser.feed("event: status\ndata: {\"state\":\"connected\"}\n\n");
        assert_eq!(events.len(), 1);

        // New parser (simulating reconnect — fresh parser per connection)
        let mut parser2 = SseParser::new();
        let events = parser2.feed("event: status\ndata: {\"state\":\"connected\"}\n\n");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].data, "{\"state\":\"connected\"}");
    }
}
