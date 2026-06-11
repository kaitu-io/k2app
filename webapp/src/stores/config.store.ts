/**
 * Config Store v3 - VPN routing configuration
 *
 * Two dimensions determine routing:
 * 1. defaultVia: 'proxy' | 'direct' — where unmatched traffic goes
 * 2. countryVia: 'direct' | 'k2p' | null — where matched country traffic goes (null = global)
 *
 * Plus country selection with optional auto-detection from Center.
 *
 * Preset mapping (UI convenience):
 *   global:     defaultVia='proxy',  countryVia=null
 *   bypass:     defaultVia='proxy',  countryVia='direct'
 *   home:       defaultVia='direct', countryVia='k2p'
 *   home_proxy: defaultVia='proxy',  countryVia='k2p'
 */

import { create } from 'zustand';

import type { ClientConfig, RouteConfig } from '../types/client-config';
import { CLIENT_CONFIG_DEFAULTS } from '../types/client-config';
import { countryToProfile, PROFILE_TO_PRESET } from '../utils/routes';
import { cloudApi } from '../services/cloud-api';
import { controlPlaneHosts } from '../services/antiblock';
/** Build-time log level from K2_BUILD_LOG_LEVEL env var (default: 'debug'). Injected by Vite define. */
declare const __K2_BUILD_LOG_LEVEL__: string;

// ============ Constants ============

const STORAGE_KEY = 'k2.vpn.config';

// ============ Types ============

export type RoutePreset = 'global' | 'bypass' | 'home' | 'home_proxy';

/**
 * Anonymous telemetry sub-state. Phase 1 dark flag.
 */
export interface TelemetryState {
  ruleMissEnabled: boolean;
}

export interface ConnectConfigParams {
  serverUrl?: string;
  forceDirect?: string[];   // Plan C: process names → Tier-1 direct route
  forceProxy?: string[];    // Plan C: process names → Tier-1 proxy route
}

export interface DetectedProfileUpdate {
  country?: string | null;
  profile?: string | null;
}

interface ConfigState {
  /** Where unmatched traffic goes. */
  defaultVia: 'proxy' | 'direct';
  /** Where matched country traffic goes. null = global (no split). */
  countryVia: 'direct' | 'k2p' | null;
  /** Country code for split routing (e.g. 'cn', 'ru'). */
  country: string | null;
  /** Whether Center auto-fills country from IP detection. */
  autoDetect: boolean;
  /**
   * iOS-only: if true, VPN auto-reactivates after system releases the app
   * (jetsam, background kill) via NEOnDemandRuleConnect. Default false.
   * The bridge forwards this to K2Plugin.connect; other platforms ignore it.
   */
  alwaysOn: boolean;
  /** Center-detected country (cached, not persisted). */
  detectedCountry: string | null;
  /** Center-suggested profile name (cached, not persisted). */
  suggestedProfile: string | null;
  telemetry: TelemetryState;
  loaded: boolean;
}

interface ConfigActions {
  loadConfig: () => Promise<void>;
  /** Set routing from a named preset. */
  setPreset: (preset: RoutePreset) => Promise<void>;
  /** Manually set the country code. Turns off autoDetect. */
  setCountry: (cc: string) => Promise<void>;
  /** Toggle auto-detect. When turning on, syncs country from detectedCountry. */
  setAutoDetect: (on: boolean) => Promise<void>;
  /** iOS-only: toggle Always On (NEOnDemandRuleConnect) opt-in. */
  setAlwaysOn: (on: boolean) => Promise<void>;
  /**
   * Cache the country + suggestedProfile from Center user-info endpoint.
   * When autoDetect is on, also syncs country.
   */
  setDetectedProfile: (update: DetectedProfileUpdate) => void;
  /**
   * Fetch country detection from anonymous `GET /api/geo` endpoint.
   * Called on app init. When autoDetect is on, always fetches and syncs
   * country. When autoDetect is off, only fetches once to populate
   * detectedCountry cache (does not change country).
   */
  fetchGeoDetection: () => Promise<void>;
  /** Derive the current RoutePreset from defaultVia + countryVia. */
  resolvePreset: () => RoutePreset;
  buildConnectConfig: (params?: ConnectConfigParams | string) => ClientConfig;
}

