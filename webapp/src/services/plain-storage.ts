/**
 * Plain localStorage storage — no encryption.
 *
 * Used by Standalone (dev/router) and Web platforms.
 * Tauri and Capacitor use native storage instead.
 *
 * Key prefix `_k2_` separates from old encrypted `_k2_secure_` keys.
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

const PREFIX = '_k2_';

export const plainLocalStorage: ISecureStorage = {
  async get<T = any>(key: string): Promise<T | null> {
    const raw = localStorage.getItem(PREFIX + key);
    if (raw === null) return null;
    try {
      return JSON.parse(raw) as T;
    } catch {
      localStorage.removeItem(PREFIX + key);
      return null;
    }
  },

  async set<T = any>(key: string, value: T, _options?: StorageOptions): Promise<void> {
    localStorage.setItem(PREFIX + key, JSON.stringify(value));
  },

  async remove(key: string): Promise<void> {
    localStorage.removeItem(PREFIX + key);
  },

  async has(key: string): Promise<boolean> {
    return localStorage.getItem(PREFIX + key) !== null;
  },

  async clear(): Promise<void> {
    const toRemove: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k?.startsWith(PREFIX)) toRemove.push(k);
    }
    toRemove.forEach(k => localStorage.removeItem(k));
  },

  async keys(): Promise<string[]> {
    const result: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k?.startsWith(PREFIX)) result.push(k.slice(PREFIX.length));
    }
    return result;
  },
};
