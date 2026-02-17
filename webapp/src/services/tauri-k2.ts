/**
 * Tauri Desktop Bridge
 *
 * Injects window._k2 (VPN control via IPC) and window._platform (desktop capabilities)
 * when running inside a Tauri v2 desktop shell.
 *
 * Detection: window.__TAURI__ is available when tauri.conf.json has withGlobalTauri: true.
 */

import { invoke } from '@tauri-apps/api/core';
import { fetch as tauriFetch } from '@tauri-apps/plugin-http';
import { open as shellOpen } from '@tauri-apps/plugin-shell';
import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError, ServiceState } from './control-types';
import { webSecureStorage } from './secure-storage';

interface ServiceResponse {
  code: number;
  message: string;
  data: any;
}

/**
 * Transform raw daemon status into normalized StatusResponseData.
 * Daemon uses "stopped" instead of "disconnected" and snake_case keys.
 * Error synthesis: disconnected + error -> error state.
 */
function transformStatus(raw: any): StatusResponseData {
  let state: ServiceState = raw.state === 'stopped' ? 'disconnected' : (raw.state ?? 'disconnected');
  const running = state === 'connecting' || state === 'connected';

  let error: ControlError | undefined;
  if (raw.error) {
    error = { code: 570, message: raw.error };
    if (state === 'disconnected') {
      state = 'error';
    }
  }

  let startAt: number | undefined;
  if (raw.connected_at) {
    startAt = Math.floor(new Date(raw.connected_at).getTime() / 1000);
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
 * Inject Tauri-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectTauriGlobals(): Promise<void> {
  const platformInfo = await invoke<{ os: string; version: string }>('get_platform_info');

  const osMap: Record<string, IPlatform['os']> = {
    macos: 'macos',
    windows: 'windows',
    linux: 'linux',
  };

  const tauriK2: IK2Vpn = {
    run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
      try {
        const response = await invoke<ServiceResponse>('daemon_exec', {
          action,
          params: params ?? null,
        });
        // Transform status response to normalize daemon state values
        if (action === 'status' && response.code === 0 && response.data) {
          const transformed = transformStatus(response.data);
          return {
            code: response.code,
            message: response.message,
            data: transformed as unknown as T,
          };
        }
        return {
          code: response.code,
          message: response.message,
          data: response.data as T,
        };
      } catch (error) {
        return {
          code: -1,
          message: error instanceof Error ? error.message : String(error),
        };
      }
    },
  };

  const tauriPlatform: IPlatform = {
    os: osMap[platformInfo.os] ?? 'linux',
    isDesktop: true,
    isMobile: false,
    version: platformInfo.version,

    storage: webSecureStorage,

    getUdid: async (): Promise<string> => {
      const response = await invoke<ServiceResponse>('get_udid');
      if (response.code === 0 && response.data?.udid) {
        return response.data.udid;
      }
      throw new Error('Failed to get UDID from daemon');
    },

    openExternal: async (url: string): Promise<void> => {
      await shellOpen(url);
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

    nativeExec: async <T = any>(action: string, params?: Record<string, any>): Promise<T> => {
      return invoke<T>(action, params ?? {});
    },

    debug: (message: string) => console.debug('[K2:Tauri]', message),
    warn: (message: string) => console.warn('[K2:Tauri]', message),
  };

  (window as any)._k2 = tauriK2;
  (window as any)._platform = tauriPlatform;

  patchFetchForTauri();

  console.info(`[K2:Tauri] Injected - os=${tauriPlatform.os}, version=${tauriPlatform.version}`);
}

/**
 * Patch window.fetch so external HTTPS requests go through the Tauri HTTP plugin
 * (bypasses WebKit cross-origin restrictions). Local requests use native fetch.
 */
function patchFetchForTauri(): void {
  const nativeFetch = window.fetch.bind(window);

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string'
      ? input
      : input instanceof URL
        ? input.href
        : input.url;

    if (
      url.startsWith('/') ||
      url.startsWith('http://127.0.0.1') ||
      url.startsWith('http://localhost') ||
      url.startsWith('tauri:') ||
      url.startsWith('data:') ||
      url.startsWith('blob:')
    ) {
      return nativeFetch(input, init);
    }

    return tauriFetch(url, init);
  };
}
