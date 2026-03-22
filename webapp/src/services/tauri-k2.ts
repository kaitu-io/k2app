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
import { getDeviceUdid } from './device-udid';
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
  if (raw.connected_at) {
    startAt = Math.floor(new Date(raw.connected_at).getTime() / 1000);
  }

  console.debug('[K2:Tauri] transformStatus: raw.state=' + (raw.state ?? 'undefined') + ' → state=' + state + ', error=' + (error?.code ?? 'none') + ', retrying=' + retrying);

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
 * Inject Tauri-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectTauriGlobals(): Promise<void> {
  const platformInfo = await invoke<{ os: string; version: string; arch: string; commit?: string }>('get_platform_info');

  const osMap: Record<string, IPlatform['os']> = {
    macos: 'macos',
    windows: 'windows',
    linux: 'linux',
  };

  const tauriK2: IK2Vpn = {
    run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
      console.debug('[K2:Tauri] run: action=' + action);
      try {
        // Daemon handleUp expects params.config + pid for lifecycle monitoring
        let wrappedParams: any = params ?? null;
        if (action === 'up' && params) {
          const pid = await window._platform?.getPid?.();
          wrappedParams = { config: params, ...(pid != null && { pid }) };
        }
        // Route adb-* actions to the helper daemon endpoint
        const command = action.startsWith('adb-') ? 'daemon_helper_exec' : 'daemon_exec';
        const response = await invoke<ServiceResponse>(command, {
          action,
          params: wrappedParams,
        });
        console.debug('[K2:Tauri] run: action=' + action + ' → code=' + response.code);
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
        console.warn('[K2:Tauri] run: action=' + action + ' → error:', error);
        return {
          code: -1,
          message: error instanceof Error ? error.message : String(error),
        };
      }
    },

    onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
      let unlisten: (() => void) | null = null;
      listen<{ available: boolean }>('service-state-changed', (event) => {
        callback(event.payload.available);
      }).then((fn) => {
        unlisten = fn;
      });
      return () => {
        unlisten?.();
      };
    },

    onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
      let unlisten: (() => void) | null = null;
      listen<any>('vpn-status-changed', (event) => {
        console.debug('[K2:Tauri] vpn-status-changed event received');
        callback(transformStatus(event.payload));
      }).then((fn) => {
        unlisten = fn;
      });
      return () => {
        unlisten?.();
      };
    },
  };

  // Fetch initial channel from Rust
  let initialChannel: 'stable' | 'beta' = 'stable';
  try {
    const ch = await invoke<string>('get_update_channel');
    initialChannel = ch === 'beta' ? 'beta' : 'stable';
  } catch {
    // Default to stable if command not available
  }

  // Build updater object implementing IUpdater
  const updaterState: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,
    channel: initialChannel,

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

    setChannel: async (channel: 'stable' | 'beta'): Promise<string> => {
      const result = await invoke<{ channel: string; logLevel: string }>('set_update_channel', {
        channel,
        currentLogLevel: localStorage.getItem('k2_log_level') || 'info',
      });
      updaterState.channel = result.channel === 'beta' ? 'beta' : 'stable';
      localStorage.setItem('k2_log_level', result.logLevel);
      return result.channel;
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
    arch: platformInfo.arch,
    commit: platformInfo.commit || '',

    storage: webSecureStorage,

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

    uploadLogs: async (params): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }> => {
      const udid = await getDeviceUdid();
      return await invoke<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>('upload_service_log_command', { params, udid });
    },

    setLogLevel: (level: string): void => {
      const effectiveLevel = updaterState.channel === 'beta' ? 'debug' : level;
      localStorage.setItem('k2_log_level', effectiveLevel);
      invoke('set_log_level', { level: effectiveLevel }).catch(() => {});
    },

    setDevEnabled: (enabled: boolean): void => {
      invoke('set_dev_enabled', { enabled }).catch(() => {});
    },
  };

  (window as any)._k2 = tauriK2;
  (window as any)._platform = tauriPlatform;

  console.info(`[K2:Tauri] Injected - os=${tauriPlatform.os}, version=${tauriPlatform.version}`);

  // Auto-restore dev mode from previous session
  if (localStorage.getItem('k2_developer_mode') === 'true') {
    invoke('set_dev_enabled', { enabled: true }).catch(() => {});
  }

  // Forward WebView console.* to Tauri log system (desktop.log)
  // JS console.info("msg") → pluginLog.info("msg") → IPC → Rust log::info! → desktop.log
  try {
    const pluginLog = await import('@tauri-apps/plugin-log');

    function formatArgs(args: any[]): string {
      return args.map(a => {
        if (typeof a === 'string') return a;
        if (a instanceof Error) return `${a.message}${a.stack ? '\n' + a.stack : ''}`;
        try { return JSON.stringify(a); } catch { return String(a); }
      }).join(' ');
    }

    const _log = console.log;
    const _debug = console.debug;
    const _info = console.info;
    const _warn = console.warn;
    const _error = console.error;

    console.log = (...args: any[]) => { _log(...args); pluginLog.debug(formatArgs(args)).catch(() => {}); };
    console.debug = (...args: any[]) => { _debug(...args); pluginLog.debug(formatArgs(args)).catch(() => {}); };
    console.info = (...args: any[]) => { _info(...args); pluginLog.info(formatArgs(args)).catch(() => {}); };
    console.warn = (...args: any[]) => { _warn(...args); pluginLog.warn(formatArgs(args)).catch(() => {}); };
    console.error = (...args: any[]) => { _error(...args); pluginLog.error(formatArgs(args)).catch(() => {}); };
  } catch {
    // plugin-log not available (non-Tauri env), skip
  }

  // Show window after frontend is fully initialized
  // This prevents size flashing on Windows
  try {
    await invoke('show_window');
  } catch (error) {
    console.warn('[K2:Tauri] Failed to show window:', error);
  }
}
