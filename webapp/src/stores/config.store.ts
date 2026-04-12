/**
 * Config Store - VPN configuration management
 *
 * Responsibilities:
 * - Persist UI-side VPN preferences (currently just ruleMode) via window._platform.storage
 * - Assemble the wire-contract ClientConfig (routes, mode, log) at connect time
 *
 * Usage:
 * ```tsx
 * const { ruleMode, updateRuleMode, buildConnectConfig } = useConfigStore();
 *
 * // Update rule mode
 * await updateRuleMode('global');
 *
 * // Build config for connection (serverUrl comes from the connection store)
 * const config = buildConnectConfig({ serverUrl: 'k2v5://...' });
 * await window._k2.run('up', config);
 * ```
 *
 * The Go `config.ClientConfig` contract no longer has a `server` field —
 * outbounds are expressed as `routes: [{via, match}, ...]`. See
 * `k2/config/config.go` and `k2/engine/engine.go:buildRouteEntries`.
 */

import { create } from 'zustand';

import type { ClientConfig, RouteConfig } from '../types/client-config';
import { CLIENT_CONFIG_DEFAULTS } from '../types/client-config';
import { profileToRoutes, legacyRuleModeToProfile } from '../utils/routes';

/** Build-time log level from K2_BUILD_LOG_LEVEL env var (default: 'debug'). Injected by Vite define. */
declare const __K2_BUILD_LOG_LEVEL__: string;

// ============ Constants ============

const STORAGE_KEY = 'k2.vpn.config';

// ============ Types ============

export type RuleMode = 'global' | 'chnroute';

/**
 * Which source the connect flow uses to decide the `routes[]` shape:
 *
 * - `auto`   — use `suggestedProfile` from the Center user profile
 *              (country-aware, the new default for fresh installs).
 * - `global` — force-global regardless of Center hint or legacy toggle.
 * - `manual` — honor the legacy `ruleMode` field (global / chnroute).
 *              Assigned to users who had a persisted ruleMode before the
 *              auto-profile feature landed, so their UX doesn't change.
 */
export type ModeOverride = 'auto' | 'global' | 'manual';

/**
 * Anonymous telemetry sub-state. Phase 1 of rule-miss telemetry ships
 * this field as a dark flag: wired into `buildConnectConfig` but
 * hard-defaulted to `{ ruleMissEnabled: false }`. There is NO UI in
 * Phase 1 — Phase 2 adds the opt-in toggle and persistence. Keeping
 * this under the typed store means the Phase 2 UI work is a pure
 * view-layer addition with no state-shape changes.
 */
export interface TelemetryState {
  ruleMissEnabled: boolean;
}

interface ConfigState {
  ruleMode: RuleMode;
  /** Center-detected country (2-letter ISO) cached from last user-info fetch. */
  detectedCountry: string | null;
  /** Center-suggested profile name (e.g. "cnroute") cached from last user-info fetch. */
  suggestedProfile: string | null;
  /** How to resolve the final profile at connect time — see ModeOverride doc. */
  modeOverride: ModeOverride;
  /**
   * Last country the user has seen/acknowledged in the travel banner. When
   * `detectedCountry` diverges from this value, the Dashboard shows a
   * one-off travel banner asking whether to switch profile. Persisted so the
   * banner doesn't re-trigger across app restarts for the same trip.
   */
  lastAcknowledgedCountry: string | null;
  telemetry: TelemetryState;
  loaded: boolean;
}

export interface ConnectConfigParams {
  serverUrl?: string;
}

export interface DetectedProfileUpdate {
  country?: string | null;
  profile?: string | null;
}

