import { HttpVpnClient } from './http-client';
import type { VpnClient } from './types';

let instance: VpnClient | null = null;

function isCapacitorNative(): boolean {
  return typeof (window as any)?.Capacitor?.isNativePlatform === 'function'
    && (window as any).Capacitor.isNativePlatform();
}

export function createVpnClient(override?: VpnClient): VpnClient {
  if (override) {
    instance = override;
    return override;
  }
  if (!instance) {
    if (isCapacitorNative()) {
      throw new Error('Use initVpnClient() for native platform');
    }
    instance = new HttpVpnClient();
  }
  return instance;
}

export async function initVpnClient(override?: VpnClient): Promise<VpnClient> {
  if (override) {
    instance = override;
    return override;
  }
  if (!instance) {
    if (isCapacitorNative()) {
      const { NativeVpnClient } = await import('./native-client');
      // Use variable to prevent Vite from statically resolving this mobile-only dependency
      const pluginModule = 'k2-plugin';
      const { K2Plugin } = await import(/* @vite-ignore */ pluginModule);
      instance = new NativeVpnClient(K2Plugin);
    } else {
      instance = new HttpVpnClient();
    }
  }
  return instance;
}

export function getVpnClient(): VpnClient {
  if (!instance) {
    throw new Error('VpnClient not initialized');
  }
  return instance;
}

export function resetVpnClient(): void {
  instance = null;
}

export type { VpnClient, VpnState, VpnStatus, VpnEvent, ReadyState, VersionInfo, VpnConfig, UpdateCheckResult } from './types';
