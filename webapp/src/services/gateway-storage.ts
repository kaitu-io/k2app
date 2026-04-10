/**
 * Gateway Storage — server-side encrypted storage via HTTP API.
 *
 * Used when webapp runs on a k2r gateway. All data is stored on the
 * gateway device at /etc/k2r/storage.json, encrypted with AES-256-GCM.
 * Browser localStorage is NOT used — multiple devices access the same gateway.
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

const STORAGE_ENDPOINT = '/api/storage';

async function storageRequest(action: string, key?: string, value?: any): Promise<any> {
  try {
    const body: Record<string, any> = { action };
    if (key !== undefined) body.key = key;
    if (value !== undefined) body.value = value;

    const resp = await fetch(STORAGE_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) return null;
    const result = await resp.json();
    return result.code === 0 ? result.data : null;
  } catch {
    return null;
  }
}

export const gatewayStorage: ISecureStorage = {
  async get<T = any>(key: string): Promise<T | null> {
    const data = await storageRequest('get', key);
    return data ?? null;
  },

  async set<T = any>(key: string, value: T, _options?: StorageOptions): Promise<void> {
    await storageRequest('set', key, value);
  },

  async remove(key: string): Promise<void> {
    await storageRequest('remove', key);
  },

  async has(key: string): Promise<boolean> {
    const result = await storageRequest('has', key);
    return result === true;
  },

  async clear(): Promise<void> {
    await storageRequest('clear');
  },

  async keys(): Promise<string[]> {
    const result = await storageRequest('keys');
    return Array.isArray(result) ? result : [];
  },
};