// ============ Helpers ============

/** Gateway-only: keep ipinfo.io on a direct route so the router can probe its real egress IP. */
function gatewayPrefix(): RouteConfig[] {
  if (typeof window !== 'undefined' && window._platform?.platformType === 'gateway') {
    return [{ via: 'direct', match: { domain_suffix: ['ipinfo.io'] } }];
  }
  return [];
}

/**
 * Keep the Kaitu control-plane API on a direct route so the app's own requests
 * never egress the tunnel. A tunneled API request carries the exit node's IP, which
 * poisons Center's IP-based geo detection (China user via JP exit → "jp" →
 * match.region=jp → missing jp.krs → 504). Applies on every platform/preset.
 */
function controlPlanePrefix(): RouteConfig[] {
  const hosts = controlPlaneHosts();
  return hosts.length > 0 ? [{ via: 'direct', match: { domain_suffix: hosts } }] : [];
}

function buildRoutes(
  defaultVia: 'proxy' | 'direct',
  countryVia: 'direct' | 'k2p' | null,
  country: string | null,
  serverUrl: string | undefined,
): RouteConfig[] {
  if (!serverUrl) {
    // Defense in depth. connection.store.connect() guards against this with a user-visible
    // error, so reaching this branch means a caller bypassed the guard — log loudly.
    console.error('[ConfigStore] buildRoutes: serverUrl is empty — this should have been caught by connect() guard');
    return gatewayPrefix();
  }

  // Prefix prepended to every mode: gateway egress probe (router only) + control-plane
  // direct route (all platforms). Gateway first so the router's ipinfo.io probe stays routes[0].
  const prefix = [...gatewayPrefix(), ...controlPlanePrefix()];

  // Global: everything through proxy
  if (countryVia === null) {
    return [...prefix, { via: serverUrl, match: { all: true } }];
  }

  // Smart (bypass) mode: emit a region route for direct local traffic.
  if (countryVia === 'direct') {
    if (!country) {
      // No country set — fall back to global shape
      return [...prefix, { via: serverUrl, match: { all: true } }];
    }
    const routes: RouteConfig[] = [
      ...prefix,
      { match: { region: country }, via: 'direct' },
    ];
    // CN-only: drop connections to Tencent's overseas ASN (the tencent-overseas
    // rule-set = AS132203) so WeChat/Tencent HTTPDNS apps — which reach overseas
    // Tencent PoPs as bare IPs, bypassing our DNS layer — fail over to mainland
    // endpoints, which the region:cn route above routes direct. Ordered AFTER
    // region:cn so the HTTPDNS anchor PoPs merged into geoip-cn win `direct`
    // first; only the non-CN AS132203 remainder is dropped. Scenario-gated:
    // emitted ONLY in cn-bypass — home/回国 and global must NOT drop (an abroad
    // user's overseas-Tencent connection is legitimate). Missing bundle degrades
    // safely (the engine skips a names route whose set is absent). via:'reject'
    // = silent drop. See docs/superpowers/plans/2026-05-30-tencent-overseas-reject.md.
    if (country === 'cn') {
      routes.push({ match: { names: ['tencent-overseas'] }, via: 'reject' });
    }
    routes.push({ match: { all: true }, via: serverUrl });
    return routes;
  }

  // Home / home_proxy: need a valid country profile (preset-based)
  const profile = countryToProfile(country);
  const preset = PROFILE_TO_PRESET[profile];
  if (!preset) {
    // Unknown country, fall back to global
    return [...prefix, { via: serverUrl, match: { all: true } }];
  }

  const countryRoute: RouteConfig = { via: 'k2p://home', match: { preset } };

  const defaultRoute: RouteConfig = defaultVia === 'proxy'
    ? { via: serverUrl, match: {} }
    : { via: 'direct', match: {} };

  return [...prefix, countryRoute, defaultRoute];
}

