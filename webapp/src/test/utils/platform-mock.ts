import { vi } from 'vitest';

/**
 * Install a `window._platform` mock for the daemon-backed (Tauri / standalone)
 * path. Sets the `appBypass.daemonBacked` capability flag so the store routes
 * through `window._k2.run('app-bypass-*')` instead of `_platform.storage`.
 *
 * Pair with {@link mockK2Run} to script the daemon responses.
 */
export function mockDaemonBackedPlatform() {
  (window as any)._platform = {
    storage: {
      get: vi.fn().mockResolvedValue(null),
      set: vi.fn().mockResolvedValue(undefined),
      remove: vi.fn().mockResolvedValue(undefined),
      has: vi.fn().mockResolvedValue(false),
      clear: vi.fn().mockResolvedValue(undefined),
      keys: vi.fn().mockResolvedValue([]),
    },
    appList: { listRunning: vi.fn().mockResolvedValue([]) },
    appBypass: { daemonBacked: true },
  };
}

/**
 * Install a `window._platform` mock for the mobile (Capacitor) path —
 * NO `appBypass.daemonBacked` flag, so the store falls through to local
 * storage persistence.
 */
export function mockMobilePlatform() {
  (window as any)._platform = {
    storage: {
      get: vi.fn().mockResolvedValue(null),
      set: vi.fn().mockResolvedValue(undefined),
      remove: vi.fn().mockResolvedValue(undefined),
      has: vi.fn().mockResolvedValue(false),
      clear: vi.fn().mockResolvedValue(undefined),
      keys: vi.fn().mockResolvedValue([]),
    },
    appList: { listInstalled: vi.fn().mockResolvedValue([]) },
    // appBypass intentionally absent
  };
}

/**
 * Install a `window._k2.run` mock backed by the given implementation. The
 * impl receives `(action, params)` and should return `{ code, message, data }`
 * shaped like a daemon response.
 */
export function mockK2Run(impl: (action: string, params?: any) => any) {
  (window as any)._k2 = { run: vi.fn(impl) };
}
