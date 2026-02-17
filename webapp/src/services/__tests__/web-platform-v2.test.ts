/**
 * Web Platform V2 Tests — cleaned web-platform (no UDID functions)
 *
 * AFTER the split:
 *   - webPlatform should NOT have getUdid (UDID comes from daemon/native, not web)
 *   - webPlatform should still have storage and os
 *   - getWebUdid and getWebFingerprint should be removed from the module
 *
 * These tests verify the cleaned web-platform contract.
 * They should FAIL until web-platform.ts is updated.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock localStorage for secure-storage dependency
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] || null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      store = {};
    }),
    key: vi.fn((index: number) => Object.keys(store)[index] || null),
    get length() {
      return Object.keys(store).length;
    },
  };
})();

// Mock crypto for web secure storage
const mockCrypto = {
  getRandomValues: vi.fn((array: Uint8Array) => {
    for (let i = 0; i < array.length; i++) {
      array[i] = Math.floor(Math.random() * 256);
    }
    return array;
  }),
  subtle: {
    digest: vi.fn(async (_algorithm: string, data: ArrayBuffer) => {
      const result = new Uint8Array(32);
      const input = new Uint8Array(data);
      for (let i = 0; i < Math.min(input.length, 32); i++) {
        result[i] = input[i];
      }
      return result.buffer;
    }),
    importKey: vi.fn(async () => ({ type: 'secret' })),
    encrypt: vi.fn(async (_algorithm: any, _key: any, data: ArrayBuffer) => {
      const input = new Uint8Array(data);
      const result = new Uint8Array(input.length + 16);
      result.set(input);
      return result.buffer;
    }),
    decrypt: vi.fn(async (_algorithm: any, _key: any, data: ArrayBuffer) => {
      const input = new Uint8Array(data);
      return input.slice(0, input.length - 16).buffer;
    }),
  },
};

describe('Web Platform V2 — cleaned interface', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', localStorageMock);
    vi.stubGlobal('crypto', mockCrypto);
    localStorageMock.clear();
    vi.clearAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('webPlatform should NOT have getUdid property', async () => {
    const { webPlatform } = await import('../web-platform');

    // After the cleanup, webPlatform should NOT provide getUdid.
    // UDID generation belongs to daemon/native layer, not the web fallback.
    // Current code has `getUdid: getWebUdid`, so this will FAIL.
    expect('getUdid' in webPlatform).toBe(false);
  });

  it('webPlatform should have storage', async () => {
    const { webPlatform } = await import('../web-platform');

    // Storage should remain on webPlatform
    expect(webPlatform.storage).toBeDefined();
    expect(typeof webPlatform.storage.get).toBe('function');
    expect(typeof webPlatform.storage.set).toBe('function');
    expect(typeof webPlatform.storage.remove).toBe('function');
  });

  it('webPlatform.os should be web', async () => {
    const { webPlatform } = await import('../web-platform');

    expect(webPlatform.os).toBe('web');
  });

  it('should NOT export getWebUdid function', async () => {
    // After cleanup, getWebUdid should be removed from the module entirely
    const mod = await import('../web-platform');

    // Current code exports getWebUdid, so this will FAIL
    expect('getWebUdid' in mod).toBe(false);
  });

  it('should NOT export getWebFingerprint function', async () => {
    // After cleanup, getWebFingerprint should be removed from the module
    const mod = await import('../web-platform');

    // Current code exports getWebFingerprint, so this will FAIL
    expect('getWebFingerprint' in mod).toBe(false);
  });
});
