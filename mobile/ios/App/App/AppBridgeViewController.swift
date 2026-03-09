import UIKit
import Capacitor
import os.log

private let logger = Logger(subsystem: "com.allnationconnect.anc.wgios", category: "AppBridge")

/// Fix for CapacitorRouter empty-path bug:
/// URL(fileURLWithPath: "") resolves to cwd, whose pathExtension may not be empty,
/// causing the router to return the directory instead of index.html.
struct FixedCapacitorRouter: Router {
    var basePath: String = ""

    func route(for path: String) -> String {
        if path.isEmpty || path == "/" {
            return basePath + "/index.html"
        }
        let pathUrl = URL(fileURLWithPath: path)
        if pathUrl.pathExtension.isEmpty {
            return basePath + "/index.html"
        }
        return basePath + path
    }
}

class AppBridgeViewController: CAPBridgeViewController {
    override func router() -> Router {
        return FixedCapacitorRouter()
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        // DIAGNOSTIC: unconditionally enable isInspectable to test if Safari can see the WebView
        if #available(iOS 16.4, *) {
            webView?.isInspectable = true
            NSLog("[AppBridge] viewDidLoad: FORCED isInspectable=true, webView=%@", webView != nil ? "exists" : "nil")
        }
        NotificationCenter.default.addObserver(self, selector: #selector(handleDevEnabledChanged(_:)), name: Notification.Name("k2DevEnabledChanged"), object: nil)
    }

    @objc private func handleDevEnabledChanged(_ notification: Notification) {
        logger.info("handleDevEnabledChanged: received notification")
        guard let enabled = notification.userInfo?["enabled"] as? Bool else {
            logger.warning("handleDevEnabledChanged: missing 'enabled' in userInfo")
            return
        }
        logger.info("handleDevEnabledChanged: enabled=\(enabled), webView=\(self.webView != nil ? "exists" : "nil")")
        if #available(iOS 16.4, *) {
            webView?.isInspectable = enabled
            let actual = webView?.isInspectable ?? false
            logger.info("handleDevEnabledChanged: isInspectable now=\(actual)")
        }
    }
}
