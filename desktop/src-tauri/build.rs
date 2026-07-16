fn main() {
    // Brand at compile time: K2_BRAND=overleap → cfg(brand_overleap).
    // rerun-if-env-changed is load-bearing — without it, switching BRAND
    // between builds reuses the stale-brand object files (串包二进制).
    println!("cargo:rerun-if-env-changed=K2_BRAND");
    println!("cargo::rustc-check-cfg=cfg(brand_overleap)");
    if std::env::var("K2_BRAND").as_deref() == Ok("overleap") {
        println!("cargo:rustc-cfg=brand_overleap");
    }

    let mcp_cap_path = std::path::Path::new("capabilities/mcp-bridge.json");

    #[cfg(feature = "mcp-bridge")]
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

    #[cfg(not(feature = "mcp-bridge"))]
    {
        let _ = std::fs::remove_file(mcp_cap_path);
    }

    tauri_build::build()
}
