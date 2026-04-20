package io.kaitu

import android.app.Application
import android.util.Log
import appext.Appext

/**
 * Application entry point. Runs before any Activity / Capacitor plugin.
 *
 * Prefetches rule bundles so the first Connect finds the cache warm and
 * doesn't block on a cold 10-second download inside engine.Start inside
 * K2VpnService. K2VpnService runs in the same process, so `cacheDir` is
 * shared on disk and in memory semantics.
 *
 * Non-fatal: any failure just means the first connect takes longer;
 * engine.Start retries internally via rule.EnsureBundles singleflight.
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
