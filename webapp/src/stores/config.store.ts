/**
 * Config Store - VPN configuration management
 *
 * Responsibilities:
 * - Load/save user VPN preferences via window._platform.storage
 * - Deep merge partial updates into stored config
 * - Build final connect config (defaults + stored + server URL)
 * - Computed getters for common config fields
 *
 * Usage:
 * ```tsx
 * const { ruleMode, updateConfig, buildConnectConfig } = useConfigStore();
 *
 * // Update rule mode
 * await updateConfig({ rule: { global: true } });
 *
 * // Build config for connection
 * const config = buildConnectConfig('k2v5://server-url');
 * await window._k2.run('up', config);
 * ```
 */

import { create } from 'zustand';

import type { ClientConfig } from '../types/client-config';
import { CLIENT_CONFIG_DEFAULTS } from '../types/client-config';

// ============ Constants ============

const STORAGE_KEY = 'k2.vpn.config';

// ============ Types ============

interface ConfigState {
  config: ClientConfig;
  loaded: boolean;

  // Computed getters (recomputed on config change)
  ruleMode: 'global' | 'chnroute';
  mode: string;
  logLevel: string;
}

interface ConfigActions {
  loadConfig: () => Promise<void>;
  updateConfig: (partial: Partial<ClientConfig>) => Promise<void>;
  buildConnectConfig: (serverUrl?: string) => ClientConfig;
}

// ============ Helpers ============

/**
 * Deep merge two ClientConfig objects (1-level nesting only).
 * Nested objects (rule, log, proxy, dns) are spread-merged.
 */
function deepMerge(base: ClientConfig, override: Partial<ClientConfig>): ClientConfig {
  const result: ClientConfig = { ...base };

  for (const [key, value] of Object.entries(override)) {
    if (value !== undefined && value !== null && typeof value === 'object' && !Array.isArray(value)) {
      // Merge nested object (rule, log, proxy, dns)
      (result as any)[key] = {
        ...((base as any)[key] || {}),
        ...value,
      };
    } else {
      (result as any)[key] = value;
    }
  }

  return result;
}

/** Compute derived getter values from a config object */
function computeGetters(config: ClientConfig) {
  return {
    ruleMode: (config.rule?.global ? 'global' : 'chnroute') as 'global' | 'chnroute',
    mode: config.mode ?? CLIENT_CONFIG_DEFAULTS.mode!,
    logLevel: config.log?.level ?? CLIENT_CONFIG_DEFAULTS.log!.level!,
  };
}

// ============ Store ============

export const useConfigStore = create<ConfigState & ConfigActions>()((set, get) => ({
  // State
  config: {},
  loaded: false,

  // Computed getters (initialized from empty config)
  ...computeGetters({}),

  // Actions
  loadConfig: async () => {
    try {
      const stored = await window._platform.storage.get<ClientConfig>(STORAGE_KEY);
      const config = stored ?? {};
      set({
        config,
        loaded: true,
        ...computeGetters(config),
      });
    } catch (error) {
      console.warn('[ConfigStore] Failed to load config from storage:', error);
      set({ config: {}, loaded: true, ...computeGetters({}) });
    }
  },

  updateConfig: async (partial) => {
    const { config } = get();
    const merged = deepMerge(config, partial);
    set({
      config: merged,
      ...computeGetters(merged),
    });

    // Fire-and-forget persistence
    try {
      await window._platform.storage.set(STORAGE_KEY, merged);
    } catch (error) {
      console.warn('[ConfigStore] Failed to save config to storage:', error);
    }
  },

  buildConnectConfig: (serverUrl?: string) => {
    const { config } = get();
    const result = deepMerge(CLIENT_CONFIG_DEFAULTS, config);

    if (serverUrl) {
      result.server = serverUrl;
    }

    return result;
  },
}));
