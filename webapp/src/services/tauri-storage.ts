/**
 * Tauri native storage adapter.
 *
 * Delegates to Rust-side storage.rs which persists a JSON file
 * in the app data directory ({app_data_dir}/storage.json).
 */

import { invoke } from '@tauri-apps/api/core';
import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

export const tauriNativeStorage: ISecureStorage = {
  async get<T = any>(key: string): Promise<T | null> {
    const raw = await invoke<string | null>('storage_get', { key });
    if (raw === null) return null;
    try {
      return JSON.parse(raw) as T;
    } catch {
      await invoke('storage_remove', { key });
      return null;
    }
  },

  async set<T = any>(key: string, value: T, _options?: StorageOptions): Promise<void> {
    await invoke('storage_set', { key, value: JSON.stringify(value) });
  },

  async remove(key: string): Promise<void> {
    await invoke('storage_remove', { key });
  },

  async has(key: string): Promise<boolean> {
    const raw = await invoke<string | null>('storage_get', { key });
    return raw !== null;
  },

  async clear(): Promise<void> {
    // Unused — no-op
  },

  async keys(): Promise<string[]> {
    // Unused
    return [];
  },
};
