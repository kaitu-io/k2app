fn main() {
    // On macOS: link the Swift NE helper static library and required system frameworks.
    // The library is built from desktop/src-tauri/ne_helper/ and placed in ne_helper/build/.
    // In CI the library is pre-built and placed there by the build script.
    //
    // Skip linking when:
    //   a) Not macOS
    //   b) NE_HELPER_SKIP_LINK=1 is set (used during `cargo test` without the real lib)
    //   c) The library file is absent (development without Swift build)
    #[cfg(target_os = "macos")]
    {
        let skip = std::env::var("NE_HELPER_SKIP_LINK").as_deref() == Ok("1");

        // Directory that contains libk2_ne_helper.a
        // Default: ne_helper/build/ relative to this build.rs location (src-tauri/).
        // Override with NE_HELPER_LIB_DIR env var if the lib is elsewhere.
        let ne_helper_lib_dir = std::env::var("NE_HELPER_LIB_DIR")
            .unwrap_or_else(|_| "ne_helper".to_string());

        let lib_path = std::path::Path::new(&ne_helper_lib_dir).join("libk2_ne_helper.a");
        let lib_exists = lib_path.exists();

        if !skip && lib_exists {
            println!("cargo:rustc-link-search=native={}", ne_helper_lib_dir);
            println!("cargo:rustc-link-lib=static=k2_ne_helper");

            // System frameworks required by the NE helper
            println!("cargo:rustc-link-lib=framework=NetworkExtension");
            println!("cargo:rustc-link-lib=framework=SystemExtensions");
            println!("cargo:rustc-link-lib=framework=Foundation");

            // Swift runtime libraries path — required for Swift compatibility shims
            // referenced by the static library (swiftCompatibility56, etc.)
            if let Ok(output) = std::process::Command::new("xcode-select")
                .arg("-p")
                .output()
            {
                let dev_dir = String::from_utf8_lossy(&output.stdout).trim().to_string();
                let swift_lib = format!(
                    "{}/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/macosx",
                    dev_dir
                );
                if std::path::Path::new(&swift_lib).exists() {
                    println!("cargo:rustc-link-search=native={}", swift_lib);
                }
            }
        } else if !skip && !lib_exists {
            // Emit a warning but do not fail — unit tests use mock stubs
            println!(
                "cargo:warning=libk2_ne_helper.a not found at {}; NE FFI will use test stubs only",
                lib_path.display()
            );
        }

        // Rebuild if the helper lib changes
        println!("cargo:rerun-if-changed={}/libk2_ne_helper.a", ne_helper_lib_dir);
        println!("cargo:rerun-if-env-changed=NE_HELPER_LIB_DIR");
        println!("cargo:rerun-if-env-changed=NE_HELPER_SKIP_LINK");
    }

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
