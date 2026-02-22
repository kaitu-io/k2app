// k2_ne_helper.h
// C header for the K2NEHelper Swift static library (libk2_ne_helper.a).
// Include this header from Rust build.rs or link it via bindgen to call
// the Swift NE control functions.
//
// All functions that return char* return heap-allocated ServiceResponse JSON:
//   {"code": 0, "message": "ok", "data": {...}}
//   {"code": -1, "message": "<error description>"}
//
// IMPORTANT: Every returned char* MUST be freed by calling k2ne_free_string()
// after use. Failure to do so will leak memory.
//
// Threading: All functions are safe to call from any thread.
// They dispatch NE operations internally to a background queue to avoid
// deadlocks (DispatchSemaphore must not wait on main queue).

#ifndef K2_NE_HELPER_H
#define K2_NE_HELPER_H

#ifdef __cplusplus
extern "C" {
#endif

/// Install the macOS Network Extension VPN profile.
/// Creates a new NETunnelProviderManager for io.kaitu.desktop.tunnel if none exists.
/// Returns: ServiceResponse JSON string (caller must free with k2ne_free_string).
char* k2ne_install(void);

/// Start the VPN tunnel with the given JSON configuration string.
/// Auto-installs the NE profile if not already present (first-launch race prevention).
/// config_json: ClientConfig JSON string (may be NULL or empty string).
/// Returns: ServiceResponse JSON string (caller must free with k2ne_free_string).
char* k2ne_start(const char* config_json);

/// Stop the VPN tunnel.
/// Returns: ServiceResponse JSON string (caller must free with k2ne_free_string).
char* k2ne_stop(void);

/// Get current VPN status.
/// Tries sendProviderMessage("status") with a 3-second timeout to get engine StatusJSON.
/// Falls back to NEVPNStatus mapping if the NE process is not running.
/// Returns: ServiceResponse JSON string (caller must free with k2ne_free_string).
/// Example data field: {"state":"connected"} or full engine status object.
char* k2ne_status(void);

/// Reinstall the VPN profile: removes existing profile, then installs a fresh one.
/// Use this when the profile is corrupted or when updating the NE bundle.
/// Returns: ServiceResponse JSON string (caller must free with k2ne_free_string).
char* k2ne_reinstall(void);

/// Register a C callback function for VPN state change notifications.
/// The callback is invoked with a state string on NEVPNStatusDidChange:
///   "connected" | "connecting" | "disconnecting" | "reconnecting" | "disconnected"
/// Pass NULL to unregister the current callback.
/// The callback is invoked on a background queue (not the main queue).
void k2ne_set_state_callback(void (*callback)(const char* state));

/// Free a C string previously returned by any k2ne_* function.
/// Must be called after every non-void k2ne_* function call.
void k2ne_free_string(char* ptr);

#ifdef __cplusplus
}
#endif

#endif /* K2_NE_HELPER_H */
