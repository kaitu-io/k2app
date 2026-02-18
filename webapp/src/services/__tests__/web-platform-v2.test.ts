/**
 * Web Platform V2 Tests — cleaned interface (no UDID, no dead methods)
 *
 * Verifies:
 * - webPlatform has only the methods defined in IPlatform v3
 * - Dead methods (isDesktop, isMobile, debug, warn, nativeExec, etc.) removed
 * - Required methods (openExternal, writeClipboard, readClipboard, syncLocale) present
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

  it('webPlatform has correct platform identity', async () => {
    const { webPlatform } = await import('../web-platform');
    expect(webPlatform.os).toBe('web');
    expect(webPlatform.version).toBe('0.0.0');
  });

  it('webPlatform has storage', async () => {
    const { webPlatform } = await import('../web-platform');
    expect(webPlatform.storage).toBeDefined();
    expect(typeof webPlatform.storage.get).toBe('function');
    expect(typeof webPlatform.storage.set).toBe('function');
    expect(typeof webPlatform.storage.remove).toBe('function');
  });

  it('webPlatform has required cross-platform methods', async () => {
    const { webPlatform } = await import('../web-platform');
    expect(typeof webPlatform.openExternal).toBe('function');
    expect(typeof webPlatform.writeClipboard).toBe('function');
    expect(typeof webPlatform.readClipboard).toBe('function');
    expect(typeof webPlatform.syncLocale).toBe('function');
  });

  it('webPlatform does NOT have deleted methods', async () => {
    const { webPlatform } = await import('../web-platform');
    expect('isDesktop' in webPlatform).toBe(false);
    expect('isMobile' in webPlatform).toBe(false);
    expect('debug' in webPlatform).toBe(false);
    expect('warn' in webPlatform).toBe(false);
    expect('showToast' in webPlatform).toBe(false);
    expect('nativeExec' in webPlatform).toBe(false);
    expect('getLocale' in webPlatform).toBe(false);
    expect('exit' in webPlatform).toBe(false);
  });

  it('webPlatform does NOT have getUdid (comes from daemon/native)', async () => {
    const { webPlatform } = await import('../web-platform');
    expect('getUdid' in webPlatform).toBe(false);
  });

  it('should NOT export getWebUdid function', async () => {
    const mod = await import('../web-platform');
    expect('getWebUdid' in mod).toBe(false);
  });

  it('should NOT export getWebFingerprint function', async () => {
    const mod = await import('../web-platform');
    expect('getWebFingerprint' in mod).toBe(false);
  });
});
