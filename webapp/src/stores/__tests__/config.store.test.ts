/**
 * Config Store Unit Tests (v3 — defaultVia/countryVia/country/autoDetect)
 *
 * Tests:
 * - loadConfig from storage (empty, v3 shape, v2/v1/v0 legacy migration)
 * - setPreset / setAutoDetect / setCountry persistence
 * - setDetectedProfile syncs country when autoDetect=true
 * - resolvePreset derives correct preset for all combos
 * - buildConnectConfig produces correct routes
 * - telemetry dark flag
 * - initializeAllStores calls loadConfig
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
    it('fresh install defaults to proxy + direct bypass + CN + autoDetect', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBe('direct');
      expect(state.autoDetect).toBe(true);
      expect(state.country).toBe('cn');
      expect(state.loaded).toBe(true);
    });

    it('loads v3 shape from storage', async () => {
      mockStorage.get.mockResolvedValue({
        defaultVia: 'direct',
        countryVia: 'k2p',
        country: 'ru',
        autoDetect: false,
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('direct');
      expect(state.countryVia).toBe('k2p');
      expect(state.autoDetect).toBe(false);
      expect(state.country).toBe('ru');
    });

    it('migrates v2 routingMode=global to proxy + null countryVia', async () => {
      mockStorage.get.mockResolvedValue({
        routingMode: 'global',
        autoDetect: true,
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBeNull();
      expect(state.autoDetect).toBe(true);
      expect(mockStorage.set).toHaveBeenCalled();
    });

    it('migrates v2 routingMode=split + selectedCountry to bypass', async () => {
      mockStorage.get.mockResolvedValue({
        routingMode: 'split',
        autoDetect: false,
        selectedCountry: 'cn',
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBe('direct');
      expect(state.autoDetect).toBe(false);
      expect(state.country).toBe('cn');
    });

    it('migrates v1 modeOverride=auto to bypass + autoDetect', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute', modeOverride: 'auto' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBe('direct');
      expect(state.autoDetect).toBe(true);
      expect(mockStorage.set).toHaveBeenCalled();
    });

    it('migrates v1 modeOverride=global to global', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute', modeOverride: 'global' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBeNull();
      expect(useConfigStore.getState().autoDetect).toBe(true);
    });

    it('migrates v1 modeOverride=manual + chnroute to bypass + cn', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute', modeOverride: 'manual' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBe('direct');
      expect(state.autoDetect).toBe(false);
      expect(state.country).toBe('cn');
    });

    it('migrates v1 modeOverride=manual + global to global', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'global', modeOverride: 'manual' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBeNull();
    });

    it('migrates v0 rule.global=true to global', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: true }, server: 'k2v5://old' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBeNull();
      expect(mockStorage.set).toHaveBeenCalled();
    });

    it('migrates v0 rule.global=false to bypass + cn', async () => {
      mockStorage.get.mockResolvedValue({ rule: { global: false } });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const state = useConfigStore.getState();
      expect(state.defaultVia).toBe('proxy');
      expect(state.countryVia).toBe('direct');
      expect(state.autoDetect).toBe(false);
      expect(state.country).toBe('cn');
    });

    it('migrates v0 ruleMode=chnroute (no modeOverride) to bypass + cn', async () => {
      mockStorage.get.mockResolvedValue({ ruleMode: 'chnroute' });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      // v0 ruleMode without modeOverride falls through to the rule/ruleMode check
      // which sees ruleMode=chnroute as non-global → bypass + cn
      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBe('direct');
    });
  });

  // ==================== setPreset ====================

  describe('setPreset', () => {
    it('global preset sets countryVia=null', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().setPreset('global');

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBeNull();
      expect(mockStorage.set).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ defaultVia: 'proxy', countryVia: null }),
      );
    });

    it('bypass preset sets proxy + direct', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().setPreset('bypass');

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBe('direct');
    });

    it('home preset sets direct + k2p', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().setPreset('home');

      expect(useConfigStore.getState().defaultVia).toBe('direct');
      expect(useConfigStore.getState().countryVia).toBe('k2p');
    });

    it('home_proxy preset sets proxy + k2p', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().setPreset('home_proxy');

      expect(useConfigStore.getState().defaultVia).toBe('proxy');
      expect(useConfigStore.getState().countryVia).toBe('k2p');
    });
  });

  // ==================== setCountry ====================

  describe('setCountry', () => {
    it('sets country and turns off autoDetect', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      await useConfigStore.getState().setCountry('RU');

      const state = useConfigStore.getState();
      expect(state.country).toBe('ru');
      expect(state.autoDetect).toBe(false);
    });
  });

  // ==================== setAutoDetect ====================

  describe('setAutoDetect', () => {
    it('turning on restores detectedCountry into country', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      // Simulate Center detection
      useConfigStore.getState().setDetectedProfile({ country: 'IR' });
      // User manually picks a different country
      await useConfigStore.getState().setCountry('RU');
      expect(useConfigStore.getState().autoDetect).toBe(false);

      // Turn auto-detect back on
      await useConfigStore.getState().setAutoDetect(true);
      expect(useConfigStore.getState().autoDetect).toBe(true);
      expect(useConfigStore.getState().country).toBe('ir');
    });
  });

  // ==================== setDetectedProfile ====================

  describe('setDetectedProfile', () => {
    it('syncs country when autoDetect is on', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ country: 'IR', profile: 'iroute' });

      const state = useConfigStore.getState();
      expect(state.detectedCountry).toBe('ir');
      expect(state.suggestedProfile).toBe('iroute');
      expect(state.country).toBe('ir');
    });

    it('does not sync country when autoDetect is off', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      // Manually select a country (turns off autoDetect)
      await useConfigStore.getState().setCountry('CN');

      useConfigStore.getState().setDetectedProfile({ country: 'IR', profile: 'iroute' });

      const state = useConfigStore.getState();
      expect(state.detectedCountry).toBe('ir');
      expect(state.country).toBe('cn'); // unchanged
    });
  });

  // ==================== resolvePreset ====================

  describe('resolvePreset', () => {
    it('proxy + null countryVia → global', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: null, autoDetect: true });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().resolvePreset()).toBe('global');
    });

    it('proxy + direct countryVia → bypass', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().resolvePreset()).toBe('bypass');
    });

    it('direct + k2p countryVia → home', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'direct', countryVia: 'k2p', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().resolvePreset()).toBe('home');
    });

    it('proxy + k2p countryVia → home_proxy', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: 'k2p', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().resolvePreset()).toBe('home_proxy');
    });
  });

  // ==================== buildConnectConfig ====================

  describe('buildConnectConfig', () => {
    it('global preset emits single all-match route', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: null, autoDetect: true });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.mode).toBe('tun');
      expect(result.routes).toEqual([
        { via: 'k2v5://example', match: { all: true } },
      ]);
      expect((result as any).server).toBeUndefined();
    });

    it('bypass preset with cn emits cn-access direct + proxy fallback', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.routes).toEqual([
        { via: 'direct', match: { preset: 'cn-access' } },
        { via: 'k2v5://example', match: {} },
      ]);
    });

    it('bypass preset with ir emits ir-access direct + proxy fallback', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      useConfigStore.getState().setDetectedProfile({ country: 'IR', profile: 'iroute' });

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.routes).toEqual([
        { via: 'direct', match: { preset: 'ir-access' } },
        { via: 'k2v5://example', match: {} },
      ]);
    });

    it('home preset with cn emits cn-access k2p + direct fallback', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'direct', countryVia: 'k2p', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.routes).toEqual([
        { via: 'k2p://home', match: { preset: 'cn-access' } },
        { via: 'direct', match: {} },
      ]);
    });

    it('home_proxy preset with cn emits cn-access k2p + proxy fallback', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: 'k2p', country: 'cn', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.routes).toEqual([
        { via: 'k2p://home', match: { preset: 'cn-access' } },
        { via: 'k2v5://example', match: {} },
      ]);
    });

    it('legacy string argument still works', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: null, autoDetect: true });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig('k2v5://legacy');
      expect(result.routes?.[result.routes.length - 1]?.via).toBe('k2v5://legacy');
    });

    it('gateway platform prepends ipinfo.io direct route', async () => {
      (window as any)._platform.platformType = 'gateway';
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: null, autoDetect: true });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://gw' });

      expect(result.routes?.[0]).toEqual({
        via: 'direct',
        match: { domain_suffix: ['ipinfo.io'] },
      });
    });

    it('without serverUrl returns empty routes', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig();
      expect(result.routes).toEqual([]);
    });

    it('unknown country falls back to global routes', async () => {
      mockStorage.get.mockResolvedValue({ defaultVia: 'proxy', countryVia: 'direct', country: 'xx', autoDetect: false });
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      const result = useConfigStore.getState().buildConnectConfig({ serverUrl: 'k2v5://example' });

      expect(result.routes).toEqual([
        { via: 'k2v5://example', match: { all: true } },
      ]);
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

  // ==================== Phase 1 rule-miss telemetry ====================

  describe('telemetry (rule-miss Phase 1)', () => {
    it('defaults ruleMissEnabled to false', async () => {
      mockStorage.get.mockResolvedValue(null);
      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().telemetry.ruleMissEnabled).toBe(false);
    });

    it('omits telemetry from buildConnectConfig when disabled', async () => {
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
      useConfigStore.setState({ telemetry: { ruleMissEnabled: true } });
      const result = useConfigStore.getState().buildConnectConfig('k2v5://example');
      expect(result.telemetry).toEqual({ rule_miss: { enabled: true } });
    });
  });

  // ==================== initializeAllStores integration ====================

  describe('initializeAllStores integration', () => {
    it('calls configStore.loadConfig during initialization', async () => {
      mockStorage.get.mockResolvedValue(null);
      const configMod = await import('../config.store');
      const loadConfigSpy = vi.spyOn(
        configMod.useConfigStore.getState(),
        'loadConfig',
      );
      const { initializeAllStores } = await import('../index');

      const cleanup = initializeAllStores();
      expect(loadConfigSpy).toHaveBeenCalled();

      cleanup();
      loadConfigSpy.mockRestore();
    });
  });

  // ==================== alwaysOn (iOS NEOnDemandRule opt-in) ====================

  describe('alwaysOn', () => {
    it('defaults to false on fresh install', async () => {
      mockStorage.get.mockResolvedValue(null);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().alwaysOn).toBe(false);
    });

    it('reads alwaysOn=true from stored v3 config', async () => {
      mockStorage.get.mockResolvedValue({
        defaultVia: 'proxy',
        countryVia: 'direct',
        country: 'cn',
        autoDetect: true,
        alwaysOn: true,
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().alwaysOn).toBe(true);
    });

    it('defaults alwaysOn to false when v3 config has no alwaysOn field', async () => {
      mockStorage.get.mockResolvedValue({
        defaultVia: 'proxy',
        countryVia: 'direct',
        country: 'cn',
        autoDetect: true,
      });

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();

      expect(useConfigStore.getState().alwaysOn).toBe(false);
    });

    it('setAlwaysOn updates state and persists', async () => {
      mockStorage.get.mockResolvedValue(null);
      mockStorage.set.mockResolvedValue(undefined);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      await useConfigStore.getState().setAlwaysOn(true);

      expect(useConfigStore.getState().alwaysOn).toBe(true);
      expect(mockStorage.set).toHaveBeenLastCalledWith(
        'k2.vpn.config',
        expect.objectContaining({ alwaysOn: true }),
      );
    });

    it('setAlwaysOn(false) persists false', async () => {
      mockStorage.get.mockResolvedValue({
        defaultVia: 'proxy',
        countryVia: 'direct',
        country: 'cn',
        autoDetect: true,
        alwaysOn: true,
      });
      mockStorage.set.mockResolvedValue(undefined);

      const useConfigStore = await getStore();
      await useConfigStore.getState().loadConfig();
      await useConfigStore.getState().setAlwaysOn(false);

      expect(useConfigStore.getState().alwaysOn).toBe(false);
      expect(mockStorage.set).toHaveBeenLastCalledWith(
        'k2.vpn.config',
        expect.objectContaining({ alwaysOn: false }),
      );
    });
  });

  describe('buildBypassRoutes (auto-detected merge)', () => {
    beforeEach(() => {
      (window as any)._platform = { os: 'android', isDesktop: false, isMobile: true };
    });

    it('returns [] when both user and auto are empty', async () => {
      const { buildBypassRoutes } = await import('../config.store');
      expect(buildBypassRoutes([])).toEqual([]);
      expect(buildBypassRoutes([], [])).toEqual([]);
    });

    it('user-only emits a package_name route', async () => {
      const { buildBypassRoutes } = await import('../config.store');
      const entries = [
        { id: 'com.foo', label: 'Foo', kind: 'package' as const, names: ['com.foo'], addedAt: 0 },
      ];
      expect(buildBypassRoutes(entries)).toEqual([
        { via: 'direct', match: { package_name: ['com.foo'] } },
      ]);
    });

    it('auto-only emits a package_name route', async () => {
      const { buildBypassRoutes } = await import('../config.store');
      expect(buildBypassRoutes([], ['com.tencent.mm', 'cmb.pb'])).toEqual([
        { via: 'direct', match: { package_name: ['com.tencent.mm', 'cmb.pb'] } },
      ]);
    });

    it('unions user + auto with dedupe', async () => {
      const { buildBypassRoutes } = await import('../config.store');
      const entries = [
        { id: 'cmb.pb', label: '招行', kind: 'package' as const, names: ['cmb.pb'], addedAt: 0 },
      ];
      const routes = buildBypassRoutes(entries, ['com.tencent.mm', 'cmb.pb']);
      expect(routes).toHaveLength(1);
      // Set semantics: cmb.pb appears once
      expect(routes[0].match.package_name).toEqual(['cmb.pb', 'com.tencent.mm']);
    });

    it('returns [] on iOS regardless of inputs', async () => {
      (window as any)._platform.os = 'ios';
      const { buildBypassRoutes } = await import('../config.store');
      expect(buildBypassRoutes([], ['com.tencent.mm'])).toEqual([]);
    });
  });
});
