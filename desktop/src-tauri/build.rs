fn main() {
    let mcp_cap_path = std::path::Path::new("capabilities/mcp-bridge.json");

    #[cfg(all(feature = "mcp-bridge", debug_assertions))]
    {
        std::fs::create_dir_all("capabilities").expect("failed to create capabilities dir");
        let cap = r#"{
  "identifier": "mcp-bridge",
  "description": "enables MCP bridge for development",
  "windows": ["main"],
  "permissions": [
    "core:default",
    "mcp-bridge:default",
    "updater:default",
    "process:default",
    "autostart:default"
  ]
}"#;
        // Only write if content changed — avoids Tauri dev watcher rebuild loop
        let needs_write = match std::fs::read_to_string(mcp_cap_path) {
            Ok(existing) => existing != cap,
            Err(_) => true,
        };
        if needs_write {
            std::fs::write(mcp_cap_path, cap).expect("failed to write mcp-bridge capability");
        }
    }

    #[cfg(not(all(feature = "mcp-bridge", debug_assertions)))]
    {
        let _ = std::fs::remove_file(mcp_cap_path);
    }

    tauri_build::build()
}
