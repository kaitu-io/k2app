/**
 * Config Store Unit Tests
 *
 * Tests:
 * - loadConfig from storage (empty, new shape, legacy shape migration)
 * - updateRuleMode persistence
 * - buildConnectConfig produces the correct routes shape for global/chnroute
 * - initializeAllStores calls loadConfig in correct order
 *
 * Run: yarn test src/stores/__tests__/config.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

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
  // Install window._platform with mock storage
  (window as any)._platform = {
    os: 'macos' as const,
    isDesktop: true,
    isMobile: false,
    version: '0.4.0',
    storage: mockStorage,
  };
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as any)._platform;
});

// ==================== Tests ====================

describe('Config Store', () => {
  /**
   * Dynamic import so each test gets a fresh module (vi.resetModules).
   */
  const getStore = async () => {
    const mod = await import('../config.store');
    return mod.useConfigStore;
  };

  beforeEach(() => {
    vi.resetModules();
    mockStorage.get.mockReset();
    mockStorage.set.mockReset();
  });

  // ==================== loadConfig ====================

  describe('loadConfig', () => {
    it('defaults to chnroute when storage returns null', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('chnroute');
      expect(state.loaded).toBe(true);
    });

    it('loads ruleMode from new-shape storage', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'global' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('global');
      expect(state.loaded).toBe(true);
    });

    it('migrates legacy rule.global=true shape to ruleMode=global', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: true }, server: 'k2v5://old' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('global');
      // Migration writes the new shape back, stripping server/rule.
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'global' },
      );
    });

    it('migrates legacy rule.global=false shape to ruleMode=chnroute', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: false } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().ruleMode).toBe('chnroute');
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'chnroute' },
      );
    });
  });

  // ==================== updateRuleMode ====================

  describe('updateRuleMode', () => {
    it('updates state and persists to storage', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().updateRuleMode('global');

      expect(useConfigStore.getState().ruleMode).toBe('global');
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'global' },
      );
    });
  });

  // ==================== buildConnectConfig ====================

  describe('buildConnectConfig', () => {
    it('global mode emits a single all-match k2v5 route', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'global' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.mode).toBe('tun');
      expect(result.routes).toEqual([
        { via: 'k2v5://example', match: { all: true } },
      ]);
      // Wire contract must not carry a top-level `server` field anymore.
      expect((result as any).server).toBeUndefined();
    });

    it('chnroute mode emits cn-direct + k2v5-fallback routes', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.mode).toBe('tun');
      expect(result.routes).toEqual([
        { via: 'direct', match: { preset: 'cn-access' } },
        { via: 'k2v5://example', match: {} },
      ]);
    });

    it('legacy string argument still works', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'global' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig('k2v5://legacy');

      expect(result.routes?.[result.routes.length - 1]?.via).toBe('k2v5://legacy');
    });

    it('gateway platform prepends ipinfo.io direct route', async () => {
      (window as any)._platform.platformType = 'gateway';
      mockStorage.get.mockResolvedValue({ ruleMode: 'global' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://gw' });

      expect(result.routes?.[0]).toEqual({
        via: 'direct',
        match: { domain_suffix: ['ipinfo.io'] },
      });
      expect(result.routes?.[1]).toEqual({
        via: 'k2v5://gw',
        match: { all: true },
      });
    });

    it('without a serverUrl returns only the gateway prefix (or empty) routes', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig();

      expect(result.routes).toEqual([]);
    });
  });

  // ==================== buildConnectConfig log level ====================

  describe('buildConnectConfig log level', () => {
    it('always uses the build-time log level', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig('k2v5://example');
      expect(result.log?.level).toBe('debug');
    });
  });

  // ==================== Getters ====================

  describe('Getters', () => {
    it('ruleMode defaults to chnroute', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().ruleMode).toBe('chnroute');
    });

    it('ruleMode returns global after updateRuleMode', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      await useConfigStore.getState().updateRuleMode('global');

      expect(useConfigStore.getState().ruleMode).toBe('global');
    });
  });

  // ==================== initializeAllStores integration ====================

  describe('initializeAllStores integration', () => {
    it('calls configStore.loadConfig during initialization', async () => {
      mockStorage.get.mockResolvedValue(null);

      // Import configStore to spy on loadConfig
      const configMod = await import('../config.store');
      const loadConfigSpy = vi.spyOn(
        configMod.useConfigStore.getState(),
        'loadConfig',
      );

      // Import initializeAllStores (which should call loadConfig)
      const { initializeAllStores } = await import('../index');

      const cleanup = initializeAllStores();

      expect(loadConfigSpy).toHaveBeenCalled();

      cleanup();
      loadConfigSpy.mockRestore();
    });
  });
});
