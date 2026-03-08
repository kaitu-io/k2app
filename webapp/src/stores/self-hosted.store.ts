/**
 * Self-Hosted Store - manages single self-hosted k2v5 tunnel
 *
 * Persists tunnel URI to platform secure storage.
 * URI format: k2v5://username:token@host:port?ech=...&pin=...&country=XX#name
 */

import { create } from 'zustand';

// ============ Constants ============

const STORAGE_KEY = 'k2.self_hosted.tunnel';

// ============ Types ============

export interface SelfHostedTunnel {
  uri: string;       // Full k2v5:// URI (contains token)
  name: string;      // Parsed from URI #fragment, or host fallback
  country?: string;  // Parsed from URI &country=
}

interface SelfHostedState {
  tunnel: SelfHostedTunnel | null;
  loaded: boolean;
}

interface SelfHostedActions {
  loadTunnel: () => Promise<void>;
  saveTunnel: (uri: string) => Promise<void>;
  clearTunnel: () => Promise<void>;
}

// ============ Helpers ============

/**
 * Validate and parse a k2v5:// URI into a SelfHostedTunnel.
 * Returns null with error message if invalid.
 */
export function parseK2v5Uri(uri: string): { tunnel?: SelfHostedTunnel; error?: string } {
  const trimmed = uri.trim();

  if (!trimmed.startsWith('k2v5://')) {
    return { error: 'invalidUri' };
  }

  if (!trimmed.includes('@')) {
    return { error: 'invalidUriNoAuth' };
  }

  try {
    // Replace k2v5:// with https:// for URL parsing
    const parsed = new URL(trimmed.replace('k2v5://', 'https://'));

    if (!parsed.hostname) {
      return { error: 'invalidUri' };
    }

    const name = parsed.hash
      ? decodeURIComponent(parsed.hash.slice(1))
      : parsed.searchParams.get('ip') || parsed.hostname;

    const country = parsed.searchParams.get('country') || undefined;

    return {
      tunnel: { uri: trimmed, name, country },
    };
  } catch {
    return { error: 'invalidUri' };
  }
}

/**
 * Mask token in URI for display: show first 4 chars + ***
 */
export function maskUriToken(uri: string): string {
  try {
    const parsed = new URL(uri.replace('k2v5://', 'https://'));
    if (parsed.password) {
      const masked = parsed.password.length > 4
        ? parsed.password.slice(0, 4) + '***'
        : '***';
      return uri.replace(`:${parsed.password}@`, `:${masked}@`);
    }
  } catch {
    // fallback: return as-is
  }
  return uri;
}

// ============ Store ============

export const useSelfHostedStore = create<SelfHostedState & SelfHostedActions>()((set) => ({
  tunnel: null,
  loaded: false,

  loadTunnel: async () => {
    try {
      const stored = await window._platform.storage.get<SelfHostedTunnel>(STORAGE_KEY);
      set({ tunnel: stored ?? null, loaded: true });
    } catch (error) {
      console.warn('[SelfHostedStore] Failed to load tunnel:', error);
      set({ tunnel: null, loaded: true });
    }
  },

  saveTunnel: async (uri: string) => {
    const result = parseK2v5Uri(uri);
    if (result.error || !result.tunnel) {
      throw new Error(result.error || 'invalidUri');
    }

    set({ tunnel: result.tunnel });

    try {
      await window._platform.storage.set(STORAGE_KEY, result.tunnel);
    } catch (error) {
      console.warn('[SelfHostedStore] Failed to save tunnel:', error);
    }
  },

  clearTunnel: async () => {
    set({ tunnel: null });

    try {
      await window._platform.storage.remove(STORAGE_KEY);
    } catch (error) {
      console.warn('[SelfHostedStore] Failed to clear tunnel:', error);
    }
  },
}));
