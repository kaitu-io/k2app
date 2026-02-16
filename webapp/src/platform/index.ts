import type { PlatformApi } from './types';
import { TauriPlatform } from './tauri';
import { CapacitorPlatform } from './capacitor';
import { WebPlatform } from './web';

let _instance: PlatformApi | null = null;

export function createPlatform(override?: PlatformApi): PlatformApi {
  if (override) {
    return override;
  }
  if (typeof window !== 'undefined' && (window as any).__TAURI__) {
    return new TauriPlatform();
  }
  if (typeof window !== 'undefined' && (window as any).Capacitor) {
    return new CapacitorPlatform();
  }
  return new WebPlatform();
}

export function getPlatform(): PlatformApi {
  if (!_instance) {
    _instance = createPlatform();
  }
  return _instance;
}

export function resetPlatform(): void {
  _instance = null;
}

export type { PlatformApi } from './types';
