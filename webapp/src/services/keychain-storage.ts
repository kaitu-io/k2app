/**
 * Keychain-backed storage (iOS, bridge v6).
 *
 * Wraps the app-private file store (capacitor-storage) and mirrors the
 * identity keys — device udid + auth tokens — into the iOS Keychain, which
 * survives uninstall/reinstall. Every other key goes to the file store only.
 *
 * Invariants:
 * - Read order is Keychain → file. A file hit with an empty Keychain is
 *   written back to the Keychain (migration) — the value is never regenerated.
 * - A Keychain miss, failure or corrupt value falls through to the file store,
 *   so a Keychain problem can never make getDeviceUdid() mint a new identity
 *   while the file still holds one.
 * - Writes and removes hit both stores. The file store is authoritative for
 *   errors (its failures propagate, as before); Keychain failures are logged
 *   and swallowed so a Keychain problem degrades to today's file-only behaviour.
 * - A key whose Keychain write/remove failed may hold a STALE Keychain value
 *   (old refresh token, a token logout meant to delete). Such keys are marked
 *   and served from the file store only, until a later Keychain write or
 *   remove for that key succeeds — otherwise the Keychain-first read would
 *   resurrect the stale value.
 * - Keychain values are JSON strings, the same encoding capacitor-storage uses.
 *
 * Only the Capacitor bridge consumes this module.
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

export const KEYCHAIN_KEYS = ['device-udid', 'k2.auth.token', 'k2.auth.refresh'] as const;

export interface KeychainBackend {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

const KEYCHAIN_KEY_SET: ReadonlySet<string> = new Set(KEYCHAIN_KEYS);

function isIdentityKey(key: string): boolean {
  return KEYCHAIN_KEY_SET.has(key);
}

export function createKeychainBackedStorage(file: ISecureStorage, kc: KeychainBackend): ISecureStorage {
  // Keys whose Keychain copy may be stale (last write/remove failed).
  const kcStale = new Set<string>();

  async function readKeychain<T>(key: string): Promise<T | null> {
    try {
      const raw = await kc.get(key);
      if (raw === null || raw === undefined) return null;
      return JSON.parse(raw) as T;
    } catch (error) {
      console.warn(`[KeychainStorage] Keychain read failed for ${key}, using file store:`, error);
      return null;
    }
  }

  async function writeKeychain(key: string, value: unknown): Promise<void> {
    try {
      await kc.set(key, JSON.stringify(value));
      kcStale.delete(key);
    } catch (error) {
      kcStale.add(key);
      console.warn(`[KeychainStorage] Keychain write failed for ${key}, serving it from file store:`, error);
    }
  }

  async function get<T = any>(key: string): Promise<T | null> {
    if (!isIdentityKey(key)) return file.get<T>(key);

    if (!kcStale.has(key)) {
      const fromKc = await readKeychain<T>(key);
      if (fromKc !== null && fromKc !== undefined) return fromKc;
    }

    const fromFile = await file.get<T>(key);
    if (fromFile !== null && fromFile !== undefined) {
      await writeKeychain(key, fromFile);
    }
    return fromFile;
  }

  return {
    get,

    async set<T = any>(key: string, value: T, options?: StorageOptions): Promise<void> {
      await file.set(key, value, options);
      if (isIdentityKey(key)) await writeKeychain(key, value);
    },

    async remove(key: string): Promise<void> {
      if (!isIdentityKey(key)) return file.remove(key);
      try {
        await kc.remove(key);
        kcStale.delete(key);
      } catch (error) {
        kcStale.add(key);
        console.warn(`[KeychainStorage] Keychain remove failed for ${key}, serving it from file store:`, error);
      }
      await file.remove(key);
    },

    async has(key: string): Promise<boolean> {
      if (!isIdentityKey(key)) return file.has(key);
      return (await get(key)) !== null;
    },

    clear: () => file.clear(),
    keys: () => file.keys(),
  };
}
