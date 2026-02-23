// K2NEHelper.swift
// Swift static library exposing macOS NetworkExtension control via C FFI.
// Rust callers link against libk2_ne_helper.a and call these C-exported functions.
// All async NE APIs are bridged synchronously via DispatchSemaphore running on a
// background queue (never the main queue) to avoid deadlocks.
// All returns are ServiceResponse JSON envelopes: {"code":0,"message":"ok","data":{...}}
// Callers must free returned char* pointers with k2ne_free_string().

import Foundation
import NetworkExtension
import SystemExtensions

// MARK: - Constants

private let kNEBundleId = "io.kaitu.desktop.tunnel"

// MARK: - Module-level State

private var stateCallback: (@convention(c) (UnsafePointer<CChar>) -> Void)? = nil
private var statusObserver: NSObjectProtocol? = nil
private var cachedManager: NETunnelProviderManager? = nil

// Serial queue for all NE operations to avoid concurrent semaphore waits
private let neQueue = DispatchQueue(label: "io.kaitu.ne-helper", qos: .userInitiated)

// MARK: - System Extension Activation

/// Delegate for OSSystemExtensionRequest that signals a semaphore on completion.
/// Used by k2ne_install() and k2ne_reinstall() to synchronously wait for sysext activation.
private class SysExtDelegate: NSObject, OSSystemExtensionRequestDelegate {
    let semaphore = DispatchSemaphore(value: 0)
    var result: OSSystemExtensionRequest.Result?
    var error: Error?

    func request(_ request: OSSystemExtensionRequest,
                 didFinishWithResult result: OSSystemExtensionRequest.Result) {
        self.result = result
        semaphore.signal()
    }

    func request(_ request: OSSystemExtensionRequest, didFailWithError error: Error) {
        self.error = error
        semaphore.signal()
    }

    func requestNeedsUserApproval(_ request: OSSystemExtensionRequest) {
        // macOS automatically shows System Settings → Privacy & Security dialog.
        // We just wait for the user to approve (semaphore timeout handles slow users).
        NSLog("[K2NEHelper] System Extension needs user approval — waiting for System Settings")
    }

    func request(_ request: OSSystemExtensionRequest,
                 actionForReplacingExtension existing: OSSystemExtensionProperties,
                 withExtension ext: OSSystemExtensionProperties) -> OSSystemExtensionRequest.ReplacementAction {
        // Auto-replace on app update — no re-approval needed
        return .replace
    }
}

/// Activate the System Extension, blocking until completion or timeout.
/// Returns (success, errorMessage). Timeout: 120s (user needs time for System Settings approval).
private func activateSystemExtension() -> (Bool, String?) {
    let delegate = SysExtDelegate()
    let request = OSSystemExtensionRequest.activationRequest(
        forExtensionWithIdentifier: kNEBundleId,
        queue: .main
    )
    request.delegate = delegate
    OSSystemExtensionManager.shared.submitRequest(request)

    let waitResult = delegate.semaphore.wait(timeout: .now() + 120.0)
    if waitResult == .timedOut {
        return (false, "System Extension activation timed out (120s). Please approve in System Settings → Privacy & Security and try again.")
    }
    if let err = delegate.error {
        return (false, "System Extension activation failed: \(err.localizedDescription)")
    }
    if let result = delegate.result {
        switch result {
        case .completed:
            NSLog("[K2NEHelper] System Extension activated successfully")
            return (true, nil)
        case .willCompleteAfterReboot:
            return (false, "System Extension will complete after reboot. Please restart your Mac.")
        @unknown default:
            return (true, nil)
        }
    }
    return (false, "System Extension activation returned no result")
}

/// Deactivate the System Extension, blocking until completion or timeout.
private func deactivateSystemExtension() -> (Bool, String?) {
    let delegate = SysExtDelegate()
    let request = OSSystemExtensionRequest.deactivationRequest(
        forExtensionWithIdentifier: kNEBundleId,
        queue: .main
    )
    request.delegate = delegate
    OSSystemExtensionManager.shared.submitRequest(request)

    let waitResult = delegate.semaphore.wait(timeout: .now() + 30.0)
    if waitResult == .timedOut {
        return (false, "System Extension deactivation timed out")
    }
    if let err = delegate.error {
        return (false, "System Extension deactivation failed: \(err.localizedDescription)")
    }
    return (true, nil)
}

