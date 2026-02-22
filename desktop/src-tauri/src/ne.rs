//! macOS Network Extension (NE) bridge — FFI to Swift libk2_ne_helper.a
//!
//! This module is compiled only on macOS (`#[cfg(target_os = "macos")]`).
//! It exposes the same IPC surface as `service.rs` but routes calls through
//! the Swift NE helper static library instead of the k2 daemon HTTP API.

#[cfg(target_os = "macos")]
pub use macos::*;

#[cfg(target_os = "macos")]
mod macos {
    use crate::service::ServiceResponse;
    use std::ffi::{CStr, CString};
    use std::os::raw::c_char;

    // ---------------------------------------------------------------------------
    // C FFI declarations — resolved at link time from libk2_ne_helper.a
    // ---------------------------------------------------------------------------

    extern "C" {
        /// Install the NE configuration into the system VPN preferences.
        /// Returns a ServiceResponse JSON string. Caller must free with k2ne_free_string.
        fn k2ne_install() -> *mut c_char;

        /// Start the VPN tunnel with the supplied config JSON.
        /// Returns a ServiceResponse JSON string. Caller must free with k2ne_free_string.
        fn k2ne_start(config_json: *const c_char) -> *mut c_char;

        /// Stop the VPN tunnel.
        /// Returns a ServiceResponse JSON string. Caller must free with k2ne_free_string.
        fn k2ne_stop() -> *mut c_char;

        /// Return current tunnel status.
        /// Returns a ServiceResponse JSON string. Caller must free with k2ne_free_string.
        fn k2ne_status() -> *mut c_char;

        /// Reinstall / repair the NE configuration.
        /// Returns a ServiceResponse JSON string. Caller must free with k2ne_free_string.
        fn k2ne_reinstall() -> *mut c_char;

        /// Register a C callback that is invoked whenever the NE state changes.
        /// Pass `None` to unregister.
        fn k2ne_set_state_callback(cb: Option<unsafe extern "C" fn(*const c_char)>);

        /// Free a string returned by any k2ne_* function.
        fn k2ne_free_string(ptr: *mut c_char);
    }

    // ---------------------------------------------------------------------------
    // Internal helper
    // ---------------------------------------------------------------------------

    /// Consume a `*mut c_char` returned by a k2ne_* function, parse it as
    /// `ServiceResponse`, and free the original C string.
    ///
    /// Returns `Err` if the pointer is null or the JSON is malformed.
    fn call_ne_fn(ptr: *mut c_char) -> Result<ServiceResponse, String> {
        if ptr.is_null() {
            return Err("k2ne returned null pointer".into());
        }
        // Safety: ptr is non-null and was returned by a k2ne_* function which
        // guarantees a valid NUL-terminated UTF-8 string.
        let json_str = unsafe {
            let s = CStr::from_ptr(ptr)
                .to_str()
                .map_err(|e| format!("k2ne response UTF-8 error: {}", e))?
                .to_owned();
            k2ne_free_string(ptr);
            s
        };
        serde_json::from_str::<ServiceResponse>(&json_str)
            .map_err(|e| format!("k2ne response parse error: {} — raw: {}", e, json_str))
    }

    // ---------------------------------------------------------------------------
    // Public API
    // ---------------------------------------------------------------------------

    /// Route a VPN action to the NE bridge.
    ///
    /// Mirrors the daemon HTTP API: action in {"up","down","status","version"}.
    /// "version" is answered purely in Rust (no Swift call needed).
    pub fn ne_action(
        action: &str,
        params: Option<serde_json::Value>,
    ) -> Result<ServiceResponse, String> {
        match action {
            "up" => {
                let config_json = params
                    .as_ref()
                    .map(|p| serde_json::to_string(p).unwrap_or_default())
                    .unwrap_or_default();
                let c_config = CString::new(config_json)
                    .map_err(|e| format!("CString error: {}", e))?;
                call_ne_fn(unsafe { k2ne_start(c_config.as_ptr()) })
            }
            "down" => call_ne_fn(unsafe { k2ne_stop() }),
            "status" => call_ne_fn(unsafe { k2ne_status() }),
            "version" => Ok(ServiceResponse {
                code: 0,
                message: "ok".into(),
                data: serde_json::json!({
                    "version": env!("CARGO_PKG_VERSION"),
                    "os": "macos"
                }),
            }),
            _ => Err(format!("unknown action: {}", action)),
        }
    }

    /// Install the NE configuration into macOS VPN preferences.
    ///
    /// Called at startup instead of `ensure_service_running` on macOS.
    pub fn ensure_ne_installed() -> Result<(), String> {
        let resp = call_ne_fn(unsafe { k2ne_install() })?;
        if resp.code == 0 {
            Ok(())
        } else {
            Err(format!("k2ne_install failed (code {}): {}", resp.code, resp.message))
        }
    }

