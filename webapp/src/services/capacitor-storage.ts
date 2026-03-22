/**
 * Capacitor native storage adapter.
 *
 * Delegates to K2Plugin native methods:
 * - iOS: App-private JSON file (Library/Application Support/k2storage.json)
 * - Android: SharedPreferences("k2_storage")
 *
 * Both backends exclude data from iCloud/Auto Backup to prevent
 * UDID collision when restoring to a new device.
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

interface K2PluginStorage {
  storageGet(options: { key: string }): Promise<{ value: string | null }>;
  storageSet(options: { key: string; value: string }): Promise<void>;
  storageRemove(options: { key: string }): Promise<void>;
}

export function createCapacitorStorage(plugin: K2PluginStorage): ISecureStorage {
  return {
    async get<T = any>(key: string): Promise<T | null> {
      const result = await plugin.storageGet({ key });
      if (result.value === null || result.value === undefined) return null;
      try {
        return JSON.parse(result.value) as T;
      } catch {
        await plugin.storageRemove({ key });
        return null;
      }
    },

    async set<T = any>(key: string, value: T, _options?: StorageOptions): Promise<void> {
      await plugin.storageSet({ key, value: JSON.stringify(value) });
    },

    async remove(key: string): Promise<void> {
      await plugin.storageRemove({ key });
    },

    async has(key: string): Promise<boolean> {
      const result = await plugin.storageGet({ key });
      return result.value !== null && result.value !== undefined;
    },

    async clear(): Promise<void> {
      // Unused — no-op
    },

    async keys(): Promise<string[]> {
      // Unused
      return [];
    },
  };
}
