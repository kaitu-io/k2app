import { create } from 'zustand';
import { getRegionalDetector, type AutoDetectedAppEntry } from '../utils/regionalAppDetection';
import { useConfigStore } from './config.store';
import type { InstalledApp } from '../types/kaitu-core';

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
 * Candidate apps shown in the "Add more" section of AppBypass page.
 * In-memory only — never persisted to storage or logs (privacy invariant).
 *  - process: macOS / Windows / Linux desktop (id = bundle id or exe path)
 *  - package: Android (id = packageName)
 */
export type Candidate =
  | { kind: 'process'; id: string; label: string; processNames: string[]; iconUrl?: string }
  | { kind: 'package'; id: string; label: string; iconUrl?: string };

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
  candidates: Candidate[];
  candidatesLoadedAt: number;
  candidatesLoading: boolean;
  /** i18n key on failure (e.g. 'dashboard:appBypass.loadFailed'); null on success. */
  candidatesError: string | null;
}

interface AppBypassActions {
  load(): Promise<void>;
  add(entry: Omit<AppBypassEntry, 'addedAt'>): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
  /** rescan: replace names of one entry (by id) with a fresh helper-name set */
  rescan(id: string, names: string[]): Promise<void>;
  /**
   * Refresh auto-detected Chinese-app list from the platform's installed-app provider.
   * Accepts a pre-fetched installed list to avoid double `listInstalled` IPC calls
   * when the caller has already enumerated apps (e.g. from `refreshCandidates`).
   */
  loadAutoDetected(preFetchedInstalled?: InstalledApp[]): Promise<void>;
  /**
   * Refresh the in-memory candidates cache from the platform's app-list provider.
   * Single IPC per call (in-flight dedup), preserves stale cache during load,
   * passes installed list to loadAutoDetected to avoid double PackageManager work.
   */
  refreshCandidates(): Promise<void>;
}

// Module-scoped in-flight dedup. Not part of store state — UI doesn't observe it.
// Cleared in the finally block so a follow-up refresh can run.
let inflightCandidatesRefresh: Promise<void> | null = null;

/**
 * Test-only escape hatch: clear the in-flight dedup guard between tests so a
 * test that fails before resolving its mocked IPC doesn't leak a pending Promise
 * into the next test (which would hit the dedup guard and hang).
 * @internal
 */
export function __resetAppBypassInflightForTests(): void {
  inflightCandidatesRefresh = null;
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
  candidates: [],
  candidatesLoadedAt: 0,
  candidatesLoading: false,
  candidatesError: null,

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

  async loadAutoDetected(preFetchedInstalled) {
    const provider = window._platform?.appList;
    if (!provider?.listInstalled && !preFetchedInstalled) {
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
      const installed = preFetchedInstalled ?? (await provider!.listInstalled!());
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

  refreshCandidates() {
    // Note: intentionally NOT declared `async` — an async function would wrap
    // `return inflightCandidatesRefresh` in a fresh Promise, breaking the
    // identity-equality the in-flight dedup test relies on.
    if (inflightCandidatesRefresh) return inflightCandidatesRefresh;
    const run = async () => {
      set({ candidatesLoading: true });
      const provider = window._platform?.appList;
      try {
        if (provider?.listInstalled) {
          const installed = await provider.listInstalled();
          const candidates: Candidate[] = installed.map((a) => ({
            kind: 'package',
            id: a.packageName,
            label: a.label,
            iconUrl: a.iconUrl,
          }));
          set({
            candidates,
            candidatesLoadedAt: Date.now(),
            candidatesError: null,
          });
          await get().loadAutoDetected(installed);
        } else if (provider?.listRunning) {
          const running = await provider.listRunning();
          const candidates: Candidate[] = running.map((a) => ({
            kind: 'process',
            id: a.id,
            label: a.label,
            processNames: a.processNames,
            iconUrl: a.iconUrl,
          }));
          set({
            candidates,
            candidatesLoadedAt: Date.now(),
            candidatesError: null,
          });
          // Desktop: no listInstalled — loadAutoDetected becomes a noop, but
          // calling it preserves the `autoDetectLoaded=true` contract for
          // first-time visitors observing the auto-detect section.
          await get().loadAutoDetected();
        } else {
          set({
            candidates: [],
            candidatesLoadedAt: Date.now(),
            candidatesError: null,
          });
          await get().loadAutoDetected();
        }
      } catch (err) {
        console.warn('[AppBypassStore] refreshCandidates failed:', err);
        set({ candidatesError: 'dashboard:appBypass.loadFailed' });
      } finally {
        set({ candidatesLoading: false });
      }
    };
    inflightCandidatesRefresh = run().finally(() => {
      inflightCandidatesRefresh = null;
    });
    return inflightCandidatesRefresh;
  },
}));