interface ConfigActions {
  loadConfig: () => Promise<void>;
  updateRuleMode: (mode: RuleMode) => Promise<void>;
  updateModeOverride: (mode: ModeOverride) => Promise<void>;
  /**
   * Cache the country + suggestedProfile returned by the Center user-info
   * endpoint. Never overrides a user who has `modeOverride === 'manual'`
   * (their legacy ruleMode toggle stays authoritative).
   */
  setDetectedProfile: (update: DetectedProfileUpdate) => void;
  /**
   * Mark the current `detectedCountry` (or any explicit country) as
   * acknowledged by the user. Called by the travel banner when the user
   * clicks either "Switch" or "Dismiss".
   */
  acknowledgeCountry: (country: string | null) => Promise<void>;
  /** Resolve the effective profile name given current store state. */
  resolveProfile: () => string;
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
 * Build the outbound routes list given a resolved profile name.
 *
 * Always prepends the gateway-only direct route for ipinfo.io so the router
 * can probe its egress IP on gateway builds.
 */
function buildRoutes(serverUrl: string | undefined, profile: string): RouteConfig[] {
  const prefix = gatewayPrefix();
  if (!serverUrl) {
    // Without a server URL we cannot assemble a working TUN config. Return
    // whatever prefix routes we have so callers can still inspect the shape
    // during tests; the daemon will reject the connect attempt.
    return prefix;
  }

  return [...prefix, ...profileToRoutes(profile, serverUrl)];
}

/** Legacy persisted shape carrying the old `server` / `rule.global` fields. */
interface LegacyStoredConfig {
  ruleMode?: RuleMode;
  modeOverride?: ModeOverride;
  lastAcknowledgedCountry?: string | null;
  rule?: { global?: boolean };
  server?: string;
  [key: string]: unknown;
}

interface ParsedStoredConfig {
  ruleMode: RuleMode;
  modeOverride: ModeOverride;
  lastAcknowledgedCountry: string | null;
  /** True if the user already had a persisted ruleMode (pre-auto-profile install). */
  hadLegacyRuleMode: boolean;
}

function parseStored(stored: LegacyStoredConfig | null | undefined): ParsedStoredConfig {
  if (!stored) {
    // Fresh install: opt into auto profile selection.
    return {
      ruleMode: 'chnroute',
      modeOverride: 'auto',
      lastAcknowledgedCountry: null,
      hadLegacyRuleMode: false,
    };
  }

  let ruleMode: RuleMode = 'chnroute';
  let hadLegacyRuleMode = false;

  if (stored.ruleMode === 'global' || stored.ruleMode === 'chnroute') {
    ruleMode = stored.ruleMode;
    hadLegacyRuleMode = true;
  } else if (stored.rule?.global === true) {
    // Legacy `rule.global=true` shape.
    ruleMode = 'global';
    hadLegacyRuleMode = true;
  } else if (stored.rule?.global === false) {
    ruleMode = 'chnroute';
    hadLegacyRuleMode = true;
  }

  let modeOverride: ModeOverride;
  if (
    stored.modeOverride === 'auto'
    || stored.modeOverride === 'global'
    || stored.modeOverride === 'manual'
  ) {
    modeOverride = stored.modeOverride;
  } else {
    // No explicit modeOverride in storage: existing users (who have a
    // persisted ruleMode) default to 'manual' to preserve their UX.
    // Fresh installs default to 'auto'.
    modeOverride = hadLegacyRuleMode ? 'manual' : 'auto';
  }

  const lastAcknowledgedCountry = typeof stored.lastAcknowledgedCountry === 'string'
    ? stored.lastAcknowledgedCountry
    : null;

  return { ruleMode, modeOverride, lastAcknowledgedCountry, hadLegacyRuleMode };
}

async function persist(
  ruleMode: RuleMode,
  modeOverride: ModeOverride,
  lastAcknowledgedCountry: string | null,
): Promise<void> {
  try {
    // Only include lastAcknowledgedCountry when set so the persisted shape
    // stays { ruleMode, modeOverride } for users who never hit the travel
    // banner (keeps existing tests + migration behaviour stable).
    const payload: Record<string, unknown> = { ruleMode, modeOverride };
    if (lastAcknowledgedCountry) {
      payload.lastAcknowledgedCountry = lastAcknowledgedCountry;
    }
    await window._platform.storage.set(STORAGE_KEY, payload);
  } catch (error) {
    console.warn('[ConfigStore] Failed to save config to storage:', error);
  }
}

// ============ Store ============

export const useConfigStore = create<ConfigState & ConfigActions>()((set, get) => ({
  // State
  ruleMode: 'chnroute',
  detectedCountry: null,
  suggestedProfile: null,
  modeOverride: 'auto',
  lastAcknowledgedCountry: null,
  // Phase 1 default: all telemetry OFF. No storage load, no UI toggle.
  // Flip via Phase 2 UI only. When false, buildConnectConfig emits no
  // telemetry block and the Go engine skips reporter construction.
  telemetry: { ruleMissEnabled: false },
  loaded: false,

  // Actions
  loadConfig: async () => {
    try {
      const stored = await window._platform.storage.get<LegacyStoredConfig>(STORAGE_KEY);
      const { ruleMode, modeOverride, lastAcknowledgedCountry } = parseStored(stored);
      console.info(
        '[ConfigStore] Config loaded: ruleMode=' + ruleMode
          + ', modeOverride=' + modeOverride
          + ', lastAcknowledgedCountry=' + (lastAcknowledgedCountry ?? 'null'),
      );
      set({ ruleMode, modeOverride, lastAcknowledgedCountry, loaded: true });

      // One-shot migration: if legacy shape detected OR the parser injected a
      // modeOverride that wasn't persisted yet, rewrite storage so we never
      // ship the dead `server` / `rule.global` fields back to the daemon.
      const needsMigration = stored && (
        stored.server !== undefined
        || stored.rule !== undefined
        || stored.modeOverride === undefined
      );
      if (needsMigration) {
        try {
          await persist(ruleMode, modeOverride, lastAcknowledgedCountry);
          console.info('[ConfigStore] Migrated legacy config shape to { ruleMode, modeOverride }');
        } catch (err) {
          console.warn('[ConfigStore] Legacy config migration write failed:', err);
        }
      }
    } catch (error) {
      console.warn('[ConfigStore] Failed to load config from storage:', error);
      set({
        ruleMode: 'chnroute',
        modeOverride: 'auto',
        lastAcknowledgedCountry: null,
        loaded: true,
      });
    }
  },

  updateRuleMode: async (mode) => {
    // Changing ruleMode is an explicit manual override — pin modeOverride to
    // 'manual' so subsequent Center hints don't reshape the routes.
    set({ ruleMode: mode, modeOverride: 'manual' });
    await persist(mode, 'manual', get().lastAcknowledgedCountry);
  },

  updateModeOverride: async (mode) => {
    set({ modeOverride: mode });
    await persist(get().ruleMode, mode, get().lastAcknowledgedCountry);
  },

  acknowledgeCountry: async (country) => {
    const normalized = country ? country.toLowerCase() : null;
    set({ lastAcknowledgedCountry: normalized });
    const { ruleMode, modeOverride } = get();
    await persist(ruleMode, modeOverride, normalized);
  },

  setDetectedProfile: ({ country, profile }) => {
    const { modeOverride } = get();
    // Don't clobber users who've pinned a manual override.
    if (modeOverride === 'manual') {
      console.debug(
        '[ConfigStore] setDetectedProfile skipped (modeOverride=manual), '
          + 'country=' + (country ?? 'null') + ', profile=' + (profile ?? 'null'),
      );
      return;
    }
    const next: Partial<ConfigState> = {};
    if (country !== undefined) next.detectedCountry = country || null;
    if (profile !== undefined) next.suggestedProfile = profile || null;
    if (Object.keys(next).length > 0) {
      console.info(
        '[ConfigStore] setDetectedProfile: country=' + (country ?? 'null')
          + ', profile=' + (profile ?? 'null'),
      );
      set(next);
    }
  },

  resolveProfile: () => {
    const { modeOverride, ruleMode, suggestedProfile } = get();
    if (modeOverride === 'global') return 'global';
    if (modeOverride === 'manual') return legacyRuleModeToProfile(ruleMode);
    // modeOverride === 'auto'
    return suggestedProfile && suggestedProfile.length > 0 ? suggestedProfile : 'global';
  },

  buildConnectConfig: (params?: ConnectConfigParams | string) => {
    const profile = get().resolveProfile();
    const { ruleMode, modeOverride, suggestedProfile, telemetry } = get();
    const serverUrl = typeof params === 'string'
      ? params
      : params?.serverUrl;

    const result: ClientConfig = {
      ...CLIENT_CONFIG_DEFAULTS,
      mode: 'tun',
      log: { ...CLIENT_CONFIG_DEFAULTS.log, level: __K2_BUILD_LOG_LEVEL__ },
      routes: buildRoutes(serverUrl, profile),
    };

    // Phase 1 dark instrumentation: emit a telemetry block ONLY when the
    // dark flag is explicitly flipped on. Zero-value omission keeps the
    // JSON payload unchanged from the pre-telemetry shape and leaves the
    // Go engine's reporter construction path dead by default.
    if (telemetry.ruleMissEnabled) {
      result.telemetry = {
        rule_miss: {
          enabled: true,
        },
      };
    }

    console.debug('[ConfigStore] buildConnectConfig:'
      + ' modeOverride=' + modeOverride
      + ', ruleMode=' + ruleMode
      + ', suggestedProfile=' + (suggestedProfile ?? 'null')
      + ', resolvedProfile=' + profile
      + ', routes=' + (result.routes?.length ?? 0)
      + ', serverUrl=' + (serverUrl ?? 'none')
      + ', logLevel=' + result.log?.level
      + ', mode=' + result.mode
      + ', ruleMissTelemetry=' + telemetry.ruleMissEnabled);
    return result;
  },
}));
