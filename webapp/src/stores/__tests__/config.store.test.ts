/**
 * Config Store Unit Tests
 *
 * Tests:
 * - loadConfig from storage (empty + existing)
 * - updateConfig deep merge + persistence
 * - buildConnectConfig merges defaults + stored + server
 * - Getters: ruleMode
 * - initializeAllStores calls loadConfig in correct order
 *
 * Run: yarn test src/stores/__tests__/config.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

/**
 * ClientConfig type definition (mirrors types/client-config.ts)
 * Defined inline so the test file compiles even before production code exists.
 * When production types are created, switch to: import type { ClientConfig } from '../../types/client-config';
 */
interface ClientConfig {
  server?: string;
  mode?: string;
  rule?: { global?: boolean; rule_url?: string; geoip_url?: string; antiporn?: boolean; porn_url?: string; cache_dir?: string };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
}

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
    getUdid: vi.fn().mockResolvedValue('test-udid'),
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
   * Will fail with import error until config.store.ts is created — expected RED phase.
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
    it('loads empty config when storage returns null', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.config).toEqual({});
      expect(state.loaded).toBe(true);
    });

    it('loads existing config from storage', async () => {
      const storedConfig: Partial<ClientConfig> = {
        rule: { global: true },
      };
      mockStorage.get.mockResolvedValue(storedConfig);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.config).toEqual(storedConfig);
      expect(state.config.rule?.global).toBe(true);
      expect(state.loaded).toBe(true);
    });
  });

  // ==================== updateConfig ====================

  describe('updateConfig', () => {
    it('deep merges and persists to storage', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: false } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().updateConfig({ rule: { global: true } });

      const state = useConfigStore.getState();
      expect(state.config).toEqual({
        rule: { global: true },
      });

      // Verify persistence — storage.set called with storage key + merged config
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          rule: { global: true },
        }),
      );
    });

    it('nested merge preserves sibling fields', async () => {
      mockStorage.get.mockResolvedValue({
        rule: { global: true },
        mode: 'tun',
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().updateConfig({ rule: { global: false } });

      const state = useConfigStore.getState();
      expect(state.config.rule?.global).toBe(false);
      expect(state.config.mode).toBe('tun');
    });
  });

  // ==================== buildConnectConfig ====================

  describe('buildConnectConfig', () => {
    it('merges defaults + stored config + serverUrl', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: true } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig('k2v5://example');

      expect(result.server).toBe('k2v5://example');
      expect(result.rule?.global).toBe(true);
      expect(result.mode).toBe('tun');
      expect(result.log?.level).toBe('info');
    });

    it('uses stored server when no serverUrl argument provided', async () => {
      mockStorage.get.mockResolvedValue({
        server: 'k2v5://saved',
        rule: { global: false },
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig();

      expect(result.server).toBe('k2v5://saved');
    });
  });

  // ==================== Getters ====================

  describe('Getters', () => {
    it('ruleMode returns chnroute by default (empty config)', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().ruleMode).toBe('chnroute');
    });

    it('ruleMode returns global when rule.global is true', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: true } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

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
