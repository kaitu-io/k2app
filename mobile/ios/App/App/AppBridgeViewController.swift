import UIKit
import Capacitor

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
}