/** Map a RoutePreset to its defaultVia + countryVia values. */
function presetToConfig(preset: RoutePreset): Pick<ConfigState, 'defaultVia' | 'countryVia'> {
  switch (preset) {
    case 'global':     return { defaultVia: 'proxy',  countryVia: null };
    case 'bypass':     return { defaultVia: 'proxy',  countryVia: 'direct' };
    case 'home':       return { defaultVia: 'direct', countryVia: 'k2p' };
    case 'home_proxy': return { defaultVia: 'proxy',  countryVia: 'k2p' };
  }
}

/** Derive RoutePreset from state dimensions. */
function derivePreset(defaultVia: 'proxy' | 'direct', countryVia: 'direct' | 'k2p' | null): RoutePreset {
  if (countryVia === null) return 'global';
  if (countryVia === 'direct' && defaultVia === 'proxy') return 'bypass';
  if (countryVia === 'k2p' && defaultVia === 'direct') return 'home';
  if (countryVia === 'k2p' && defaultVia === 'proxy') return 'home_proxy';
  // Fallback — should not happen with well-formed state
  return 'global';
}

// ============ Storage ============

/**
 * Stored shape covers v3 (current) plus legacy v0/v1/v2 fields for migration.
 */
interface StoredConfig {
  // v3 fields
  defaultVia?: 'proxy' | 'direct';
  countryVia?: 'direct' | 'k2p' | null;
  country?: string | null;
  autoDetect?: boolean;
  alwaysOn?: boolean;
  // v2 fields (legacy)
  routingMode?: 'split' | 'global';
  selectedCountry?: string | null;
  // v1 fields (legacy)
  ruleMode?: 'global' | 'chnroute';
  modeOverride?: 'auto' | 'global' | 'manual';
  // v0 fields (ancient legacy)
  rule?: { global?: boolean };
  [key: string]: unknown;
}

interface ParsedConfig {
  defaultVia: 'proxy' | 'direct';
  countryVia: 'direct' | 'k2p' | null;
  country: string | null;
  autoDetect: boolean;
  alwaysOn: boolean;
  needsMigration: boolean;
}

function parseStored(stored: StoredConfig | null | undefined): ParsedConfig {
  if (!stored) {
    // Fresh install — default to CN, geo detection will override if user is elsewhere
    return { defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: true, alwaysOn: false, needsMigration: false };
  }

  // v3 shape: has defaultVia field
  if (stored.defaultVia !== undefined) {
    return {
      defaultVia: stored.defaultVia === 'direct' ? 'direct' : 'proxy',
      countryVia: stored.countryVia === 'direct' ? 'direct' : stored.countryVia === 'k2p' ? 'k2p' : null,
      country: stored.country ?? null,
      autoDetect: stored.autoDetect !== false,
      alwaysOn: stored.alwaysOn === true,
      needsMigration: false,
    };
  }

  // v2 shape: has routingMode field
  if (stored.routingMode === 'split' || stored.routingMode === 'global') {
    if (stored.routingMode === 'global') {
      return { defaultVia: 'proxy', countryVia: null, country: null, autoDetect: true, alwaysOn: false, needsMigration: true };
    }
    // split mode
    const autoDetect = stored.autoDetect !== false;
    return {
      defaultVia: 'proxy',
      countryVia: 'direct',
      country: autoDetect ? null : (stored.selectedCountry ?? null),
      autoDetect,
      alwaysOn: false,
      needsMigration: true,
    };
  }

  // v1 shape: has modeOverride field
  if (stored.modeOverride !== undefined) {
    if (stored.modeOverride === 'global') {
      return { defaultVia: 'proxy', countryVia: null, country: null, autoDetect: true, alwaysOn: false, needsMigration: true };
    }
    if (stored.modeOverride === 'manual') {
      const ruleMode = stored.ruleMode ?? 'chnroute';
      if (ruleMode === 'global') {
        return { defaultVia: 'proxy', countryVia: null, country: null, autoDetect: true, alwaysOn: false, needsMigration: true };
      }
      // manual + chnroute
      return { defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: false, alwaysOn: false, needsMigration: true };
    }
    // modeOverride === 'auto'
    return { defaultVia: 'proxy', countryVia: 'direct', country: null, autoDetect: true, alwaysOn: false, needsMigration: true };
  }

  // v0 shape: has rule.global field
  if (stored.rule !== undefined) {
    if (stored.rule?.global === true) {
      return { defaultVia: 'proxy', countryVia: null, country: null, autoDetect: true, alwaysOn: false, needsMigration: true };
    }
    return { defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: false, alwaysOn: false, needsMigration: true };
  }

  // Unknown shape, fresh defaults
  return { defaultVia: 'proxy', countryVia: 'direct', country: null, autoDetect: true, alwaysOn: false, needsMigration: false };
}

