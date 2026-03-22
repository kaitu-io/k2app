/**
 * Self-Hosted Store Unit Tests
 *
 * Tests:
 * - parseK2v5Uri: valid URIs, invalid URIs, edge cases
 * - maskUriToken: token masking for display
 * - loadTunnel: empty + existing storage
 * - saveTunnel: validation + persistence
 * - clearTunnel: removal from storage
 *
 * Run: yarn test src/stores/__tests__/self-hosted.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { parseK2v5Uri, maskUriToken } from '../self-hosted.store';

// ==================== Mock window._platform ====================

const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  clear: vi.fn(),
  keys: vi.fn(),
};

beforeEach(() => {
  (window as any)._platform = {
    os: 'macos' as const,
    version: '0.4.0',
    storage: mockStorage,
  };
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as any)._platform;
});

// ==================== parseK2v5Uri ====================

describe('parseK2v5Uri', () => {
  it('parses valid k2v5 URI with all fields', () => {
    const uri = 'k2v5://alice:token123@1.2.3.4:443?ech=abc&pin=def&country=JP#tokyo-server';
    const result = parseK2v5Uri(uri);

    expect(result.error).toBeUndefined();
    expect(result.tunnel).toBeDefined();
    expect(result.tunnel!.uri).toBe(uri);
    expect(result.tunnel!.name).toBe('tokyo-server');
    expect(result.tunnel!.country).toBe('JP');
  });

  it('uses hostname as name when no fragment', () => {
    const uri = 'k2v5://alice:token123@1.2.3.4:443?ech=abc';
    const result = parseK2v5Uri(uri);

    expect(result.tunnel!.name).toBe('1.2.3.4');
    expect(result.tunnel!.country).toBeUndefined();
  });

  it('decodes URI-encoded fragment', () => {
    const uri = 'k2v5://alice:token123@1.2.3.4:443#my%20server';
    const result = parseK2v5Uri(uri);

    expect(result.tunnel!.name).toBe('my server');
  });

  it('handles URI without country param', () => {
    const uri = 'k2v5://alice:token123@1.2.3.4:443?ech=abc#tokyo';
    const result = parseK2v5Uri(uri);

    expect(result.tunnel!.country).toBeUndefined();
  });

  it('trims whitespace from URI', () => {
    const uri = '  k2v5://alice:token123@1.2.3.4:443  ';
    const result = parseK2v5Uri(uri);

    expect(result.error).toBeUndefined();
    expect(result.tunnel!.uri).toBe(uri.trim());
  });

  it('rejects non-k2v5 protocol', () => {
    expect(parseK2v5Uri('https://example.com').error).toBe('invalidUri');
    expect(parseK2v5Uri('k2wss://example.com').error).toBe('invalidUri');
    expect(parseK2v5Uri('plain text').error).toBe('invalidUri');
    expect(parseK2v5Uri('').error).toBe('invalidUri');
  });

  it('rejects URI without credentials', () => {
    const uri = 'k2v5://1.2.3.4:443?ech=abc';
    const result = parseK2v5Uri(uri);

    expect(result.error).toBe('invalidUriNoAuth');
  });

  it('accepts URI with only username (no password)', () => {
    // k2v5://user@host has @ but no password — technically has userinfo
    const uri = 'k2v5://alice@1.2.3.4:443';
    const result = parseK2v5Uri(uri);

    // Has @ so passes auth check, URL parses fine
    expect(result.error).toBeUndefined();
    expect(result.tunnel).toBeDefined();
  });

  it('handles hop parameter without conflict', () => {
    const uri = 'k2v5://alice:token@1.2.3.4:443?hop=50000-50100&country=JP#tokyo';
    const result = parseK2v5Uri(uri);

    expect(result.tunnel!.country).toBe('JP');
    expect(result.tunnel!.name).toBe('tokyo');
  });
});

// ==================== maskUriToken ====================

describe('maskUriToken', () => {
  it('masks long token to first 4 chars + ***', () => {
    const uri = 'k2v5://alice:a3f8b2c1d4e5f6a7@1.2.3.4:443';
    const masked = maskUriToken(uri);

    expect(masked).toBe('k2v5://alice:a3f8***@1.2.3.4:443');
    expect(masked).not.toContain('a3f8b2c1d4e5f6a7');
  });

  it('masks short token to ***', () => {
    const uri = 'k2v5://alice:abc@1.2.3.4:443';
    const masked = maskUriToken(uri);

    expect(masked).toBe('k2v5://alice:***@1.2.3.4:443');
  });

  it('returns URI as-is when no password', () => {
    const uri = 'k2v5://alice@1.2.3.4:443';
    const masked = maskUriToken(uri);

    expect(masked).toBe(uri);
  });

  it('returns malformed URI as-is', () => {
    const uri = 'not-a-uri';
    const masked = maskUriToken(uri);

    expect(masked).toBe(uri);
  });
});

// ==================== Store ====================

describe('SelfHostedStore', () => {
  const getStore = async () => {
    const mod = await import('../self-hosted.store');
    return mod.useSelfHostedStore;
  };

  beforeEach(() => {
    vi.resetModules();
    mockStorage.get.mockReset();
    mockStorage.set.mockReset();
    mockStorage.remove.mockReset();
  });

  // ==================== loadTunnel ====================

  describe('loadTunnel', () => {
    it('loads null tunnel when storage is empty', async () => {
      mockStorage.get.mockResolvedValue(null);
      const store = await getStore();

      await store.getState().loadTunnel();

      expect(store.getState().tunnel).toBeNull();
      expect(store.getState().loaded).toBe(true);
      expect(mockStorage.get).toHaveBeenCalledWith('k2.self_hosted.tunnel');
    });

    it('loads existing tunnel from storage', async () => {
      const saved = {
        uri: 'k2v5://alice:token@1.2.3.4:443#tokyo',
        name: 'tokyo',
        country: 'JP',
      };
      mockStorage.get.mockResolvedValue(saved);
      const store = await getStore();

      await store.getState().loadTunnel();

      expect(store.getState().tunnel).toEqual(saved);
      expect(store.getState().loaded).toBe(true);
    });

    it('handles storage read failure gracefully', async () => {
      mockStorage.get.mockRejectedValue(new Error('storage corrupt'));
      const store = await getStore();

      await store.getState().loadTunnel();

      expect(store.getState().tunnel).toBeNull();
      expect(store.getState().loaded).toBe(true);
    });
  });

  // ==================== saveTunnel ====================

  describe('saveTunnel', () => {
    it('saves valid URI and updates state', async () => {
      mockStorage.set.mockResolvedValue(undefined);
      const store = await getStore();

      await store.getState().saveTunnel('k2v5://alice:token@1.2.3.4:443?country=JP#tokyo');

      const tunnel = store.getState().tunnel;
      expect(tunnel).not.toBeNull();
      expect(tunnel!.name).toBe('tokyo');
      expect(tunnel!.country).toBe('JP');
      expect(mockStorage.set).toHaveBeenCalledWith(
        'k2.self_hosted.tunnel',
        expect.objectContaining({ name: 'tokyo', country: 'JP' }),
      );
    });

    it('throws on invalid URI (wrong protocol)', async () => {
      const store = await getStore();

      await expect(store.getState().saveTunnel('https://example.com'))
        .rejects.toThrow('invalidUri');

      expect(store.getState().tunnel).toBeNull();
      expect(mockStorage.set).not.toHaveBeenCalled();
    });

    it('throws on URI without credentials', async () => {
      const store = await getStore();

      await expect(store.getState().saveTunnel('k2v5://1.2.3.4:443'))
        .rejects.toThrow('invalidUriNoAuth');
    });

    it('handles storage write failure silently (state still updated)', async () => {
      mockStorage.set.mockRejectedValue(new Error('write failed'));
      const store = await getStore();

      // Should not throw — state updates, persistence is best-effort
      await store.getState().saveTunnel('k2v5://alice:token@1.2.3.4:443#test');

      expect(store.getState().tunnel).not.toBeNull();
      expect(store.getState().tunnel!.name).toBe('test');
    });
  });

  // ==================== clearTunnel ====================

  describe('clearTunnel', () => {
    it('clears tunnel from state and storage', async () => {
      mockStorage.get.mockResolvedValue({
        uri: 'k2v5://alice:token@1.2.3.4:443',
        name: '1.2.3.4',
      });
      mockStorage.remove.mockResolvedValue(undefined);
      const store = await getStore();

      await store.getState().loadTunnel();
      expect(store.getState().tunnel).not.toBeNull();

      await store.getState().clearTunnel();

      expect(store.getState().tunnel).toBeNull();
      expect(mockStorage.remove).toHaveBeenCalledWith('k2.self_hosted.tunnel');
    });

    it('handles storage remove failure silently', async () => {
      mockStorage.remove.mockRejectedValue(new Error('remove failed'));
      const store = await getStore();

      await store.getState().clearTunnel();

      expect(store.getState().tunnel).toBeNull();
    });
  });
});