// MARK: - Private Helpers

/// Build a ServiceResponse JSON string.
/// Returns a heap-allocated C string that the caller must free with k2ne_free_string().
private func serviceResponse(code: Int, message: String, data: Any? = nil) -> String {
    var dict: [String: Any] = ["code": code, "message": message]
    if let d = data {
        dict["data"] = d
    }
    guard
        let jsonData = try? JSONSerialization.data(withJSONObject: dict, options: []),
        let str = String(data: jsonData, encoding: .utf8)
    else {
        return "{\"code\":-1,\"message\":\"json serialization failed\"}"
    }
    return str
}

/// Allocate a C string copy. Caller must free with k2ne_free_string().
private func makeCString(_ str: String) -> UnsafeMutablePointer<CChar> {
    return strdup(str)!
}

/// Map NEVPNStatus to a state string matching the webapp's VPN state contract.
/// Mirrors the iOS K2Plugin.mapVPNStatus implementation exactly.
private func mapVPNStatus(_ status: NEVPNStatus) -> String {
    switch status {
    case .connected:     return "connected"
    case .connecting:    return "connecting"
    case .disconnecting: return "disconnecting"
    case .reasserting:   return "reconnecting"
    case .disconnected:  return "disconnected"
    case .invalid:       return "disconnected"
    @unknown default:    return "disconnected"
    }
}

/// Load the NETunnelProviderManager for kNEBundleId, blocking the calling thread.
/// Must be called from a background thread (not main queue) to avoid deadlock.
/// Returns cached manager if already loaded.
private func loadManager() -> NETunnelProviderManager? {
    // MUST: DispatchSemaphore must NOT wait on main queue — deadlock prevention
    dispatchPrecondition(condition: .notOnQueue(.main))
    if let m = cachedManager { return m }
    let sem = DispatchSemaphore(value: 0)
    var result: NETunnelProviderManager?
    NETunnelProviderManager.loadAllFromPreferences { managers, _ in
        result = managers?.first(where: {
            ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == kNEBundleId
        })
        sem.signal()
    }
    sem.wait()
    cachedManager = result
    return result
}

/// Save manager to preferences, blocking the calling thread.
/// Must be called from a background thread (not main queue) to avoid deadlock.
private func saveManager(_ manager: NETunnelProviderManager) -> Error? {
    // MUST: DispatchSemaphore must NOT wait on main queue — deadlock prevention
    dispatchPrecondition(condition: .notOnQueue(.main))
    let sem = DispatchSemaphore(value: 0)
    var saveError: Error?
    manager.saveToPreferences { err in
        saveError = err
        sem.signal()
    }
    sem.wait()
    return saveError
}

/// Reload manager from preferences, blocking the calling thread.
/// Must be called from a background thread (not main queue) to avoid deadlock.
private func reloadManager(_ manager: NETunnelProviderManager) -> Error? {
    // MUST: DispatchSemaphore must NOT wait on main queue — deadlock prevention
    dispatchPrecondition(condition: .notOnQueue(.main))
    let sem = DispatchSemaphore(value: 0)
    var loadError: Error?
    manager.loadFromPreferences { err in
        loadError = err
        sem.signal()
    }
    sem.wait()
    return loadError
}

/// Remove manager from preferences, blocking the calling thread.
/// Must be called from a background thread (not main queue) to avoid deadlock.
private func removeManager(_ manager: NETunnelProviderManager) -> Error? {
    // MUST: DispatchSemaphore must NOT wait on main queue — deadlock prevention
    dispatchPrecondition(condition: .notOnQueue(.main))
    let sem = DispatchSemaphore(value: 0)
    var removeError: Error?
    manager.removeFromPreferences { err in
        removeError = err
        sem.signal()
    }
    sem.wait()
    return removeError
}

