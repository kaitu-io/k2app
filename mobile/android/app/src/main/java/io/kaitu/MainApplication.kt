package io.kaitu

import android.app.Application
import android.util.Log
import appext.Appext

/**
 * Application entry point. Runs before any Activity / Capacitor plugin.
 *
 * Refreshes rule bundles in the background so a newer rule set is usually on
 * disk before the user connects. engine.Start (inside K2VpnService) never
 * waits for it — the connect path is local-only and serves whatever is on
 * disk (#3889); if this lands a new set after the engine loaded the old one,
 * the engine's post-connect updater hot-reloads it. K2VpnService runs in the
 * same process, so `cacheDir` is shared on disk and in memory semantics.
 *
 * Non-fatal: a failure only means the engine keeps the cached (or embedded)
 * rules until its updater's next check succeeds.
 */
class MainApplication : Application() {
    companion object {
        private const val TAG = "MainApplication"
    }

    override fun onCreate() {
        super.onCreate()
        Thread {
            try {
                Appext.prefetchRules(cacheDir.absolutePath)
            } catch (t: Throwable) {
                Log.w(TAG, "rule prefetch failed", t)
            }
        }.start()
    }
}
