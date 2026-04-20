import UIKit
import Capacitor
import K2Mobile

private let kAppGroup = "group.io.kaitu"

@UIApplicationMain
class AppDelegate: UIResponder, UIApplicationDelegate {
    var window: UIWindow?

    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        // Prefetch rule bundles so the first connect finds the cache warm and
        // doesn't block on a cold 10-second download inside NE's engine.Start.
        // Runs in the main App process; NE (separate process, 50 MB jetsam)
        // reads the same cache via the App Group container.
        //
        // Also mark the rules directory as excluded from iCloud backup — bundles
        // are re-downloadable and shouldn't eat user backup quota.
        if let containerURL = FileManager.default
                .containerURL(forSecurityApplicationGroupIdentifier: kAppGroup) {

            let rulesURL = containerURL.appendingPathComponent("k2/rules")
            try? FileManager.default.createDirectory(
                at: rulesURL, withIntermediateDirectories: true)

            var excludedURL = rulesURL
            var resourceValues = URLResourceValues()
            resourceValues.isExcludedFromBackup = true
            try? excludedURL.setResourceValues(resourceValues)

            DispatchQueue.global(qos: .utility).async {
                // cacheDir is the BASE — bundles live in cacheDir + "/rules",
                // matching engine.Start's clientCfg.CacheDir layout.
                AppextPrefetchRules(containerURL.appendingPathComponent("k2").path)
            }
        } else {
            NSLog("[AppDelegate] rule prefetch skipped — app group container unavailable")
        }

        return true
    }
}
