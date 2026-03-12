/**
 * Capacitor Mobile Bridge
 *
 * Injects window._k2 (VPN control via K2Plugin) and window._platform (mobile capabilities)
 * when running inside a Capacitor native app (iOS/Android).
 *
 * Detection: Capacitor.isNativePlatform() returns true in native context.
 */

import { Capacitor } from '@capacitor/core';
import { Browser } from '@capacitor/browser';
import { Clipboard } from '@capacitor/clipboard';
import { Share } from '@capacitor/share';
import { K2Plugin } from 'k2-plugin';
import type { IK2Vpn, IPlatform, IUpdater, UpdateInfo, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError, ServiceState } from './vpn-types';
import { webSecureStorage } from './secure-storage';
import { dispatch as vpnDispatch, backendStatusToEvent } from '../stores/vpn-machine.store';

/**
 * Check if running inside a Capacitor native environment.
 */
export function isCapacitorNative(): boolean {
  return Capacitor.isNativePlatform();
}

/**
 * Transform K2Plugin's raw status into StatusResponseData format.
 * Error synthesis: disconnected + error -> error state.
 */
function transformStatus(raw: any): StatusResponseData {
  let state: ServiceState = raw.state ?? 'disconnected';
  const running = state === 'connecting' || state === 'connected';

  let error: ControlError | undefined;
  let retrying = false;
  if (raw.error) {
    if (typeof raw.error === 'object' && raw.error !== null && 'code' in raw.error) {
      error = { code: raw.error.code, message: raw.error.message || '' };
    } else {
      // Backward compat: old daemon sends string
      error = { code: 570, message: String(raw.error) };
    }
    if (state === 'disconnected' || state === 'connected') {
      // connected + error: TUN up but wire broken — engine retries on next traffic
      // disconnected + error: engine gave up
      const isClientError = [400, 401, 402, 403].includes(error.code);
      retrying = state === 'connected' && !isClientError;
      state = 'error';
    }
  }

  let startAt: number | undefined;
  if (raw.connectedAt) {
    startAt = Math.floor(new Date(raw.connectedAt).getTime() / 1000);
  }

  return {
    state,
    running,
    networkAvailable: true,
    startAt,
    error,
    retrying,
  };
}

