import { create } from 'zustand';
import { classifyApps, type RouteDefault } from '../services/classify-apps';
import type { InstalledApp } from '../types/kaitu-core';

export const STORAGE_KEY = 'k2.routes.overrides';
export const OLD_STORAGE_KEY = 'k2.advanced.app_bypass';
export const OLD_MARKER = 'k2.advanced.app_bypass.migrated_at';

interface AppRoutesStorageShape {
  v: 1;
  forceProxy: string[];
  forceDirect: string[];
}

/** Minimal app shape required by setOverride — both InstalledApp and RunningApp satisfy it. */
type OverrideApp = Pick<InstalledApp, 'processNames'>;

interface AppRoutesState {
  /** Plan C: process names force-routed via proxy regardless of region default. */
  forceProxy: string[];
  /** Plan C: process names force-routed direct regardless of region default. */
  forceDirect: string[];
  /** Cached classify-apps result (keyed by app id). */
  classifications: Map<string, RouteDefault>;
  loaded: boolean;
  setForceProxy: (ids: string[]) => Promise<void>;
  setForceDirect: (ids: string[]) => Promise<void>;
  load: () => Promise<void>;
  classifyInstalled: (region: string, installed: InstalledApp[]) => Promise<void>;
  setOverride: (app: OverrideApp, mode: 'direct' | 'proxy' | 'default') => Promise<void>;
  resetOverrides: () => Promise<void>;
}

async function persist(forceProxy: string[], forceDirect: string[]): Promise<void> {
  if (!window._platform?.storage) return;
  await window._platform.storage.set<AppRoutesStorageShape>(STORAGE_KEY, {
    v: 1, forceProxy, forceDirect,
  });
}

/**
 * One-shot migration: remove the legacy app-bypass keys from _platform.storage
 * (encrypted bridge storage — NOT localStorage). The old shape
 * ({region, custom:{process_adds, package_adds}}) doesn't map to the new
 * {forceProxy, forceDirect} model, so we discard rather than carry a defensive
 * migration bridge. Exported for unit tests; invoked by load() at boot.
 */
export async function migrateLegacyKey(): Promise<void> {
  const s = window._platform?.storage;
  if (!s) return;
  if (await s.has(OLD_STORAGE_KEY)) await s.remove(OLD_STORAGE_KEY);
  if (await s.has(OLD_MARKER)) await s.remove(OLD_MARKER);
}

export const useAppRoutesStore = create<AppRoutesState>((set, get) => ({
  forceProxy: [],
  forceDirect: [],
  classifications: new Map(),
  loaded: false,
  setForceProxy: async (ids) => {
    const v = [...new Set(ids)];
    set({ forceProxy: v });
    await persist(v, get().forceDirect);
  },
  setForceDirect: async (ids) => {
    const v = [...new Set(ids)];
    set({ forceDirect: v });
    await persist(get().forceProxy, v);
  },
  load: async () => {
    await migrateLegacyKey();
    const stored = await window._platform?.storage?.get<AppRoutesStorageShape>(STORAGE_KEY);
    if (stored && stored.v === 1) {
      set({ forceProxy: stored.forceProxy ?? [], forceDirect: stored.forceDirect ?? [], loaded: true });
    } else {
      set({ loaded: true });
    }
  },
  classifyInstalled: async (region, installed) => {
    const map = await classifyApps(region, installed);
    set({ classifications: map });
  },
  setOverride: async (app, mode) => {
    const names = app.processNames ?? [];
    const fp = new Set(get().forceProxy);
    const fd = new Set(get().forceDirect);
    for (const n of names) { fp.delete(n); fd.delete(n); }
    if (mode === 'proxy') for (const n of names) fp.add(n);
    if (mode === 'direct') for (const n of names) fd.add(n);
    const proxy = [...fp]; const direct = [...fd];
    set({ forceProxy: proxy, forceDirect: direct });
    await persist(proxy, direct);
  },
  resetOverrides: async () => {
    set({ forceProxy: [], forceDirect: [] });
    await persist([], []);
  },
}));
