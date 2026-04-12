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

    it('migrates legacy rule.global=true shape to ruleMode=global + modeOverride=manual', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: true }, server: 'k2v5://old' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('global');
      expect(state.modeOverride).toBe('manual');
      // Migration writes the new shape back, stripping server/rule and adding modeOverride.
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'global', modeOverride: 'manual' },
      );
    });

    it('migrates legacy rule.global=false shape to ruleMode=chnroute + modeOverride=manual', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: false } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('chnroute');
      expect(state.modeOverride).toBe('manual');
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'chnroute', modeOverride: 'manual' },
      );
    });

    it('existing users with persisted ruleMode default to modeOverride=manual', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('chnroute');
      expect(state.modeOverride).toBe('manual');
    });

    it('fresh install (null storage) defaults to modeOverride=auto', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.modeOverride).toBe('auto');
      expect(state.suggestedProfile).toBeNull();
      expect(state.detectedCountry).toBeNull();
    });

    it('respects persisted modeOverride=auto over legacy ruleMode', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute', modeOverride: 'auto' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.modeOverride).toBe('auto');
    });
  });

  // ==================== updateRuleMode ====================

  describe('updateRuleMode', () => {
    it('updates state, pins modeOverride=manual, and persists', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().updateRuleMode('global');

      const state = useConfigStore.getState();
      expect(state.ruleMode).toBe('global');
      expect(state.modeOverride).toBe('manual');
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        { ruleMode: 'global', modeOverride: 'manual' },
      );
    });
  });

  describe('setDetectedProfile', () => {
    it('caches country + profile in auto mode', async () => {
      mockStorage.get.mockResolvedValue(null); // fresh install → auto

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ country: 'IR', profile: 'iroute' });

      const state = useConfigStore.getState();
      expect(state.detectedCountry).toBe('IR');
      expect(state.suggestedProfile).toBe('iroute');
    });

    it('skips when modeOverride is manual', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'global' }); // legacy → manual

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ country: 'IR', profile: 'iroute' });

      const state = useConfigStore.getState();
      expect(state.detectedCountry).toBeNull();
      expect(state.suggestedProfile).toBeNull();
    });
  });

  describe('resolveProfile', () => {
    it('auto mode with no suggestion → global', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().resolveProfile()).toBe('global');
    });

    it('auto mode with suggestion → suggestion', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ country: 'RU', profile: 'ruroute' });
      expect(useConfigStore.getState().resolveProfile()).toBe('ruroute');
    });

    it('global override ignores suggestion', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ profile: 'iroute' });
      await useConfigStore.getState().updateModeOverride('global');
      expect(useConfigStore.getState().resolveProfile()).toBe('global');
    });

    it('manual override with chnroute → cnroute', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute' });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().resolveProfile()).toBe('cnroute');
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

  // ==================== Phase 1 rule-miss telemetry (dark flag) ====================

  describe('telemetry (rule-miss Phase 1)', () => {
    it('defaults ruleMissEnabled to false', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().telemetry.ruleMissEnabled).toBe(false);
    });

    it('omits the telemetry block from buildConnectConfig when disabled', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      const result = useConfigStore.getState().buildConnectConfig('k2v5://example');
      expect(result.telemetry).toBeUndefined();
    });

    it('emits telemetry.rule_miss when dark flag is flipped', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      // Simulate a Phase 2 UI toggle flipping the dark flag via direct
      // set. Phase 1 has no public action for this — the test reaches
      // into setState deliberately to prove the config assembly works
      // end-to-end once the toggle lands.
      useConfigStore.setState({ telemetry: { ruleMissEnabled: true } });
      const result = useConfigStore.getState().buildConnectConfig('k2v5://example');
      expect(result.telemetry).toEqual({ rule_miss: { enabled: true } });
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
