import { create } from 'zustand';

export interface AppBypassEntry {
  id: string;
  label: string;
  kind: 'process' | 'package';
  names: string[];
  iconUrl?: string;
  addedAt: number;
}

interface AppBypassStorageShape {
  v: 1;
  entries: AppBypassEntry[];
}

const STORAGE_KEY = 'k2.advanced.app_bypass';

interface AppBypassState {
  entries: AppBypassEntry[];
  loaded: boolean;
}

interface AppBypassActions {
  load(): Promise<void>;
  add(entry: Omit<AppBypassEntry, 'addedAt'>): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
  /** rescan: replace names of one entry (by id) with a fresh helper-name set */
  rescan(id: string, names: string[]): Promise<void>;
}

async function persist(entries: AppBypassEntry[]): Promise<void> {
  const payload: AppBypassStorageShape = { v: 1, entries };
  await window._platform.storage.set(STORAGE_KEY, payload);
}

export const useAppBypassStore = create<AppBypassState & AppBypassActions>()((set, get) => ({
  entries: [],
  loaded: false,

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