/**
 * Inject Capacitor-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectCapacitorGlobals(): Promise<void> {
  // Get app version from K2Plugin
  let appVersion = 'unknown';
  try {
    const readyResult = await K2Plugin.checkReady();
    if (readyResult?.version) {
      appVersion = readyResult.version;
    }
  } catch (err) {
    console.warn('[K2:Capacitor] checkReady failed, using fallback version:', err);
  }

  const platform = Capacitor.getPlatform() as IPlatform['os'];

  // Build _k2: VPN control via K2Plugin
  const capacitorK2: IK2Vpn = {
    run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
      try {
        switch (action) {
          case 'status': {
            const raw = await K2Plugin.getStatus();
            const data = transformStatus(raw);
            return { code: 0, message: 'ok', data: data as unknown as T };
          }

          case 'up': {
            if (!params) {
              return { code: -1, message: 'Config is required for connect' };
            }
            await K2Plugin.connect({ config: JSON.stringify(params) });
            return { code: 0, message: 'ok' };
          }

          case 'down': {
            await K2Plugin.disconnect();
            return { code: 0, message: 'ok' };
          }

          case 'version': {
            const versionInfo = await K2Plugin.getVersion();
            return { code: 0, message: 'ok', data: versionInfo as unknown as T };
          }

          default:
            return { code: -1, message: `Unknown action: ${action}` };
        }
      } catch (error) {
        return {
          code: -1,
          message: error instanceof Error ? error.message : String(error),
        };
      }
    },
  };

  // Build updater: native update support
  let updateReadyCallbacks: ((info: UpdateInfo) => void)[] = [];
  let storedPath: string | null = null;
  let storedAppStoreUrl: string | null = null;

  const updater: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,
    channel: 'stable',
    applyUpdateNow: async () => {
      const currentPlatform = Capacitor.getPlatform();
      if (currentPlatform === 'android' && storedPath) {
        await K2Plugin.installNativeUpdate({ path: storedPath });
      } else if (currentPlatform === 'ios' && storedAppStoreUrl) {
        await Browser.open({ url: storedAppStoreUrl });
      }
    },
    onUpdateReady: (callback: (info: UpdateInfo) => void) => {
      updateReadyCallbacks.push(callback);
      return () => {
        updateReadyCallbacks = updateReadyCallbacks.filter(cb => cb !== callback);
      };
    },
  };

  // Build _platform: mobile capabilities
  const capacitorPlatform: IPlatform = {
    os: platform,
    version: appVersion,
    arch: 'arm64',

    storage: webSecureStorage,

    getUdid: async (): Promise<string> => {
      const result = await K2Plugin.getUDID();
      return result.udid;
    },

    openExternal: async (url: string): Promise<void> => {
      await Browser.open({ url });
    },

    writeClipboard: async (text: string): Promise<void> => {
      await Clipboard.write({ string: text });
    },

    readClipboard: async (): Promise<string> => {
      const result = await Clipboard.read();
      return result.value ?? '';
    },

    share: async (params: { text: string; title?: string; url?: string }): Promise<void> => {
      await Share.share({
        title: params.title,
        text: params.text,
        url: params.url,
        dialogTitle: params.title,
      });
    },

    syncLocale: async (_locale: string): Promise<void> => {
      // No-op on mobile — no tray menu to update
    },

    updater,

    uploadLogs: async (params) => {
      const result = await K2Plugin.uploadLogs({
        email: params.email ?? undefined,
        reason: params.reason,
        feedbackId: params.feedbackId,
        platform: params.platform,
        version: params.version,
      });
      return result;
    },

    setLogLevel: (level: string): void => {
      localStorage.setItem('k2_log_level', level);
      K2Plugin.setLogLevel({ level }).catch(() => {});
    },

    setDevEnabled: (enabled: boolean): void => {
      K2Plugin.setDevEnabled({ enabled }).catch(() => {});
    },
  };

  // Inject globals
  (window as any)._k2 = capacitorK2;
  (window as any)._platform = capacitorPlatform;

  // Register event listeners — wire native events to VPN machine store.
  // Events provide immediate feedback; polling is fallback.
  K2Plugin.addListener('vpnStateChange', (event: any) => {
    console.debug('[K2:Capacitor] vpnStateChange:', event.state,
      event.connectedAt ? `connectedAt=${event.connectedAt}` : '');
    try {
      const status = transformStatus(event);
      const machineEvent = backendStatusToEvent(status);
      vpnDispatch(machineEvent, {
        error: status.error ?? null,
        isRetrying: status.retrying ?? false,
        networkAvailable: status.networkAvailable ?? true,
      });
    } catch (e) {
      // Store may not be initialized yet during startup — polling will catch up
    }
  });

  K2Plugin.addListener('vpnError', (event: any) => {
    console.warn('[K2:Capacitor] vpnError:', event.code ?? 'no-code', event.message ?? event);
    try {
      const errorCode = typeof event.code === 'number' ? event.code : 570;
      vpnDispatch('BACKEND_ERROR', {
        error: { code: errorCode, message: event.message ?? String(event) },
        isRetrying: false,
        networkAvailable: true,
      });
    } catch (e) {
      // Store may not be initialized yet — polling will catch up
    }
  });

  K2Plugin.addListener('nativeUpdateReady', (event: any) => {
    storedPath = event.path;
    const info: UpdateInfo = {
      currentVersion: appVersion,
      newVersion: event.version,
    };
    updater.isUpdateReady = true;
    updater.updateInfo = info;
    updateReadyCallbacks.forEach(cb => cb(info));
  });

  K2Plugin.addListener('nativeUpdateAvailable', (event: any) => {
    storedAppStoreUrl = event.appStoreUrl;
    const info: UpdateInfo = {
      currentVersion: appVersion,
      newVersion: event.version,
    };
    updater.isUpdateReady = true;
    updater.updateInfo = info;
    updateReadyCallbacks.forEach(cb => cb(info));
  });

  console.info(`[K2:Capacitor] Injected - os=${platform}, version=${appVersion}`);

  // Auto-restore dev mode from previous session
  if (localStorage.getItem('k2_developer_mode') === 'true') {
    K2Plugin.setDevEnabled({ enabled: true }).catch(() => {});
  }

  // Forward WebView console.* to native file logging (webapp.log)
  // Buffers entries and flushes to K2Plugin.appendLogs() on threshold/timer/visibility
  try {
    function formatArgs(args: any[]): string {
      return args.map(a => {
        if (typeof a === 'string') return a;
        if (a instanceof Error) return `${a.message}${a.stack ? '\n' + a.stack : ''}`;
        try { return JSON.stringify(a); } catch { return String(a); }
      }).join(' ');
    }

    let buffer: Array<{ level: string; message: string; timestamp: number }> = [];
    let flushTimer: ReturnType<typeof setTimeout> | null = null;
    const FLUSH_THRESHOLD = 50;
    const FLUSH_INTERVAL = 3000;

    function flushBuffer() {
      if (flushTimer) { clearTimeout(flushTimer); flushTimer = null; }
      if (buffer.length === 0) return;
      const entries = buffer;
      buffer = [];
      K2Plugin.appendLogs?.({ entries })?.catch?.(() => {});
    }

    function scheduleFlush() {
      if (flushTimer) return;
      flushTimer = setTimeout(flushBuffer, FLUSH_INTERVAL);
    }

    function pushEntry(level: string, args: any[]) {
      buffer.push({ level, message: formatArgs(args), timestamp: Date.now() });
      if (buffer.length >= FLUSH_THRESHOLD) {
        flushBuffer();
      } else {
        scheduleFlush();
      }
    }

    const _log = console.log;
    const _debug = console.debug;
    const _info = console.info;
    const _warn = console.warn;
    const _error = console.error;

    console.log = (...args: any[]) => { _log(...args); pushEntry('log', args); };
    console.debug = (...args: any[]) => { _debug(...args); pushEntry('debug', args); };
    console.info = (...args: any[]) => { _info(...args); pushEntry('info', args); };
    console.warn = (...args: any[]) => { _warn(...args); pushEntry('warn', args); flushBuffer(); };
    console.error = (...args: any[]) => { _error(...args); pushEntry('error', args); flushBuffer(); };

    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') flushBuffer();
    });
  } catch {
    // Console interceptor setup failed, skip
  }
}