async function persist(
  defaultVia: 'proxy' | 'direct',
  countryVia: 'direct' | 'k2p' | null,
  country: string | null,
  autoDetect: boolean,
  alwaysOn: boolean,
): Promise<void> {
  try {
    const payload: Record<string, unknown> = { defaultVia, countryVia, autoDetect, alwaysOn };
    if (country) payload.country = country;
    await window._platform.storage.set(STORAGE_KEY, payload);
  } catch (error) {
    console.warn('[ConfigStore] Failed to save config to storage:', error);
  }
}

// ============ Store ============

export const useConfigStore = create<ConfigState & ConfigActions>()((set, get) => ({
  // State
  defaultVia: 'proxy',
  countryVia: 'direct',
  country: null,
  autoDetect: true,
  alwaysOn: false,
  detectedCountry: null,
  suggestedProfile: null,
  telemetry: { ruleMissEnabled: false },
  loaded: false,

  // Actions
  loadConfig: async () => {
    try {
      const stored = await window._platform.storage.get<StoredConfig>(STORAGE_KEY);
      const { defaultVia, countryVia, country, autoDetect, alwaysOn, needsMigration } = parseStored(stored);
      const preset = derivePreset(defaultVia, countryVia);
      console.info(
        '[ConfigStore] Config loaded: preset=' + preset
          + ', defaultVia=' + defaultVia
          + ', countryVia=' + (countryVia ?? 'null')
          + ', country=' + (country ?? 'null')
          + ', autoDetect=' + autoDetect
          + ', alwaysOn=' + alwaysOn,
      );
      set({ defaultVia, countryVia, country, autoDetect, alwaysOn, loaded: true });

      if (needsMigration) {
        try {
          await persist(defaultVia, countryVia, country, autoDetect, alwaysOn);
          console.info('[ConfigStore] Migrated legacy config to v3 shape');
        } catch (err) {
          console.warn('[ConfigStore] Migration write failed:', err);
        }
      }
    } catch (error) {
      console.warn('[ConfigStore] Failed to load config from storage:', error);
      set({ defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: true, alwaysOn: false, loaded: true });
    }
  },

  setPreset: async (preset) => {
    const { defaultVia, countryVia } = presetToConfig(preset);
    set({ defaultVia, countryVia });
    const { country, autoDetect, alwaysOn } = get();
    await persist(defaultVia, countryVia, country, autoDetect, alwaysOn);
  },

  setCountry: async (cc) => {
    const lower = cc.toLowerCase();
    set({ country: lower, autoDetect: false });
    const { defaultVia, countryVia, alwaysOn } = get();
    await persist(defaultVia, countryVia, lower, false, alwaysOn);
  },

  setAutoDetect: async (on) => {
    const next: Partial<ConfigState> = { autoDetect: on };
    if (on) {
      const { detectedCountry } = get();
      if (detectedCountry) {
        next.country = detectedCountry.toLowerCase();
      }
    }
    set(next);
    const { defaultVia, countryVia, country, alwaysOn } = get();
    await persist(defaultVia, countryVia, country, on, alwaysOn);
  },

  setAlwaysOn: async (on) => {
    console.info('[ConfigStore] setAlwaysOn: on=' + on);
    set({ alwaysOn: on });
    const { defaultVia, countryVia, country, autoDetect } = get();
    await persist(defaultVia, countryVia, country, autoDetect, on);
  },

  setDetectedProfile: ({ country, profile }) => {
    const next: Partial<ConfigState> = {};
    if (country !== undefined) next.detectedCountry = country ? country.toLowerCase() : null;
    if (profile !== undefined) next.suggestedProfile = profile || null;

    const { autoDetect } = get();
    if (autoDetect && country) {
      next.country = country.toLowerCase();
    }

    if (Object.keys(next).length > 0) {
      console.info(
        '[ConfigStore] setDetectedProfile: country=' + (country ?? 'null')
          + ', profile=' + (profile ?? 'null')
          + ', autoDetect=' + autoDetect,
      );
      set(next);
    }
  },

  fetchGeoDetection: async () => {
    try {
      const resp = await cloudApi.get<{ country: string; profile: string }>('/api/geo');
      // Fallback to CN if API fails or returns empty country
      const cc = (resp.code === 0 && resp.data?.country)
        ? resp.data.country.toLowerCase()
        : 'cn';
      const profile = (resp.code === 0 && resp.data?.profile) || 'cnroute';

      const { autoDetect, detectedCountry } = get();

      const next: Partial<ConfigState> = { detectedCountry: cc, suggestedProfile: profile };

      if (autoDetect) {
        next.country = cc;
      }

      // When autoDetect is off and no country yet, use first detection
      if (!autoDetect && !get().country && !detectedCountry) {
        next.country = cc;
        const { defaultVia, countryVia, alwaysOn } = get();
        await persist(defaultVia, countryVia, cc, false, alwaysOn);
      }

      console.info('[ConfigStore] fetchGeoDetection: country=' + cc + ', profile=' + profile + ', autoDetect=' + autoDetect);
      set(next);
    } catch (err) {
      console.warn('[ConfigStore] fetchGeoDetection failed, using CN default:', err);
      // Network error — still set CN as fallback
      const { autoDetect, country: current } = get();
      if (autoDetect || !current) {
        set({ detectedCountry: 'cn', suggestedProfile: 'cnroute', country: 'cn' });
      }
    }
  },

  resolvePreset: () => {
    const { defaultVia, countryVia } = get();
    return derivePreset(defaultVia, countryVia);
  },

  buildConnectConfig: (params?: ConnectConfigParams | string) => {
    const { defaultVia, countryVia, country, autoDetect, telemetry } = get();
    const preset = derivePreset(defaultVia, countryVia);
    const opts = typeof params === 'string' ? { serverUrl: params } : (params ?? {});
    const serverUrl = opts.serverUrl;

    const baseRoutes = buildRoutes(defaultVia, countryVia, country, serverUrl);

    // Plan C: Tier-1 per-app override routes — prepended before the region route
    const fd = opts.forceDirect ?? [];
    const fp = opts.forceProxy ?? [];
    const overrideRoutes: RouteConfig[] = [];
    if (fd.length > 0) overrideRoutes.push({ match: { apps: [...fd] }, via: 'direct' });
    if (fp.length > 0) overrideRoutes.push({ match: { apps: [...fp] }, via: serverUrl as string });
    const routes = [...overrideRoutes, ...baseRoutes];

    const result: ClientConfig = {
      ...CLIENT_CONFIG_DEFAULTS,
      mode: 'tun',
      log: { ...CLIENT_CONFIG_DEFAULTS.log, level: __K2_BUILD_LOG_LEVEL__ },
      routes,
    };

    if (telemetry.ruleMissEnabled) {
      result.telemetry = {
        rule_miss: { enabled: true },
      };
    }

    console.debug('[ConfigStore] buildConnectConfig:'
      + ' preset=' + preset
      + ', defaultVia=' + defaultVia
      + ', countryVia=' + (countryVia ?? 'null')
      + ', country=' + (country ?? 'null')
      + ', autoDetect=' + autoDetect
      + ', routes=' + (result.routes?.length ?? 0)
      + ', serverUrl=' + (serverUrl ?? 'none')
      + ', logLevel=' + result.log?.level
      + ', mode=' + result.mode
      + ', ruleMissTelemetry=' + telemetry.ruleMissEnabled);
    return result;
  },
}));
