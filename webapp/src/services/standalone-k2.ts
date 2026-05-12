/**
 * Standalone K2 Implementation
 *
 * Provides default window._k2 (VPN-only) and window._platform for standalone/router mode.
 * Uses relative fetch() to connect to cmd/k2 HTTP server.
 *
 * After the split:
 *   window._k2      = { run(action, params) }           (pure VPN)
 *   window._platform = { os, storage, ... }              (platform capabilities)
 */

declare const __K2_BUILD_COMMIT__: string;

import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import { plainLocalStorage } from './plain-storage';
import { webPlatform } from './web-platform';

const CORE_ENDPOINT = '/api/core';
const HELPER_ENDPOINT = '/api/helper';

async function coreExec<T = any>(action: string, params?: any): Promise<SResponse<T>> {
  try {
    // Route adb-* actions to the helper endpoint
    const endpoint = action.startsWith('adb-') ? HELPER_ENDPOINT : CORE_ENDPOINT;
    const response = await fetch(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params: params ?? {} }),
    });

    if (!response.ok) {
      return {
        code: -1,
        message: 'Service error',
      };
    }

    return await response.json();
  } catch {
    return {
      code: -1,
      message: 'Service unavailable',
    };
  }
}

/**
 * Standalone K2 — VPN-only global (window._k2)
 */
export const standaloneK2: IK2Vpn = {
  run: coreExec,
};

/**
 * Standalone Platform — platform capabilities global (window._platform)
 */
export const standalonePlatform: IPlatform = {
  ...webPlatform,
  os: 'web',
  platformType: 'web',
  version: 'standalone',
  arch: 'unknown',
  commit: typeof __K2_BUILD_COMMIT__ !== 'undefined' ? __K2_BUILD_COMMIT__ : '',
  storage: plainLocalStorage,
  setDevEnabled: () => {},

  // App list — only exposed when running on Linux desktop (cmd/k2 with embedded
  // webapp). Web standalone has no Go daemon to answer /api/helper, so leave
  // appList undefined and the Dashboard bypass entry won't render.
  //
  // Gated on window._platform?.platformType at construction time: if a host
  // page set platformType='desktop' on window._platform before this module
  // ran, we expose appList; otherwise (pure web standalone, where _platform
  // is set BY this module) it stays undefined.
  appList: window._platform?.platformType === 'desktop' ? {
    listRunning: async () => {
      const resp = await fetch(HELPER_ENDPOINT, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'app-list-running' }),
      }).then(r => r.json());
      if (resp.code !== 0) {
        throw new Error(resp.message || 'app-list-running failed');
      }
      // Remap snake_case → camelCase per project convention (Go json.Marshal
      // emits snake_case; webapp consumes camelCase).
      return (resp.data?.apps ?? []).map((a: any) => ({
        id: a.id,
        label: a.label,
        processNames: a.process_names ?? [],
        iconUrl: undefined, // Linux desktop has no icons in v1; Avatar falls back to first letter
      }));
    },
  } : undefined,
};

export function isK2Injected(): boolean {
  return typeof window._k2 !== 'undefined';
}

export function isPlatformInjected(): boolean {
  return typeof window._platform !== 'undefined';
}

export function getK2Source(): 'tauri' | 'capacitor' | 'gateway' | 'standalone' | 'none' {
  if (!isK2Injected()) {
    return 'none';
  }

  if (!isPlatformInjected()) {
    return 'standalone';
  }

  const platform = window._platform;

  // Gateway (k2r) — must come before tauri check: gateway uses os='linux' which
  // would otherwise be misclassified as desktop tauri.
  if (platform.platformType === 'gateway') {
    return 'gateway';
  }

  // Tauri: desktop OS + non-standalone version
  if (['macos', 'windows', 'linux'].includes(platform.os) && platform.version !== 'standalone') {
    return 'tauri';
  }

  // Capacitor: mobile OS
  if (['ios', 'android'].includes(platform.os)) {
    return 'capacitor';
  }

  return 'standalone';
}

export function ensureK2Injected(): void {
  if (!isK2Injected()) {
    console.info('[K2:Standalone] Injecting standalone K2 implementation...');
    (window as any)._k2 = standaloneK2;
    console.info('[K2:Standalone] Standalone K2 injected (VPN-only)');
  }

  if (!isPlatformInjected()) {
    console.info('[K2:Standalone] Injecting standalone platform...');
    (window as any)._platform = standalonePlatform;
    console.info('[K2:Standalone] Standalone platform injected');
  }

  const source = getK2Source();
  console.info('[K2:Standalone] Using K2 implementation:', source);
}
