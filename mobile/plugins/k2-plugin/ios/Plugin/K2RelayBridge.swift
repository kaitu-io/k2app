import Foundation

/// Bridge that lets the K2Plugin pod reach the gomobile-exported relay function
/// (RelayFetch). That symbol lives in K2Mobile.xcframework, which is linked to the
/// **App target** (and the NE target) — NOT to this static CocoaPod. So the pod
/// cannot `import K2Mobile` directly; instead the App target's AppDelegate
/// registers a handler at launch that wraps the gomobile relay call — see
/// `AppDelegate.didFinishLaunchingWithOptions`.
///
/// String-in/string-out matches the gomobile boundary and the daemon's
/// relay-fetch action. This mirrors Android's `VpnServiceBridge.relayFetch`.
/// The relay runs in the App process and is VPN-independent — a single ordinary
/// outbound used for control-plane bootstrap before the tunnel exists.
public enum K2RelayBridge {
    /// Set once by the App target. `nil` until registered → the plugin reports
    /// relay unsupported (code:-1) so the webapp transport uses the direct path.
    public static var handler: ((String) -> String)?

    /// Relay node feed (gomobile RelayAddNodes). Incrementally registers
    /// camouflage-node descriptors with the gomobile RelayManager, which owns
    /// node storage/ranking/health. Also set by the App target's AppDelegate.
    /// `nil` until registered → the plugin reports a transient code so the webapp
    /// neither errors nor learns relay is unsupported.
    public static var addNodesHandler: ((String) -> String)?
}
