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
import { K2Plugin } from 'k2-plugin';
import type { IK2Vpn, IPlatform, IUpdater, UpdateInfo, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError, ServiceState } from './vpn-types';
import { webSecureStorage } from './secure-storage';
import { useVPNStore } from '../stores/vpn.store';

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
  if (raw.error) {
    if (typeof raw.error === 'object' && raw.error !== null && 'code' in raw.error) {
      error = { code: raw.error.code, message: raw.error.message || '' };
    } else {
      // Backward compat: old daemon sends string
      error = { code: 570, message: String(raw.error) };
    }
    if (state === 'disconnected') {
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
    retrying: false,
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

    syncLocale: async (_locale: string): Promise<void> => {
      // No-op on mobile — no tray menu to update
    },

    updater,
  };

  // Inject globals
  (window as any)._k2 = capacitorK2;
  (window as any)._platform = capacitorPlatform;

  // Register event listeners — wire native events to VPN store for instant UI updates.
  // Polling is the steady-state source, but events provide immediate feedback on connect/error.
  K2Plugin.addListener('vpnStateChange', (event: any) => {
    console.debug('[K2:Capacitor] vpnStateChange:', event.state,
      event.connectedAt ? `connectedAt=${event.connectedAt}` : '');
    // Push native state change into VPN store immediately (don't wait for next poll)
    try {
      const status = transformStatus(event);
      const store = useVPNStore.getState();
      store.setStatus(status);
      // Clear optimistic state on terminal states so UI reflects reality
      if (event.state === 'connected' || event.state === 'disconnected' || event.state === 'error') {
        store.setOptimisticState(null);
      }
    } catch (e) {
      // Store may not be initialized yet during startup — polling will catch up
    }
  });

  K2Plugin.addListener('vpnError', (event: any) => {
    console.warn('[K2:Capacitor] vpnError:', event.code ?? 'no-code', event.message ?? event);
    // Push error into VPN store immediately
    try {
      const store = useVPNStore.getState();
      const currentStatus = store.status;
      // Use structured error code from event if available, fallback to 570 (unclassified)
      const errorCode = typeof event.code === 'number' ? event.code : 570;
      store.setStatus({
        ...currentStatus,
        state: 'error',
        running: false,
        networkAvailable: currentStatus?.networkAvailable ?? true,
        error: { code: errorCode, message: event.message ?? String(event) },
        retrying: false,
      });
      store.setOptimisticState(null);
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
}
