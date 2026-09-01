/**
 * config.store — brand routing-scope behavior (Stage 1: Kaitu = China-only).
 *
 * Brand is baked at build time (vitest defines __K2_BRAND__ from env K2_BRAND),
 * so `MULTI_COUNTRY` below follows the build exactly like production. Each brand
 * asserts its own behavior via describe.runIf, so the suite stays green under
 * BOTH `vitest run` (kaitu) and `K2_BRAND=overleap vitest run`.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../services/cloud-api', () => ({ cloudApi: { get: vi.fn() } }));

import { useConfigStore } from './config.store';
import { getCurrentAppConfig } from '../config/apps';
import { cloudApi } from '../services/cloud-api';

const MULTI_COUNTRY = getCurrentAppConfig().features.multiCountryRouting === true;
const cloudGet = vi.mocked(cloudApi.get);
const STORAGE_KEY = 'k2.vpn.config';

function installStorageMock(): Map<string, unknown> {
  const m = new Map<string, unknown>();
  (window as any)._platform = {
    storage: {
      get: async (k: string) => (m.has(k) ? m.get(k) : null),
      set: async (k: string, v: unknown) => { m.set(k, v); },
      remove: async (k: string) => { m.delete(k); },
      has: async (k: string) => m.has(k),
      clear: async () => { m.clear(); },
      keys: async () => [...m.keys()],
    },
  };
  return m;
}

function resetStore() {
  useConfigStore.setState(
    {
      defaultVia: 'proxy', countryVia: 'direct', country: null, autoDetect: true,
      alwaysOn: false, detectedCountry: null, suggestedProfile: null, loaded: false,
    },
    false,
  );
}

describe('config.store — brand routing scope', () => {
  let store: Map<string, unknown>;
  beforeEach(() => {
    store = installStorageMock();
    resetStore();
    cloudGet.mockReset();
    cloudGet.mockResolvedValue({ code: 0, data: { country: 'jp', profile: 'jproute' } } as never);
  });

  describe.runIf(!MULTI_COUNTRY)('cn-fixed brand (Kaitu)', () => {
    it('loadConfig pins region to cn, ignoring a stale non-cn persisted country', async () => {
      store.set(STORAGE_KEY, { defaultVia: 'proxy', countryVia: 'direct', country: 'us', autoDetect: true, alwaysOn: false });
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().country).toBe('cn');
    });

    it('detectGeo is a no-op — no detection, no network', async () => {
      const tzSpy = vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
        .mockReturnValue({ timeZone: 'Europe/Istanbul' } as Intl.ResolvedDateTimeFormatOptions);
      await useConfigStore.getState().detectGeo();
      expect(useConfigStore.getState().detectedCountry).toBeNull();
      expect(cloudGet).not.toHaveBeenCalled();
      tzSpy.mockRestore();
    });
  });

  describe.runIf(MULTI_COUNTRY)('multi-country brand (Overleap)', () => {
    it('detectGeo detects locally from the system timezone — never a network call', async () => {
      const tzSpy = vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
        .mockReturnValue({ timeZone: 'Europe/Istanbul' } as Intl.ResolvedDateTimeFormatOptions);
      await useConfigStore.getState().detectGeo();
      const s = useConfigStore.getState();
      expect(s.detectedCountry).toBe('tr');
      expect(s.country).toBe('tr'); // autoDetect on → synced
      expect(cloudGet).not.toHaveBeenCalled();
      tzSpy.mockRestore();
    });

    it('detectGeo clamps a bundle-less country to cn for routing, keeps raw detection for display', async () => {
      const tzSpy = vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
        .mockReturnValue({ timeZone: 'Asia/Tokyo' } as Intl.ResolvedDateTimeFormatOptions);
      await useConfigStore.getState().detectGeo();
      const s = useConfigStore.getState();
      expect(s.detectedCountry).toBe('jp'); // raw — travel banner material
      expect(s.country).toBe('cn');         // clamped — jp has no rule bundles
      tzSpy.mockRestore();
    });

    it('detectGeo with autoDetect off and a country already set only fills the cache', async () => {
      useConfigStore.setState({ autoDetect: false, country: 'ru' });
      const tzSpy = vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
        .mockReturnValue({ timeZone: 'Europe/Istanbul' } as Intl.ResolvedDateTimeFormatOptions);
      await useConfigStore.getState().detectGeo();
      const s = useConfigStore.getState();
      expect(s.detectedCountry).toBe('tr');
      expect(s.country).toBe('ru'); // manual choice untouched
      tzSpy.mockRestore();
    });

    it('detectGeo falls back to cn when the runtime has no timezone', async () => {
      const tzSpy = vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
        .mockImplementation(() => { throw new Error('no ICU'); });
      await useConfigStore.getState().detectGeo();
      const s = useConfigStore.getState();
      expect(s.detectedCountry).toBe('cn');
      expect(s.country).toBe('cn');
      tzSpy.mockRestore();
    });

    it('loadConfig preserves the persisted country', async () => {
      store.set(STORAGE_KEY, { defaultVia: 'proxy', countryVia: 'direct', country: 'us', autoDetect: false, alwaysOn: false });
      await useConfigStore.getState().loadConfig();
      expect(useConfigStore.getState().country).toBe('us');
    });
  });
});
