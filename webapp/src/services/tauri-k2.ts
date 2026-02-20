/**
 * Tauri Desktop Bridge
 *
 * Injects window._k2 (VPN control via IPC) and window._platform (desktop capabilities)
 * when running inside a Tauri v2 desktop shell.
 *
 * Detection: window.__TAURI__ is available when tauri.conf.json has withGlobalTauri: true.
 */

import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import { openUrl } from '@tauri-apps/plugin-opener';
import { writeText, readText } from '@tauri-apps/plugin-clipboard-manager';
import type { IK2Vpn, IPlatform, IUpdater, UpdateInfo, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError, ServiceState } from './vpn-types';
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
        // Daemon handleUp expects params.config + pid for lifecycle monitoring
        let wrappedParams: any = params ?? null;
        if (action === 'up' && params) {
          const pid = await window._platform?.getPid?.();
          wrappedParams = { config: params, ...(pid != null && { pid }) };
        }
        const response = await invoke<ServiceResponse>('daemon_exec', {
          action,
          params: wrappedParams,
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

  // Build updater object implementing IUpdater
  const updaterState: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,

    applyUpdateNow: async (): Promise<void> => {
      await invoke('apply_update_now');
    },

    checkUpdateManual: async (): Promise<string> => {
      updaterState.isChecking = true;
      updaterState.error = null;
      try {
        const result = await invoke<string>('check_update_now');
        updaterState.isChecking = false;
        return result;
      } catch (e) {
        updaterState.isChecking = false;
        updaterState.error = e instanceof Error ? e.message : String(e);
        throw e;
      }
    },

    onUpdateReady: (callback: (info: UpdateInfo) => void): (() => void) => {
      let unlisten: (() => void) | null = null;
      listen<UpdateInfo>('update-ready', (event) => {
        updaterState.isUpdateReady = true;
        updaterState.updateInfo = event.payload;
        callback(event.payload);
      }).then((fn) => {
        unlisten = fn;
      });
      return () => {
        unlisten?.();
      };
    },
  };

  // Initialize updater state from existing Rust state (app may have update ready from startup check)
  try {
    const existingUpdate = await invoke<UpdateInfo | null>('get_update_status');
    if (existingUpdate) {
      updaterState.isUpdateReady = true;
      updaterState.updateInfo = existingUpdate;
    }
  } catch {
    // Updater not available, leave defaults
  }

  const tauriPlatform: IPlatform = {
    os: osMap[platformInfo.os] ?? 'linux',
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
      await openUrl(url);
    },

    writeClipboard: async (text: string): Promise<void> => {
      await writeText(text);
    },

    readClipboard: async (): Promise<string> => {
      return await readText();
    },

    syncLocale: async (locale: string): Promise<void> => {
      await invoke('sync_locale', { locale });
    },

    updater: updaterState,

    reinstallService: async (): Promise<void> => {
      await invoke('admin_reinstall_service');
    },

    getPid: async (): Promise<number> => {
      return await invoke<number>('get_pid');
    },

    uploadLogs: async (params): Promise<{ success: boolean; error?: string }> => {
      return await invoke<{ success: boolean; error?: string }>('upload_service_log_command', { params });
    },
  };

  (window as any)._k2 = tauriK2;
  (window as any)._platform = tauriPlatform;

  console.info(`[K2:Tauri] Injected - os=${tauriPlatform.os}, version=${tauriPlatform.version}`);
}