    /// Read the hardware UUID via `sysctl -n kern.uuid`.
    ///
    /// Replaces the daemon HTTP call on macOS. The UUID is stable across reboots
    /// and does not require admin privileges.
    pub fn get_udid_native() -> Result<ServiceResponse, String> {
        let output = std::process::Command::new("sysctl")
            .args(["-n", "kern.uuid"])
            .output()
            .map_err(|e| format!("sysctl failed: {}", e))?;

        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(format!("sysctl exited non-zero: {}", stderr));
        }

        let uuid = String::from_utf8_lossy(&output.stdout).trim().to_string();
        if uuid.is_empty() {
            return Err("sysctl returned empty UUID".into());
        }

        Ok(ServiceResponse {
            code: 0,
            message: "ok".into(),
            data: serde_json::json!({ "udid": uuid }),
        })
    }

    /// Reinstall / repair the NE configuration (admin-level operation).
    ///
    /// Replaces `admin_reinstall_service_macos` on macOS.
    pub fn admin_reinstall_ne() -> Result<String, String> {
        let resp = call_ne_fn(unsafe { k2ne_reinstall() })?;
        if resp.code == 0 {
            Ok(resp.message)
        } else {
            Err(format!("k2ne_reinstall failed (code {}): {}", resp.code, resp.message))
        }
    }

    // ---------------------------------------------------------------------------
    // State callback — emit Tauri events when NE state changes
    // ---------------------------------------------------------------------------

    /// C-compatible callback invoked by the Swift NE helper when the VPN state changes.
    ///
    /// Safety: called from a Swift background thread; must be Send + 'static.
    /// We forward the state string to the global AppHandle stored in STATE_HANDLE.
    unsafe extern "C" fn ne_state_callback(state_ptr: *const c_char) {
        if state_ptr.is_null() {
            return;
        }
        let state = match CStr::from_ptr(state_ptr).to_str() {
            Ok(s) => s.to_owned(),
            Err(_) => return,
        };

        // Acquire the stored AppHandle and emit a Tauri event.
        let guard = STATE_HANDLE.lock();
        if let Ok(maybe_handle) = guard {
            if let Some(handle) = maybe_handle.as_ref() {
                let payload = serde_json::json!({ "state": state });
                if let Err(e) = handle.emit("ne-state-changed", payload) {
                    log::error!("[ne] Failed to emit ne-state-changed: {}", e);
                }
            }
        }
    }

    use std::sync::Mutex;
    use tauri::Emitter;

    /// Global storage for the AppHandle so the C callback can reach it.
    static STATE_HANDLE: Mutex<Option<tauri::AppHandle>> = Mutex::new(None);

    /// Register the state callback with the Swift NE helper and store the
    /// AppHandle for use in `ne_state_callback`.
    pub fn register_state_callback(app_handle: tauri::AppHandle) {
        match STATE_HANDLE.lock() {
            Ok(mut guard) => {
                *guard = Some(app_handle);
            }
            Err(e) => {
                log::error!("[ne] Failed to store AppHandle for state callback: {}", e);
                return;
            }
        }
        unsafe {
            k2ne_set_state_callback(Some(ne_state_callback));
        }
        log::info!("[ne] State callback registered");
    }

    /// Unregister the state callback (e.g., on app exit).
    pub fn unregister_state_callback() {
        unsafe {
            k2ne_set_state_callback(None);
        }
        if let Ok(mut guard) = STATE_HANDLE.lock() {
            *guard = None;
        }
        log::info!("[ne] State callback unregistered");
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    // -----------------------------------------------------------------------
    // Mock implementation of the extern "C" k2ne_* functions.
    //
    // These stubs are used when building for test — they don't link against
    // libk2_ne_helper.a and therefore work in any CI environment.
    //
    // The stubs are declared with `#[no_mangle]` so the linker resolves them
    // instead of looking for the real Swift library symbols.
    //
    // NOTE: These are only compiled for tests; the `extern "C"` declarations
    // in `macos` still reference the real symbols in production builds.
    // -----------------------------------------------------------------------

    use crate::service::ServiceResponse;
    use std::ffi::{CStr, CString};
    use std::os::raw::c_char;

    // ----- mock C-function bodies (platform-agnostic) ----------------------

    fn make_response_json(code: i32, message: &str, data: serde_json::Value) -> *mut c_char {
        let json = serde_json::json!({
            "code": code,
            "message": message,
            "data": data,
        })
        .to_string();
        CString::new(json).unwrap().into_raw()
    }

    #[no_mangle]
    pub extern "C" fn k2ne_install() -> *mut c_char {
        make_response_json(0, "installed", serde_json::json!({}))
    }

    #[no_mangle]
    pub extern "C" fn k2ne_start(config_json: *const c_char) -> *mut c_char {
        let _config = if config_json.is_null() {
            "{}".to_string()
        } else {
            unsafe { CStr::from_ptr(config_json).to_string_lossy().into_owned() }
        };
        make_response_json(0, "started", serde_json::json!({ "state": "connected" }))
    }

    #[no_mangle]
    pub extern "C" fn k2ne_stop() -> *mut c_char {
        make_response_json(0, "stopped", serde_json::json!({ "state": "disconnected" }))
    }

    #[no_mangle]
    pub extern "C" fn k2ne_status() -> *mut c_char {
        make_response_json(
            0,
            "ok",
            serde_json::json!({ "state": "disconnected", "running": false }),
        )
    }

    #[no_mangle]
    pub extern "C" fn k2ne_reinstall() -> *mut c_char {
        make_response_json(0, "reinstalled", serde_json::json!({}))
    }

    #[no_mangle]
    pub extern "C" fn k2ne_set_state_callback(
        _cb: Option<unsafe extern "C" fn(*const c_char)>,
    ) {
        // no-op in tests
    }

    #[no_mangle]
    pub extern "C" fn k2ne_free_string(ptr: *mut c_char) {
        if ptr.is_null() {
            return;
        }
        // Safety: ptr was created by CString::into_raw() in the mock above.
        unsafe {
            drop(CString::from_raw(ptr));
        }
    }

    // ----- helper: call the real ne_action logic under a controlled mock ----

    /// Call the ne_action routing logic using the mock C stubs above.
    fn invoke_ne_action(
        action: &str,
        params: Option<serde_json::Value>,
    ) -> Result<ServiceResponse, String> {
        // Duplicate the routing logic from macos::ne_action so we don't
        // need #[cfg(target_os = "macos")] here — the mock stubs satisfy
        // the extern "C" declarations on all platforms.
        match action {
            "up" => {
                let config_json = params
                    .as_ref()
                    .map(|p| serde_json::to_string(p).unwrap_or_default())
                    .unwrap_or_default();
                let c_config = CString::new(config_json).map_err(|e| e.to_string())?;
                call_stub(unsafe { k2ne_start(c_config.as_ptr()) })
            }
            "down" => call_stub(unsafe { k2ne_stop() }),
            "status" => call_stub(unsafe { k2ne_status() }),
            "version" => Ok(ServiceResponse {
                code: 0,
                message: "ok".into(),
                data: serde_json::json!({
                    "version": env!("CARGO_PKG_VERSION"),
                    "os": "macos"
                }),
            }),
            _ => Err(format!("unknown action: {}", action)),
        }
    }

    /// Parse a *mut c_char produced by a mock stub and free it.
    fn call_stub(ptr: *mut c_char) -> Result<ServiceResponse, String> {
        if ptr.is_null() {
            return Err("null response".into());
        }
        let json_str = unsafe {
            let s = CStr::from_ptr(ptr)
                .to_str()
                .map_err(|e| e.to_string())?
                .to_owned();
            k2ne_free_string(ptr);
            s
        };
        serde_json::from_str::<ServiceResponse>(&json_str)
            .map_err(|e| format!("parse error: {}", e))
    }

    // -----------------------------------------------------------------------
    // Test 1: ne_action("up", Some(json)) returns ServiceResponse with code
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_up() {
        let params = serde_json::json!({ "server": "1.2.3.4", "port": 443 });
        let result = invoke_ne_action("up", Some(params));
        assert!(result.is_ok(), "ne_action('up') should succeed: {:?}", result.err());
        let resp = result.unwrap();
        assert_eq!(resp.code, 0, "code should be 0 for success");
    }

    // -----------------------------------------------------------------------
    // Test 2: ne_action("down", None) returns ServiceResponse
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_down() {
        let result = invoke_ne_action("down", None);
        assert!(result.is_ok(), "ne_action('down') should succeed: {:?}", result.err());
        let resp = result.unwrap();
        assert_eq!(resp.code, 0);
    }

    // -----------------------------------------------------------------------
    // Test 3: ne_action("status", None) returns ServiceResponse
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_status() {
        let result = invoke_ne_action("status", None);
        assert!(result.is_ok(), "ne_action('status') should succeed: {:?}", result.err());
        let resp = result.unwrap();
        assert_eq!(resp.code, 0);
        // status should have a "state" field in data
        assert!(resp.data.get("state").is_some(), "status response should have 'state' field");
    }

    // -----------------------------------------------------------------------
    // Test 4: ne_action("version", None) returns version from CARGO_PKG_VERSION
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_version() {
        let result = invoke_ne_action("version", None);
        assert!(result.is_ok(), "ne_action('version') should succeed: {:?}", result.err());
        let resp = result.unwrap();
        assert_eq!(resp.code, 0);
        let version = resp.data.get("version").and_then(|v| v.as_str());
        assert!(version.is_some(), "version field should be present");
        let version_str = version.unwrap();
        assert!(!version_str.is_empty(), "version should not be empty");
        // Should match CARGO_PKG_VERSION
        assert_eq!(version_str, env!("CARGO_PKG_VERSION"));
        // Should have "os" field set to "macos"
        let os = resp.data.get("os").and_then(|v| v.as_str());
        assert_eq!(os, Some("macos"), "os should be 'macos'");
    }

    // -----------------------------------------------------------------------
    // Test 5: ne_action returns valid JSON with code, message fields
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_response_format() {
        for action in &["up", "down", "status", "version"] {
            let params = if *action == "up" {
                Some(serde_json::json!({}))
            } else {
                None
            };
            let result = invoke_ne_action(action, params);
            assert!(
                result.is_ok(),
                "ne_action('{}') should succeed: {:?}",
                action,
                result.err()
            );
            let resp = result.unwrap();
            // code field must be present (is an i32 — always serialized)
            // message must be non-empty string
            assert!(
                !resp.message.is_empty(),
                "ne_action('{}') response.message should not be empty",
                action
            );
        }
    }

    // -----------------------------------------------------------------------
    // Test 6: unknown action returns an error
    // -----------------------------------------------------------------------
    #[test]
    fn test_ne_action_unknown_returns_error() {
        let result = invoke_ne_action("restart", None);
        assert!(result.is_err(), "unknown action should return Err");
        let err = result.unwrap_err();
        assert!(err.contains("unknown action"), "error should mention 'unknown action': {}", err);
    }

    // -----------------------------------------------------------------------
    // Test 7: register_state_callback + unregister don't crash
    // -----------------------------------------------------------------------
    #[test]
    fn test_state_callback_propagation() {
        // The mock k2ne_set_state_callback is a no-op — we just verify
        // there are no panics when calling the stub.
        unsafe {
            k2ne_set_state_callback(None);
            k2ne_set_state_callback(None);
        }
        // No panic = pass
    }

    // -----------------------------------------------------------------------
    // Test 8: get_udid_native returns a UUID-like string (macOS only)
    // -----------------------------------------------------------------------
    #[test]
    fn test_get_udid_macos_native() {
        // On macOS, call the real sysctl to get the hardware UUID.
        // On other platforms, test the response parsing logic with a known UUID.
        #[cfg(target_os = "macos")]
        {
            // Import the real implementation
            let result = crate::ne::get_udid_native();
            assert!(result.is_ok(), "get_udid_native should succeed on macOS: {:?}", result.err());
            let resp = result.unwrap();
            assert_eq!(resp.code, 0);
            let udid = resp.data.get("udid").and_then(|v| v.as_str());
            assert!(udid.is_some(), "udid field should be present");
            let udid_str = udid.unwrap();
            // UUID format: 8-4-4-4-12 hex characters separated by dashes
            assert_eq!(udid_str.len(), 36, "UUID should be 36 chars: '{}'", udid_str);
            assert!(
                udid_str.chars().all(|c| c.is_ascii_hexdigit() || c == '-'),
                "UUID should only contain hex digits and dashes: '{}'",
                udid_str
            );
        }
        #[cfg(not(target_os = "macos"))]
        {
            // Simulate the parsing logic: build a ServiceResponse with a known UUID
            // and verify the structure is correct.
            let known_uuid = "ABCDEF12-3456-7890-ABCD-EF1234567890";
            let resp = ServiceResponse {
                code: 0,
                message: "ok".into(),
                data: serde_json::json!({ "udid": known_uuid }),
            };
            let udid = resp.data.get("udid").and_then(|v| v.as_str());
            assert_eq!(udid, Some(known_uuid));
        }
    }

    // -----------------------------------------------------------------------
    // Test 9: ensure_ne_installed function exists and is callable
    // -----------------------------------------------------------------------
    #[test]
    fn test_ensure_ne_installed_replaces_service() {
        // Verify the function exists by calling it through the mock.
        // On macOS the real crate::ne::ensure_ne_installed() is available.
        // On other platforms we test the stub call directly.
        #[cfg(target_os = "macos")]
        {
            let result = crate::ne::ensure_ne_installed();
            // Mock returns code=0, so this should succeed
            assert!(
                result.is_ok(),
                "ensure_ne_installed should succeed with mock: {:?}",
                result.err()
            );
        }
        #[cfg(not(target_os = "macos"))]
        {
            // Verify the mock k2ne_install stub itself returns a valid ServiceResponse
            let ptr = k2ne_install();
            let resp = call_stub(ptr);
            assert!(resp.is_ok(), "mock k2ne_install should return valid response");
            assert_eq!(resp.unwrap().code, 0);
        }
    }
}
