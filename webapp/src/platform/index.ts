import type { PlatformApi } from './types';

let _instance: PlatformApi | null = null;

export function createPlatform(_override?: PlatformApi): PlatformApi {
  throw new Error('Not implemented');
}

export function getPlatform(): PlatformApi {
  throw new Error('Not implemented');
}

export function resetPlatform(): void {
  _instance = null;
}

export type { PlatformApi } from './types';
