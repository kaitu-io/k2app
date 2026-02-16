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
      const { registerPlugin } = await import('@capacitor/core');
      const K2Plugin = registerPlugin('K2Plugin');
      instance = new NativeVpnClient(K2Plugin as any);
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

export type { VpnClient, VpnState, VpnStatus, VpnEvent, ReadyState, VersionInfo, ClientConfig, UpdateCheckResult } from './types';
