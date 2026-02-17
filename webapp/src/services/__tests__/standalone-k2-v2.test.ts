/**
 * Standalone K2 V2 Tests — new split-global injection behavior
 *
 * AFTER the split, ensureK2Injected() should inject:
 *   window._k2      = { run(action, params) }     (VPN-only)
 *   window._platform = { os, isDesktop, ..., storage, getUdid }
 *
 * These tests verify the new injection behavior.
 * They should FAIL until standalone-k2.ts is updated.
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

describe('Standalone K2 V2 — split global injection', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', localStorageMock);
    vi.stubGlobal('crypto', mockCrypto);
    localStorageMock.clear();
    vi.clearAllMocks();
    vi.resetModules();

    // Clear any existing globals
    delete (window as any)._k2;
    delete (window as any)._platform;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete (window as any)._k2;
    delete (window as any)._platform;
  });

  it('should inject both window._k2 and window._platform after ensureK2Injected()', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // AFTER the split, both globals should be defined
    expect(window._k2).toBeDefined();
    // This will FAIL because current ensureK2Injected() only sets window._k2
    // and does NOT set window._platform as a separate global
    expect((window as any)._platform).toBeDefined();
  });

  it('should have window._k2 without an api property', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // After the split, _k2 is VPN-only — no `api` property
    // Current code sets _k2.api, so this will FAIL
    expect(window._k2).toBeDefined();
    expect('api' in window._k2).toBe(false);
  });

  it('should have window._k2 without a platform property', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // After the split, _k2 is VPN-only — no `platform` property
    // Current code sets _k2.platform, so this will FAIL
    expect(window._k2).toBeDefined();
    expect('platform' in window._k2).toBe(false);
  });

  it('should have window._platform.storage defined', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // After the split, platform capabilities live on window._platform
    // Current code doesn't set window._platform, so this will FAIL
    const platform = (window as any)._platform;
    expect(platform).toBeDefined();
    expect(platform.storage).toBeDefined();
    expect(typeof platform.storage.get).toBe('function');
    expect(typeof platform.storage.set).toBe('function');
    expect(typeof platform.storage.remove).toBe('function');
  });

  it('should have window._platform.getUdid as a function', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // After the split, getUdid lives on window._platform
    // Current code doesn't set window._platform, so this will FAIL
    const platform = (window as any)._platform;
    expect(platform).toBeDefined();
    expect(typeof platform.getUdid).toBe('function');
  });

  it('should have window._k2.run as a function (new VPN command method)', async () => {
    const { ensureK2Injected } = await import('../standalone-k2');

    ensureK2Injected();

    // After the split, _k2 should have `run` (not `core.exec`)
    // Current code has _k2.core.exec, so this will FAIL
    expect(typeof (window._k2 as any).run).toBe('function');
  });
});