/// Configure protocol on manager with the macOS NE bundle identifier.
private func configureProtocol(on manager: NETunnelProviderManager) {
    let proto = NETunnelProviderProtocol()
    proto.providerBundleIdentifier = kNEBundleId
    proto.serverAddress = "Kaitu VPN"
    manager.protocolConfiguration = proto
    manager.localizedDescription = "Kaitu VPN"
    manager.isEnabled = true
}

// MARK: - C-Exported Functions

/// Install the System Extension and Network Extension VPN profile.
/// Two-step flow: (1) activate System Extension if needed, (2) save VPN profile.
/// Returns ServiceResponse JSON: {"code":0,"message":"ok","data":{"installed":true}}
/// On error: {"code":-1,"message":"<error description>"}
@_cdecl("k2ne_install")
public func k2ne_install() -> UnsafeMutablePointer<CChar> {
    var resultJSON = ""

    let sem = DispatchSemaphore(value: 0)
    DispatchQueue.global(qos: .userInitiated).async {
        // Check if already installed (existing profile means sysext already activated)
        if loadManager() != nil {
            resultJSON = serviceResponse(
                code: 0,
                message: "ok",
                data: ["installed": true, "existing": true]
            )
            sem.signal()
            return
        }

        // Step 1: Activate System Extension (user approval required on first install)
        let (activated, activationError) = activateSystemExtension()
        if !activated {
            resultJSON = serviceResponse(code: -1, message: activationError ?? "System Extension activation failed")
            sem.signal()
            return
        }

        // Step 2: Create and save VPN profile
        let manager = NETunnelProviderManager()
        configureProtocol(on: manager)

        if let err = saveManager(manager) {
            resultJSON = serviceResponse(code: -1, message: "Failed to save VPN profile: \(err.localizedDescription)")
            sem.signal()
            return
        }

        // Cache the newly installed manager
        cachedManager = manager

        resultJSON = serviceResponse(code: 0, message: "ok", data: ["installed": true])
        sem.signal()
    }
    sem.wait()

    return makeCString(resultJSON)
}

