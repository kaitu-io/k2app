import { create } from 'zustand';
import { getRegionalDetector, type AutoDetectedAppEntry } from '../utils/regionalAppDetection';
import { useConfigStore } from './config.store';

export type { AutoDetectedAppEntry } from '../utils/regionalAppDetection';

export interface AppBypassEntry {
  id: string;
  label: string;
  kind: 'process' | 'package';
  names: string[];
  iconUrl?: string;
  addedAt: number;
}

/**
 * Detector-supplied i18n keys for the auto-detected section. Set when a
 * region-specific detector ran successfully; cleared when the dispatcher
 * resolves to the no-op (non-CN country, null country, missing provider).
 */
export interface AutoDetectorMeta {
  sectionTitleKey: string;
  noteSmartKey: string;
  noteGlobalKey: string;
}

interface AppBypassStorageShape {
  v: 1;
  entries: AppBypassEntry[];
}

const STORAGE_KEY = 'k2.advanced.app_bypass';

interface AppBypassState {
  entries: AppBypassEntry[];
  autoDetected: AutoDetectedAppEntry[];
  autoDetectorMeta: AutoDetectorMeta | null;
  loaded: boolean;
  autoDetectLoaded: boolean;
}

interface AppBypassActions {
  load(): Promise<void>;
  add(entry: Omit<AppBypassEntry, 'addedAt'>): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
  /** rescan: replace names of one entry (by id) with a fresh helper-name set */
  rescan(id: string, names: string[]): Promise<void>;
  /** Refresh auto-detected Chinese-app list from the platform's installed-app provider. */
  loadAutoDetected(): Promise<void>;
}

async function persist(entries: AppBypassEntry[]): Promise<void> {
  const payload: AppBypassStorageShape = { v: 1, entries };
  await window._platform.storage.set(STORAGE_KEY, payload);
}

export const useAppBypassStore = create<AppBypassState & AppBypassActions>()((set, get) => ({
  entries: [],
  autoDetected: [],
  autoDetectorMeta: null,
  loaded: false,
  autoDetectLoaded: false,

  async load() {
    try {
      const stored = await window._platform.storage.get<AppBypassStorageShape>(STORAGE_KEY);
      if (stored && stored.v === 1 && Array.isArray(stored.entries)) {
        set({ entries: stored.entries, loaded: true });
      } else {
        set({ entries: [], loaded: true });
      }
    } catch (err) {
      console.warn('[AppBypassStore] load failed:', err);
      set({ entries: [], loaded: true });
    }
  },

  async loadAutoDetected() {
    const provider = window._platform?.appList;
    if (!provider?.listInstalled) {
      set({ autoDetected: [], autoDetectorMeta: null, autoDetectLoaded: true });
      return;
    }
    const country = useConfigStore.getState().country;
    const detector = getRegionalDetector(country);
    if (detector.region === 'noop') {
      set({ autoDetected: [], autoDetectorMeta: null, autoDetectLoaded: true });
      return;
    }
    try {
      const installed = await provider.listInstalled();
      const detected = detector.detect(installed);
      set({
        autoDetected: detected,
        autoDetectorMeta: {
          sectionTitleKey: detector.sectionTitleKey,
          noteSmartKey: detector.noteSmartKey,
          noteGlobalKey: detector.noteGlobalKey,
        },
        autoDetectLoaded: true,
      });
    } catch (err) {
      console.warn('[AppBypassStore] loadAutoDetected failed:', err);
      set({ autoDetected: [], autoDetectorMeta: null, autoDetectLoaded: true });
    }
  },

  async add(entry) {
    const current = get().entries;
    if (current.some(e => e.id === entry.id)) return;
    const next = [...current, { ...entry, addedAt: Date.now() }];
    await persist(next);
    set({ entries: next });
  },

  async remove(id) {
    const next = get().entries.filter(e => e.id !== id);
    await persist(next);
    set({ entries: next });
  },

  async clear() {
    await persist([]);
    set({ entries: [] });
  },

  async rescan(id, names) {
    const next = get().entries.map(e =>
      e.id === id ? { ...e, names: [...new Set(names)] } : e
    );
    await persist(next);
    set({ entries: next });
  },
}));
