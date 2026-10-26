/**
 * Keychain-backed storage (iOS bridge v6): identity keys (device udid + auth
 * tokens) are mirrored into the Keychain so they survive uninstall/reinstall.
 * The file store stays the fallback — a Keychain miss or failure must never
 * cause the identity to be regenerated while the file still holds it.
 */
import { describe, it, expect, vi } from 'vitest';
import type { ISecureStorage } from '../../types/kaitu-core';
import { createKeychainBackedStorage, KEYCHAIN_KEYS } from '../keychain-storage';

function memFile(init: Record<string, unknown> = {}): ISecureStorage & { data: Map<string, unknown> } {
  const data = new Map<string, unknown>(Object.entries(init));
  return {
    data,
    get: vi.fn(async (k: string) => (data.has(k) ? (data.get(k) as any) : null)),
    set: vi.fn(async (k: string, v: unknown) => { data.set(k, v); }),
    remove: vi.fn(async (k: string) => { data.delete(k); }),
    has: vi.fn(async (k: string) => data.has(k)),
    clear: vi.fn(async () => { data.clear(); }),
    keys: vi.fn(async () => [...data.keys()]),
  } as any;
}

function memKc(init: Record<string, string> = {}) {
  const data = new Map<string, string>(Object.entries(init));
  return {
    data,
    get: vi.fn(async (k: string) => data.get(k) ?? null),
    set: vi.fn(async (k: string, v: string) => { data.set(k, v); }),
    remove: vi.fn(async (k: string) => { data.delete(k); }),
  };
}

describe('keychain-storage', () => {
  it('KEYCHAIN_KEYS are the real identity storage keys', () => {
    expect([...KEYCHAIN_KEYS]).toEqual(['device-udid', 'k2.auth.token', 'k2.auth.refresh']);
  });

  it('migrates file udid into keychain without regenerating', async () => {
    const file = memFile({ 'device-udid': 'raw-1' });
    const kc = memKc();
    const s = createKeychainBackedStorage(file, kc);

    expect(await s.get('device-udid')).toBe('raw-1');
    expect(kc.data.get('device-udid')).toBe(JSON.stringify('raw-1'));
    expect(file.set).not.toHaveBeenCalled();
  });

  it('reads keychain when file is empty (reinstall)', async () => {
    const file = memFile();
    const kc = memKc({ 'device-udid': JSON.stringify('raw-kc'), 'k2.auth.token': JSON.stringify('tok') });
    const s = createKeychainBackedStorage(file, kc);

    expect(await s.get('device-udid')).toBe('raw-kc');
    expect(await s.get('k2.auth.token')).toBe('tok');
    expect(await s.has('device-udid')).toBe(true);
  });

  it('writes identity keys to both stores', async () => {
    const file = memFile();
    const kc = memKc();
    const s = createKeychainBackedStorage(file, kc);

    await s.set('k2.auth.refresh', 'r-1');
    expect(file.data.get('k2.auth.refresh')).toBe('r-1');
    expect(kc.data.get('k2.auth.refresh')).toBe(JSON.stringify('r-1'));
  });

  it('remove clears both', async () => {
    const file = memFile({ 'k2.auth.token': 'tok' });
    const kc = memKc({ 'k2.auth.token': JSON.stringify('tok') });
    const s = createKeychainBackedStorage(file, kc);

    await s.remove('k2.auth.token');
    expect(file.data.has('k2.auth.token')).toBe(false);
    expect(kc.data.has('k2.auth.token')).toBe(false);
    expect(await s.get('k2.auth.token')).toBeNull();
  });

  it('non-identity keys never touch keychain', async () => {
    const file = memFile();
    const kc = memKc();
    const s = createKeychainBackedStorage(file, kc);

    await s.set('k2.vpn.config', { a: 1 });
    expect(await s.get('k2.vpn.config')).toEqual({ a: 1 });
    await s.has('k2.vpn.config');
    await s.remove('k2.vpn.config');
    expect(kc.get).toHaveBeenCalledTimes(0);
    expect(kc.set).toHaveBeenCalledTimes(0);
    expect(kc.remove).toHaveBeenCalledTimes(0);
  });

  it('keychain read failure falls back to file', async () => {
    const file = memFile({ 'device-udid': 'raw-file' });
    const kc = memKc();
    kc.get.mockRejectedValue(new Error('errSecInteractionNotAllowed'));
    const s = createKeychainBackedStorage(file, kc);

    expect(await s.get('device-udid')).toBe('raw-file');
  });

  it('keychain write failure does not fail set (file is authoritative fallback)', async () => {
    const file = memFile();
    const kc = memKc();
    kc.set.mockRejectedValue(new Error('kc down'));
    const s = createKeychainBackedStorage(file, kc);

    await expect(s.set('device-udid', 'raw-2')).resolves.toBeUndefined();
    expect(file.data.get('device-udid')).toBe('raw-2');
  });

  it('keychain remove failure still clears file (logout really logs out locally)', async () => {
    const file = memFile({ 'k2.auth.token': 'tok' });
    const kc = memKc();
    kc.remove.mockRejectedValue(new Error('kc down'));
    const s = createKeychainBackedStorage(file, kc);

    await s.remove('k2.auth.token');
    expect(file.data.has('k2.auth.token')).toBe(false);
  });

  it('corrupt keychain value falls back to file', async () => {
    const file = memFile({ 'device-udid': 'raw-file' });
    const kc = memKc({ 'device-udid': '{not json' });
    const s = createKeychainBackedStorage(file, kc);

    expect(await s.get('device-udid')).toBe('raw-file');
  });
});
