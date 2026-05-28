import { create } from 'zustand';

export const STORAGE_KEY = 'k2.routes.overrides';
export const OLD_STORAGE_KEY = 'k2.advanced.app_bypass';
export const OLD_MARKER = 'k2.advanced.app_bypass.migrated_at';

interface AppRoutesStorageShape {
  v: 1;
  forceProxy: string[];
  forceDirect: string[];
}

interface AppRoutesState {
  /** Plan C: app ids force-routed via proxy regardless of region default. */
  forceProxy: string[];
  /** Plan C: app ids force-routed direct regardless of region default. */
  forceDirect: string[];
  loaded: boolean;
  setForceProxy: (ids: string[]) => Promise<void>;
  setForceDirect: (ids: string[]) => Promise<void>;
  load: () => Promise<void>;
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
}));