/// Start the VPN tunnel with the given JSON configuration.
/// Auto-installs the NE profile if not already present (first-launch race prevention).
/// config_json: ClientConfig JSON string to pass to the tunnel provider.
/// Returns ServiceResponse JSON: {"code":0,"message":"ok"}
/// On error: {"code":-1,"message":"<error description>"}
@_cdecl("k2ne_start")
public func k2ne_start(_ config_json: UnsafePointer<CChar>?) -> UnsafeMutablePointer<CChar> {
    let configString = config_json.map { String(cString: $0) } ?? ""
    var resultJSON = ""

    let sem = DispatchSemaphore(value: 0)
    DispatchQueue.global(qos: .userInitiated).async {
        // Load or auto-install manager (includes System Extension activation)
        var manager = loadManager()
        if manager == nil {
            // Auto-install: activate sysext + create profile on first launch
            let (activated, activationError) = activateSystemExtension()
            if !activated {
                resultJSON = serviceResponse(code: -1, message: "Auto-install failed: \(activationError ?? "System Extension activation failed")")
                sem.signal()
                return
            }
            let newManager = NETunnelProviderManager()
            configureProtocol(on: newManager)
            if let err = saveManager(newManager) {
                resultJSON = serviceResponse(code: -1, message: "Auto-install failed: \(err.localizedDescription)")
                sem.signal()
                return
            }
            cachedManager = newManager
            manager = newManager
        }

        guard let mgr = manager else {
            resultJSON = serviceResponse(code: -1, message: "Failed to get VPN manager")
            sem.signal()
            return
        }

        // Update protocol with config and re-save
        let proto = (mgr.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
        proto.providerBundleIdentifier = kNEBundleId
        proto.serverAddress = "Kaitu VPN"
        proto.providerConfiguration = configString.isEmpty ? [:] : ["configJSON": configString]
        mgr.protocolConfiguration = proto
        mgr.localizedDescription = "Kaitu VPN"
        mgr.isEnabled = true

        if let err = saveManager(mgr) {
            resultJSON = serviceResponse(code: -1, message: "Failed to save config: \(err.localizedDescription)")
            sem.signal()
            return
        }

        if let err = reloadManager(mgr) {
            resultJSON = serviceResponse(code: -1, message: "Failed to reload config: \(err.localizedDescription)")
            sem.signal()
            return
        }

        do {
            let tunnelOptions: [String: NSObject]
            if configString.isEmpty {
                tunnelOptions = [:]
            } else {
                tunnelOptions = ["configJSON": NSString(string: configString)]
            }
            try (mgr.connection as? NETunnelProviderSession)?.startVPNTunnel(options: tunnelOptions)
            resultJSON = serviceResponse(code: 0, message: "ok")
        } catch {
            resultJSON = serviceResponse(code: -1, message: "Failed to start tunnel: \(error.localizedDescription)")
        }

        sem.signal()
    }
    sem.wait()

    return makeCString(resultJSON)
}

/// Stop the VPN tunnel.
/// Returns ServiceResponse JSON: {"code":0,"message":"ok"}
/// On error: {"code":-1,"message":"<error description>"}
@_cdecl("k2ne_stop")
public func k2ne_stop() -> UnsafeMutablePointer<CChar> {
    var resultJSON = ""

    let sem = DispatchSemaphore(value: 0)
    DispatchQueue.global(qos: .userInitiated).async {
        guard let manager = loadManager() else {
            // No profile installed — already stopped
            resultJSON = serviceResponse(code: 0, message: "ok")
            sem.signal()
            return
        }

        manager.connection.stopVPNTunnel()
        resultJSON = serviceResponse(code: 0, message: "ok")
        sem.signal()
    }
    sem.wait()

    return makeCString(resultJSON)
}

/// Get current VPN status.
/// Tries sendProviderMessage("status") with a 3-second timeout to get engine StatusJSON.
/// Falls back to mapVPNStatus(manager.connection.status) if NE is not running.
/// Returns ServiceResponse JSON: {"code":0,"message":"ok","data":{...engine status or state string...}}
@_cdecl("k2ne_status")
public func k2ne_status() -> UnsafeMutablePointer<CChar> {
    var resultJSON = ""

    let sem = DispatchSemaphore(value: 0)
    DispatchQueue.global(qos: .userInitiated).async {
        guard let manager = loadManager() else {
            // No profile — disconnected
            resultJSON = serviceResponse(code: 0, message: "ok", data: ["state": "disconnected"])
            sem.signal()
            return
        }

        guard let session = manager.connection as? NETunnelProviderSession else {
            let state = mapVPNStatus(manager.connection.status)
            resultJSON = serviceResponse(code: 0, message: "ok", data: ["state": state])
            sem.signal()
            return
        }

        let msgData = "status".data(using: .utf8)!
        let msgSem = DispatchSemaphore(value: 0)
        var engineData: [String: Any]? = nil
        var messageSent = false

        do {
            try session.sendProviderMessage(msgData) { responseData in
                if let data = responseData,
                   let json = try? JSONSerialization.jsonObject(with: data, options: []) as? [String: Any] {
                    engineData = json
                }
                msgSem.signal()
            }
            messageSent = true
        } catch {
            // NE not running — fall through to status fallback
        }

        if messageSent {
            // Wait up to 3 seconds for provider response
            let waitResult = msgSem.wait(timeout: .now() + 3.0)
            if waitResult == .success, let data = engineData {
                resultJSON = serviceResponse(code: 0, message: "ok", data: data)
            } else {
                // Timeout or no data — fall back to NEVPNStatus mapping
                let state = mapVPNStatus(manager.connection.status)
                resultJSON = serviceResponse(code: 0, message: "ok", data: ["state": state])
            }
        } else {
            // sendProviderMessage threw — NE not running
            let state = mapVPNStatus(manager.connection.status)
            resultJSON = serviceResponse(code: 0, message: "ok", data: ["state": state])
        }

        sem.signal()
    }
    sem.wait()

    return makeCString(resultJSON)
}

/// Register a C callback function to receive VPN state change notifications.
/// The callback is invoked with a state string (connected/disconnected/connecting/etc.)
/// whenever NEVPNStatusDidChange fires.
/// Replaces any previously registered callback.
@_cdecl("k2ne_set_state_callback")
public func k2ne_set_state_callback(
    _ callback: (@convention(c) (UnsafePointer<CChar>) -> Void)?
) {
    stateCallback = callback

    // Remove any existing observer
    if let observer = statusObserver {
        NotificationCenter.default.removeObserver(observer)
        statusObserver = nil
    }

    guard callback != nil else { return }

    // Register NEVPNStatusDidChange observer on global queue for thread safety
    statusObserver = NotificationCenter.default.addObserver(
        forName: .NEVPNStatusDidChange,
        object: nil,
        queue: nil  // nil = delivered on the notification thread
    ) { notification in
        // MUST 1: Thread-safe callback invocation — capture callback reference into
        // a local constant before dispatching to avoid races with k2ne_set_state_callback
        // being called concurrently to unregister/replace the callback.
        let capturedCallback = stateCallback
        DispatchQueue.global(qos: .userInitiated).async {
            guard let cb = capturedCallback else { return }
            let state: String
            if let connection = notification.object as? NEVPNConnection {
                state = mapVPNStatus(connection.status)
            } else if let manager = cachedManager {
                state = mapVPNStatus(manager.connection.status)
            } else {
                state = "disconnected"
            }
            state.withCString { ptr in
                cb(ptr)
            }
        }
    }
}

/// Reinstall: deactivate System Extension, remove VPN profile, reactivate, install fresh profile.
/// Invalidates the cached manager.
/// Returns ServiceResponse JSON: {"code":0,"message":"ok","data":{"installed":true}}
/// On error: {"code":-1,"message":"<error description>"}
@_cdecl("k2ne_reinstall")
public func k2ne_reinstall() -> UnsafeMutablePointer<CChar> {
    var resultJSON = ""

    let sem = DispatchSemaphore(value: 0)
    DispatchQueue.global(qos: .userInitiated).async {
        // Step 1: Remove existing VPN profile
        if let existing = loadManager() {
            if let err = removeManager(existing) {
                resultJSON = serviceResponse(code: -1, message: "Failed to remove profile: \(err.localizedDescription)")
                sem.signal()
                return
            }
        }
        cachedManager = nil

        // Step 2: Deactivate System Extension
        let (deactivated, deactivateErr) = deactivateSystemExtension()
        if !deactivated {
            NSLog("[K2NEHelper] Deactivation warning: \(deactivateErr ?? "unknown") — continuing with reactivation")
            // Non-fatal: proceed to reactivation even if deactivation fails
        }

        // Step 3: Reactivate System Extension
        let (activated, activateErr) = activateSystemExtension()
        if !activated {
            resultJSON = serviceResponse(code: -1, message: activateErr ?? "System Extension reactivation failed")
            sem.signal()
            return
        }

        // Step 4: Install fresh VPN profile
        let manager = NETunnelProviderManager()
        configureProtocol(on: manager)

        if let err = saveManager(manager) {
            resultJSON = serviceResponse(code: -1, message: "Failed to save VPN profile: \(err.localizedDescription)")
            sem.signal()
            return
        }

        cachedManager = manager
        resultJSON = serviceResponse(code: 0, message: "ok", data: ["installed": true])
        sem.signal()
    }
    sem.wait()

    return makeCString(resultJSON)
}

/// Free a C string previously returned by any k2ne_* function.
/// Must be called for every pointer returned by k2ne_install, k2ne_start, k2ne_stop,
/// k2ne_status, and k2ne_reinstall to avoid memory leaks.
@_cdecl("k2ne_free_string")
public func k2ne_free_string(_ ptr: UnsafeMutablePointer<CChar>?) {
    guard let p = ptr else { return }
    free(p)
}
