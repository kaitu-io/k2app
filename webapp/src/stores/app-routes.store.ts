import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

export const OLD_STORAGE_KEY = 'k2.advanced.app_bypass';
export const NEW_STORAGE_KEY = 'k2.routes.overrides';

interface AppRoutesState {
  /** ISO 3166-1 alpha-2 region for Tier-2 match.region route. */
  region: string;
  /** Plan C: app ids force-routed via proxy regardless of region default. */
  forceProxy: string[];
  /** Plan C: app ids force-routed direct regardless of region default. */
  forceDirect: string[];

  setRegion: (region: string) => void;
  setForceProxy: (ids: string[]) => void;
  setForceDirect: (ids: string[]) => void;
}

/**
 * One-shot migration: drop the legacy `k2.advanced.app_bypass` key.
 * Old shape ({region, custom:{process_adds, package_adds}}) doesn't map
 * to the new {forceProxy, forceDirect} model, so we discard rather than
 * carry a defensive migration bridge.
 *
 * Exported for unit tests. Auto-runs at module load.
 */
export function migrateLegacyKey(): void {
  if (typeof localStorage === 'undefined') return;
  if (localStorage.getItem(OLD_STORAGE_KEY) !== null) {
    localStorage.removeItem(OLD_STORAGE_KEY);
  }
}

migrateLegacyKey();

export const useAppRoutesStore = create<AppRoutesState>()(
  persist(
    (set) => ({
      region: '',
      forceProxy: [],
      forceDirect: [],
      setRegion: (region) => set({ region }),
      setForceProxy: (ids) => set({ forceProxy: [...new Set(ids)] }),
      setForceDirect: (ids) => set({ forceDirect: [...new Set(ids)] }),
    }),
    {
      name: NEW_STORAGE_KEY,
      storage: createJSONStorage(() => localStorage),
    },
  ),
);
