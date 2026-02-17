/**
 * Capacitor Mobile Bridge
 *
 * Injects window._k2 (VPN control via K2Plugin) and window._platform (mobile capabilities)
 * when running inside a Capacitor native app (iOS/Android).
 *
 * Detection: Capacitor.isNativePlatform() returns true in native context.
 */

import { Capacitor } from '@capacitor/core';
import { K2Plugin } from 'k2-plugin';
import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError } from './control-types';
import { webSecureStorage } from './secure-storage';

/**
 * Check if running inside a Capacitor native environment.
 */
export function isCapacitorNative(): boolean {
  return Capacitor.isNativePlatform();
}

/**
 * Transform K2Plugin's raw status into StatusResponseData format.
 */
function transformStatus(raw: any): StatusResponseData {
  const state = raw.state ?? 'disconnected';
  const running = state === 'connecting' || state === 'connected';

  let error: ControlError | undefined;
  if (raw.error) {
    error = { code: 570, message: raw.error };
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

  // Build _platform: mobile capabilities
  const capacitorPlatform: IPlatform = {
    os: platform,
    isMobile: true,
    isDesktop: false,
    version: appVersion,

    storage: webSecureStorage,

    getUdid: async (): Promise<string> => {
      const result = await K2Plugin.getUDID();
      return result.udid;
    },

    writeClipboard: async (text: string): Promise<void> => {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      }
    },

    readClipboard: async (): Promise<string> => {
      if (navigator.clipboard) {
        return navigator.clipboard.readText();
      }
      return '';
    },

    debug: (message: string) => console.debug('[K2:Capacitor]', message),
    warn: (message: string) => console.warn('[K2:Capacitor]', message),
  };

  // Inject globals
  (window as any)._k2 = capacitorK2;
  (window as any)._platform = capacitorPlatform;

  // Register event listeners (polling handles state updates, these just log)
  K2Plugin.addListener('vpnStateChange', (event: any) => {
    console.debug('[K2:Capacitor] vpnStateChange:', event);
  });

  K2Plugin.addListener('vpnError', (event: any) => {
    console.warn('[K2:Capacitor] vpnError:', event);
  });

  console.info(`[K2:Capacitor] Injected - os=${platform}, version=${appVersion}`);
}
