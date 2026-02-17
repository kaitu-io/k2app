/**
 * Standalone K2 Implementation
 *
 * Provides default window._k2 (VPN-only) and window._platform for standalone/router mode.
 * Uses relative fetch() to connect to cmd/k2 HTTP server.
 *
 * After the split:
 *   window._k2      = { run(action, params) }           (pure VPN)
 *   window._platform = { os, storage, getUdid, ... }    (platform capabilities)
 */

import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import { webSecureStorage } from './secure-storage';
import { webPlatform } from './web-platform';

const CORE_ENDPOINT = '/core';

async function coreExec<T = any>(action: string, params?: any): Promise<SResponse<T>> {
  try {
    const response = await fetch(CORE_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params: params ?? {} }),
    });

    if (!response.ok) {
      return {
        code: -1,
        message: `HTTP ${response.status}: ${response.statusText}`,
      };
    }

    return await response.json();
  } catch (error) {
    return {
      code: -1,
      message: error instanceof Error ? error.message : 'Network error',
    };
  }
}

/**
 * Get device UDID from the daemon's /api/device/udid endpoint.
 * In standalone/router mode, UDID generation is the daemon's responsibility.
 */
async function getDaemonUdid(): Promise<string> {
  const resp = await fetch('/api/device/udid');
  const json = await resp.json();
  if (json.code === 0 && json.data?.udid) return json.data.udid;
  throw new Error('Failed to get UDID from daemon');
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
  isDesktop: false,
  isMobile: false,
  version: 'standalone',
  getUdid: getDaemonUdid,
  storage: webSecureStorage,
  debug: (message: string) => console.debug('[K2:Standalone]', message),
  warn: (message: string) => console.warn('[K2:Standalone]', message),
};

export function isK2Injected(): boolean {
  return typeof window._k2 !== 'undefined';
}

export function isPlatformInjected(): boolean {
  return typeof window._platform !== 'undefined';
}

export function getK2Source(): 'tauri' | 'capacitor' | 'standalone' | 'none' {
  if (!isK2Injected()) {
    return 'none';
  }

  if (!isPlatformInjected()) {
    return 'standalone';
  }

  const platform = window._platform;

  // Tauri: isDesktop + non-standalone version
  if (platform.isDesktop && platform.version !== 'standalone') {
    return 'tauri';
  }

  // Capacitor: mobile + non-web OS
  if (platform.isMobile && platform.os !== 'web') {
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
