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

/** Build-time log level from K2_BUILD_LOG_LEVEL env var (default: 'debug'). Injected by Vite define. */
declare const __K2_BUILD_LOG_LEVEL__: string;

// ============ Constants ============

const STORAGE_KEY = 'k2.vpn.config';

// ============ Types ============

export type RuleMode = 'global' | 'chnroute';

interface ConfigState {
  ruleMode: RuleMode;
  loaded: boolean;
}

export interface ConnectConfigParams {
  serverUrl?: string;
}

interface ConfigActions {
  loadConfig: () => Promise<void>;
  updateRuleMode: (mode: RuleMode) => Promise<void>;
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

function buildRoutes(serverUrl: string | undefined, mode: RuleMode): RouteConfig[] {
  const prefix = gatewayPrefix();
  if (!serverUrl) {
    // Without a server URL we cannot assemble a working TUN config. Return
    // whatever prefix routes we have so callers can still inspect the shape
    // during tests; the daemon will reject the connect attempt.
    return prefix;
  }

  if (mode === 'global') {
    return [...prefix, { via: serverUrl, match: { all: true } }];
  }

  // chnroute: CN direct, foreign via k2v5 (empty match on last route → fallback).
  return [
    ...prefix,
    { via: 'direct', match: { preset: 'cn-access' } },
    { via: serverUrl, match: {} },
  ];
}

/** Legacy persisted shape carrying the old `server` / `rule.global` fields. */
interface LegacyStoredConfig {
  ruleMode?: RuleMode;
  rule?: { global?: boolean };
  server?: string;
  [key: string]: unknown;
}

function parseStoredRuleMode(stored: LegacyStoredConfig | null | undefined): RuleMode {
  if (!stored) return 'chnroute';
  if (stored.ruleMode === 'global' || stored.ruleMode === 'chnroute') {
    return stored.ruleMode;
  }
  // Fall back to legacy `rule.global` toggle if present.
  if (stored.rule?.global === true) return 'global';
  return 'chnroute';
}

// ============ Store ============

export const useConfigStore = create<ConfigState & ConfigActions>()((set, get) => ({
  // State
  ruleMode: 'chnroute',
  loaded: false,

  // Actions
  loadConfig: async () => {
    try {
      const stored = await window._platform.storage.get<LegacyStoredConfig>(STORAGE_KEY);
      const ruleMode = parseStoredRuleMode(stored);
      console.info('[ConfigStore] Config loaded: ruleMode=' + ruleMode);
      set({ ruleMode, loaded: true });

      // One-shot migration: if legacy shape detected, rewrite storage so we
      // never ship the dead `server` / `rule.global` fields back to the daemon.
      if (stored && (stored.server !== undefined || stored.rule !== undefined)) {
        try {
          await window._platform.storage.set(STORAGE_KEY, { ruleMode });
          console.info('[ConfigStore] Migrated legacy config shape to { ruleMode }');
        } catch (err) {
          console.warn('[ConfigStore] Legacy config migration write failed:', err);
        }
      }
    } catch (error) {
      console.warn('[ConfigStore] Failed to load config from storage:', error);
      set({ ruleMode: 'chnroute', loaded: true });
    }
  },

  updateRuleMode: async (mode) => {
    set({ ruleMode: mode });
    try {
      await window._platform.storage.set(STORAGE_KEY, { ruleMode: mode });
    } catch (error) {
      console.warn('[ConfigStore] Failed to save config to storage:', error);
    }
  },

  buildConnectConfig: (params?: ConnectConfigParams | string) => {
    const { ruleMode } = get();
    const serverUrl = typeof params === 'string'
      ? params
      : params?.serverUrl;

    const result: ClientConfig = {
      ...CLIENT_CONFIG_DEFAULTS,
      mode: 'tun',
      log: { ...CLIENT_CONFIG_DEFAULTS.log, level: __K2_BUILD_LOG_LEVEL__ },
      routes: buildRoutes(serverUrl, ruleMode),
    };

    console.debug('[ConfigStore] buildConnectConfig: ruleMode=' + ruleMode
      + ', routes=' + (result.routes?.length ?? 0)
      + ', serverUrl=' + (serverUrl ?? 'none')
      + ', logLevel=' + result.log?.level
      + ', mode=' + result.mode);
    return result;
  },
}));
