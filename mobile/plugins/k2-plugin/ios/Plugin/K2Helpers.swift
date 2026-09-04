import Foundation

/// Remap Go StatusJSON snake_case keys to JS camelCase.
func remapStatusKeys(_ json: [String: Any]) -> [String: Any] {
    let keyMap: [String: String] = [
        "connected_at": "connectedAt",
        "uptime_seconds": "uptimeSeconds",
    ]
    var result: [String: Any] = [:]
    for (key, value) in json {
        let newKey = keyMap[key] ?? key
        result[newKey] = value
    }
    return result
}

/// Map NEVPNStatus raw value to string without importing NetworkExtension.
/// Raw values: 0=invalid, 1=disconnected, 2=connecting, 3=connected, 4=reasserting, 5=disconnecting
func mapVPNStatusString(_ rawValue: Int) -> String {
    switch rawValue {
    case 3: return "connected"
    case 2: return "connecting"
    case 5: return "disconnecting"
    case 4: return "reconnecting"
    case 0, 1: return "disconnected"
    default: return "disconnected"
    }
}

/// Semantic version comparison: true if remote > local.
/// Handles -beta.N pre-release suffixes correctly.
func isNewerVersion(_ remote: String, than local: String) -> Bool {
    let (rBase, rPre) = splitVersion(remote)
    let (lBase, lPre) = splitVersion(local)
    let baseCmp = compareSegments(rBase, lBase)
    if baseCmp != 0 { return baseCmp > 0 }
    // Same base: stable (no pre-release) > beta (has pre-release)
    if rPre == nil && lPre != nil { return true }
    if rPre != nil && lPre == nil { return false }
    if rPre == nil && lPre == nil { return false }
    // Both have pre-release: compare segments
    let rPreSegs = rPre!.split(separator: ".").map { Int($0) ?? 0 }
    let lPreSegs = lPre!.split(separator: ".").map { Int($0) ?? 0 }
    return compareSegments(rPreSegs, lPreSegs) > 0
}

/// Check if appVersion's base version >= minNative's base version.
/// Ignores pre-release: 0.4.0-beta.6 satisfies min_native=0.4.0.
func isCompatibleNativeVersion(_ minNative: String?, appVersion: String) -> Bool {
    guard let minNative = minNative, !minNative.isEmpty else { return true }
    let (minBase, _) = splitVersion(minNative)
    let (appBase, _) = splitVersion(appVersion)
    return compareSegments(appBase, minBase) >= 0
}

private func splitVersion(_ v: String) -> (base: [Int], pre: String?) {
    let parts = v.split(separator: "-", maxSplits: 1)
    let base = parts[0].split(separator: ".").map { Int($0) ?? 0 }
    let pre = parts.count > 1 ? String(parts[1]) : nil
    return (base, pre)
}

private func compareSegments(_ a: [Int], _ b: [Int]) -> Int {
    let maxLen = max(a.count, b.count)
    for i in 0..<maxLen {
        let av = i < a.count ? a[i] : 0
        let bv = i < b.count ? b[i] : 0
        if av != bv { return av < bv ? -1 : 1 }
    }
    return 0
}

/// Real semantic version of this build, taken from package.json (the
/// cross-layer source of truth) by scripts/sync-version.sh, which
/// `make pre-build` runs on every build path.
///
/// Compiled in rather than read from Info.plist ON PURPOSE — this value feeds
/// the web-OTA `min_native` gate, and a gate must never be able to fall back to
/// reading a *different quantity*. See K2Plugin.appVersion for the fail-open
/// bug that motivated this.
let k2AppVersion = "0.4.10"

/// Compile-time bridge API version. MUST equal BRIDGE_API_VERSION in
/// webapp/src/types/bridge-version.ts — the webapp contract gate
/// (bridge-contract.test.ts) greps this file's literal and fails on drift.
let k2BridgeApiVersion = 3

/// min_bridge gate for web OTA manifests (spec §4). nil / absent (manifests
/// published before the bridge-version era) always passes.
func isCompatibleBridgeVersion(_ minBridge: Int?, bridgeVersion: Int = k2BridgeApiVersion) -> Bool {
    guard let minBridge = minBridge else { return true }
    return minBridge <= bridgeVersion
}

// MARK: - Web OTA boot decision

/// Cold-start decision for the web-OTA bundle on disk. Swift mirror of
/// Android's `WebBootDecision` — the two shells must fail identically.
///
/// WHY THIS EXISTS — "boot verified" != "UI rendered"
/// ==================================================
/// Until 2026-08 the shell cleared `.boot-pending` inside `checkReady()`, which
/// the webapp calls from `injectCapacitorGlobals()` — BEFORE store init and the
/// first React render. A bundle that loaded its JS and then died during either
/// stage still reported "boot verified": marker gone, next cold start saw a
/// clean bundle, user got the same white screen forever. The desktop shell hit
/// exactly this (2026-08-18) and moved its handshake after `ReactDOM.render`;
/// mobile kept the old shape, where it is strictly worse — no `?ui=embedded`
/// escape hatch, and a bad bundle can only be undone by a new store release.
///
/// The handshake is now `confirmWebBootOk()`, called from main.tsx only after
/// the app rendered. `checkReady()` no longer touches the marker.
enum WebBootDecision: Equatable {
    /// Serve the OTA bundle from disk. Caller arms `.boot-pending` first.
    case serveDisk
    /// Last boot never confirmed it rendered: delete the bundle, fall back to
    /// the bundled webapp, and quarantine `version` so it is not re-applied.
    /// `version` is nil when version.txt was missing or unreadable.
    case rollback(version: String?)
    /// Bundle directory present but has no index.html — remove it.
    case cleanCorrupt
    /// Nothing usable on disk; serve the webapp bundled in the app.
    case serveBundled
}

func decideWebBoot(
    hasWebUpdateDir: Bool,
    hasBootPending: Bool,
    hasIndex: Bool,
    diskVersion: String?
) -> WebBootDecision {
    guard hasWebUpdateDir else { return .serveBundled }
    // Checked before the corrupt case on purpose: an unconfirmed boot is a
    // FAILURE and must be quarantined, whereas cleanCorrupt merely tidies up.
    // Reordering silently drops the quarantine for half the failures.
    if hasBootPending { return .rollback(version: diskVersion) }
    return hasIndex ? .serveDisk : .cleanCorrupt
}

/// Version gate for applying a web bundle: nil = apply, non-nil = loggable
/// skip reason. `min_native` / `min_bridge` are checked separately by the
/// caller (they have their own helpers); this covers the two version rules.
///
/// The quarantine half is what stops the reinstall treadmill: rollback deletes
/// version.txt, so `localVersion` falls back to the app version and the same
/// bad bundle reads as "newer" on the very next check — and `fetchManifest`
/// has neither cache nor backoff.
func webBundleSkipReason(
    remoteVersion: String,
    localVersion: String,
    quarantinedVersion: String?
) -> String? {
    if !isNewerVersion(remoteVersion, than: localVersion) { return "no newer version" }
    if let q = quarantinedVersion, q == remoteVersion { return "version quarantined" }
    return nil
}
