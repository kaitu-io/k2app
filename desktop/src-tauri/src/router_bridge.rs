//! LAN router bridge: default-gateway lookup + native HTTP to the k2r panel.
//! Native HTTP exists solely to reach a LAN router — the private-host guard
//! is a hard invariant, not a convenience.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RouterRequestOptions {
    pub url: String,
    #[serde(default)]
    pub method: Option<String>,
    #[serde(default)]
    pub headers: Option<HashMap<String, String>>,
    #[serde(default)]
    pub body: Option<String>,
    #[serde(default)]
    pub timeout_ms: Option<u64>,
}

#[derive(Serialize)]
pub struct RouterResponse {
    pub status: u16,
    pub body: String,
}

fn is_private_host(u: &url::Url) -> bool {
    match u.host() {
        Some(url::Host::Ipv4(ip)) => ip.is_private() || ip.is_loopback(),
        _ => false,
    }
}

#[tauri::command]
pub async fn router_http_request(opts: RouterRequestOptions) -> Result<RouterResponse, String> {
    let parsed = url::Url::parse(&opts.url).map_err(|e| e.to_string())?;
    if parsed.scheme() != "http" || !is_private_host(&parsed) {
        return Err("router_http_request: only http:// to private IPv4".into());
    }
    // reqwest::blocking panics inside an async runtime — always spawn_blocking.
    tokio::task::spawn_blocking(move || {
        let client = reqwest::blocking::Client::builder()
            .timeout(std::time::Duration::from_millis(opts.timeout_ms.unwrap_or(5000)))
            .build()
            .map_err(|e| e.to_string())?;
        let method = reqwest::Method::from_bytes(
            opts.method.as_deref().unwrap_or("GET").as_bytes(),
        )
        .map_err(|e| e.to_string())?;
        let mut req = client.request(method, &opts.url);
        if let Some(h) = &opts.headers {
            for (k, v) in h {
                req = req.header(k, v);
            }
        }
        if let Some(b) = opts.body {
            req = req.body(b);
        }
        let resp = req.send().map_err(|e| e.to_string())?;
        let status = resp.status().as_u16();
        let body = resp.text().map_err(|e| e.to_string())?;
        Ok(RouterResponse { status, body })
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn get_default_gateway() -> Result<Option<String>, String> {
    tokio::task::spawn_blocking(default_gateway_impl)
        .await
        .map_err(|e| e.to_string())?
}

#[cfg(target_os = "macos")]
fn default_gateway_impl() -> Result<Option<String>, String> {
    let out = std::process::Command::new("route")
        .args(["-n", "get", "default"])
        .output()
        .map_err(|e| e.to_string())?;
    Ok(parse_route_get(&String::from_utf8_lossy(&out.stdout)))
}

#[cfg(target_os = "macos")]
fn parse_route_get(s: &str) -> Option<String> {
    let mut gateway = None;
    let mut iface = None;
    for line in s.lines() {
        let line = line.trim();
        if let Some(v) = line.strip_prefix("gateway:") {
            gateway = Some(v.trim().to_string());
        }
        if let Some(v) = line.strip_prefix("interface:") {
            iface = Some(v.trim().to_string());
        }
    }
    match (gateway, iface) {
        (Some(g), Some(i))
            if !i.starts_with("utun") && g.parse::<std::net::Ipv4Addr>().is_ok() =>
        {
            Some(g)
        }
        _ => None,
    }
}

#[cfg(target_os = "windows")]
fn default_gateway_impl() -> Result<Option<String>, String> {
    use std::os::windows::process::CommandExt;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    let ps = r#"Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
        Where-Object { $_.NextHop -ne '0.0.0.0' } |
        Sort-Object RouteMetric |
        ForEach-Object { "$($_.NextHop)|$((Get-NetAdapter -InterfaceIndex $_.ifIndex -ErrorAction SilentlyContinue).InterfaceDescription)" }"#;
    let out = std::process::Command::new("powershell")
        .args(["-NoProfile", "-Command", ps])
        .creation_flags(CREATE_NO_WINDOW)
        .output()
        .map_err(|e| e.to_string())?;
    Ok(parse_netroute(&String::from_utf8_lossy(&out.stdout)))
}

#[cfg(target_os = "windows")]
fn parse_netroute(s: &str) -> Option<String> {
    for line in s.lines() {
        let line = line.trim();
        let Some((hop, desc)) = line.split_once('|') else {
            continue;
        };
        let d = desc.to_ascii_lowercase();
        if d.contains("wintun") || d.contains("tap") || d.contains("kaitu") || d.contains("tunnel") {
            continue;
        }
        if hop.parse::<std::net::Ipv4Addr>().is_ok() {
            return Some(hop.to_string());
        }
    }
    None
}

#[cfg(not(any(target_os = "macos", target_os = "windows")))]
fn default_gateway_impl() -> Result<Option<String>, String> {
    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(target_os = "macos")]
    #[test]
    fn parse_route_get_extracts_gateway() {
        let fixture = "   route to: default\ndestination: default\n       mask: default\n    gateway: 192.168.1.1\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>\n";
        assert_eq!(parse_route_get(fixture), Some("192.168.1.1".to_string()));
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn parse_route_get_skips_utun() {
        let fixture = "    gateway: 198.18.0.1\n  interface: utun4\n";
        assert_eq!(parse_route_get(fixture), None);
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn parse_netroute_skips_tun_adapters() {
        let fixture = "198.18.0.1|Wintun Userspace Tunnel\n192.168.1.1|Intel(R) Wi-Fi 6 AX201\n";
        assert_eq!(parse_netroute(fixture), Some("192.168.1.1".to_string()));
    }

    #[test]
    fn private_host_guard() {
        assert!(is_private_host(&url::Url::parse("http://192.168.1.1:1779/ping").unwrap()));
        assert!(!is_private_host(&url::Url::parse("http://8.8.8.8/").unwrap()));
        assert!(!is_private_host(&url::Url::parse("http://example.com/").unwrap()));
    }
}
