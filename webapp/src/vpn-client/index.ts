import { HttpVpnClient } from './http-client';
import type { VpnClient } from './types';

let instance: VpnClient | null = null;

export function createVpnClient(override?: VpnClient): VpnClient {
  if (override) {
    instance = override;
    return override;
  }
  if (!instance) {
    instance = new HttpVpnClient();
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

export type { VpnClient, VpnState, VpnStatus, VpnEvent, ReadyState, VersionInfo, VpnConfig } from './types';
