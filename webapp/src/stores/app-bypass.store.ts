import { create } from 'zustand';

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

interface AppBypassStorageShape {
  v: 1;
  entries: AppBypassEntry[];
}

const STORAGE_KEY = 'k2.advanced.app_bypass';

/**
 * Wire shape of the `app-bypass-get` daemon response. Mirrors
 * k2/daemon/api_app_bypass.go `appBypassState`. Snake_case keys come from
 * Go json.Marshal; webapp keeps them snake_case here (no bridge remap) to
 * match the daemon contract.
 */
export interface DaemonAppBypassState {
  feature_supported: boolean;
  region: string;
  custom: {
    process_adds: string[];
    package_adds: string[];
  };
}

interface DaemonDelta {
  process?: string[];
  package?: string[];
}

function isDaemonBacked(): boolean {
  return !!window._platform?.appBypass?.daemonBacked;
}

async function daemonGet(): Promise<DaemonAppBypassState | null> {
  const r = await window._k2.run<DaemonAppBypassState>('app-bypass-get');
  if (r.code !== 0 || !r.data) {
    console.warn('[AppBypassStore] daemon get failed:', r.code, r.message);
    return null;
  }
  return r.data;
}

async function daemonSetCustom(add: DaemonDelta, remove: DaemonDelta): Promise<DaemonAppBypassState | null> {
  const r = await window._k2.run<DaemonAppBypassState>('app-bypass-set-custom', { add, remove });
  if (r.code !== 0 || !r.data) {
    console.warn('[AppBypassStore] daemon set-custom failed:', r.code, r.message);
    return null;
  }
  return r.data;
}

async function daemonSetRegion(region: string): Promise<DaemonAppBypassState | null> {
  const r = await window._k2.run<DaemonAppBypassState>('app-bypass-set-region', { region });
  if (r.code !== 0 || !r.data) {
    console.warn('[AppBypassStore] daemon set-region failed:', r.code, r.message);
    return null;
  }
  return r.data;
}

/**
 * Lift daemon's flat process/package add lists into the webapp's grouped
 * AppBypassEntry shape. Each daemon entry becomes one synthetic entry with
 * id = name, label = name (UI can resolve a prettier label via candidates
 * lookup later — not all daemon entries have icon/label metadata).
 */
function daemonStateToEntries(s: DaemonAppBypassState): AppBypassEntry[] {
  const now = Date.now();
  const out: AppBypassEntry[] = [];
  for (const n of s.custom.process_adds ?? []) {
    out.push({ id: n, label: n, kind: 'process', names: [n], addedAt: now });
  }
  for (const n of s.custom.package_adds ?? []) {
    out.push({ id: n, label: n, kind: 'package', names: [n], addedAt: now });
  }
  return out;
}

function entryToDelta(e: { kind: 'process' | 'package'; names: string[] }): DaemonDelta {
  return e.kind === 'process'
    ? { process: e.names, package: [] }
    : { process: [], package: e.names };
}

interface AppBypassState {
  entries: AppBypassEntry[];
  loaded: boolean;
  candidates: Candidate[];
  candidatesLoadedAt: number;
  candidatesLoading: boolean;
  /** i18n key on failure (e.g. 'dashboard:appBypass.loadFailed'); null on success. */
  candidatesError: string | null;
  /**
   * Daemon-reported platform support. `undefined` until first load resolves.
   * On mobile (no daemon) this stays `undefined`; UI uses a separate fallback
   * gate (`!!window._platform?.appList`).
   */
  featureSupported: boolean | undefined;
  /**
   * Region returned by daemon (`'cn' / 'ir' / ...` or `''` for off).
   * On mobile mirrors what's about to be packed into ClientConfig.
   */
  region: string;
}

interface AppBypassActions {
  load(): Promise<void>;
  add(entry: Omit<AppBypassEntry, 'addedAt'>): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
  /** rescan: replace names of one entry (by id) with a fresh helper-name set */
  rescan(id: string, names: string[]): Promise<void>;
  /**
   * Sync the smart-bypass region to the daemon. On mobile this is a no-op
   * (region travels via ClientConfig.app_bypass at connect time).
   *
   * Pass empty string to disable smart bypass.
   */
  setRegion(region: string): Promise<void>;
  /**
   * Refresh the in-memory candidates cache from the platform's app-list provider.
   * Single IPC per call (in-flight dedup), preserves stale cache during load.
   *
   * App Bypass v2 retired local Chinese-app detection; smart bypass now runs
   * inside the Go engine via region presets shipped through k2-rules, so this
   * helper only feeds the "Add more" picker.
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
  loaded: false,
  candidates: [],
  candidatesLoadedAt: 0,
  candidatesLoading: false,
  candidatesError: null,
  featureSupported: undefined,
  region: '',

  async load() {
    if (!isDaemonBacked()) {
      // Mobile path: read from local storage as before.
      try {
        const stored = await window._platform.storage.get<AppBypassStorageShape>(STORAGE_KEY);
        if (stored && stored.v === 1 && Array.isArray(stored.entries)) {
          set({ entries: stored.entries, loaded: true });
        } else {
          set({ entries: [], loaded: true });
        }
      } catch (err) {
        console.warn('[AppBypassStore] load failed (mobile):', err);
        set({ entries: [], loaded: true });
      }
      return;
    }

    // Desktop / standalone path: daemon owns state.
    const snapshot = await daemonGet();
    if (snapshot == null) {
      // Daemon unreachable. Show empty UI without spinning — better than
      // hanging the page on an offline daemon during AuthGate startup.
      set({ entries: [], loaded: true, featureSupported: undefined });
      return;
    }

    // One-shot migration (spec §10.4): if daemon has no custom entries but
    // local storage does, push local → daemon, then delete local. Idempotent
    // via daemon applyDelta dedup.
    let entriesFromDaemon = daemonStateToEntries(snapshot);
    if (entriesFromDaemon.length === 0) {
      try {
        const legacy = await window._platform.storage.get<AppBypassStorageShape>(STORAGE_KEY);
        if (legacy && legacy.v === 1 && Array.isArray(legacy.entries) && legacy.entries.length > 0) {
          const processAdds: string[] = [];
          const packageAdds: string[] = [];
          for (const e of legacy.entries) {
            for (const n of e.names ?? []) {
              if (e.kind === 'process') processAdds.push(n);
              else if (e.kind === 'package') packageAdds.push(n);
            }
          }
          const migrated = await daemonSetCustom(
            { process: processAdds, package: packageAdds },
            { process: [], package: [] },
          );
          if (migrated) {
            await window._platform.storage.remove(STORAGE_KEY);
            entriesFromDaemon = daemonStateToEntries(migrated);
            console.info('[AppBypassStore] migrated', legacy.entries.length, 'local entries to daemon');
          }
        }
      } catch (err) {
        // Migration is best-effort. Failure leaves local storage intact so the
        // next launch can retry. Don't surface to the user.
        console.warn('[AppBypassStore] migration attempt failed (will retry next launch):', err);
      }
    }

    set({
      entries: entriesFromDaemon,
      loaded: true,
      featureSupported: snapshot.feature_supported,
      region: snapshot.region,
    });
  },

  async add(entry) {
    const current = get().entries;
    if (current.some(e => e.id === entry.id)) return;

    if (!isDaemonBacked()) {
      // Mobile: local persist as before.
      const next = [...current, { ...entry, addedAt: Date.now() }];
      await persist(next);
      set({ entries: next });
      return;
    }

    // Daemon path: push add delta, refresh entries from daemon response.
    const delta = entryToDelta(entry);
    const snap = await daemonSetCustom(delta, { process: [], package: [] });
    if (snap == null) return; // daemon unreachable; UI keeps stale view
    set({ entries: daemonStateToEntries(snap) });
  },

  async remove(id) {
    const current = get().entries;
    const target = current.find(e => e.id === id);
    if (!target) return;

    if (!isDaemonBacked()) {
      const next = current.filter(e => e.id !== id);
      await persist(next);
      set({ entries: next });
      return;
    }

    const delta = entryToDelta(target);
    const snap = await daemonSetCustom({ process: [], package: [] }, delta);
    if (snap == null) return;
    set({ entries: daemonStateToEntries(snap) });
  },

  async clear() {
    if (!isDaemonBacked()) {
      await persist([]);
      set({ entries: [] });
      return;
    }
    // Daemon path: delete every name we currently know about.
    const current = get().entries;
    const proc: string[] = [];
    const pkg: string[] = [];
    for (const e of current) {
      for (const n of e.names ?? []) {
        if (e.kind === 'process') proc.push(n);
        else if (e.kind === 'package') pkg.push(n);
      }
    }
    const snap = await daemonSetCustom(
      { process: [], package: [] },
      { process: proc, package: pkg },
    );
    if (snap == null) return;
    set({ entries: daemonStateToEntries(snap) });
  },

  async rescan(id, names) {
    const current = get().entries;
    const target = current.find(e => e.id === id);
    if (!target) return;
    const uniqNew = [...new Set(names)];

    if (!isDaemonBacked()) {
      const next = current.map(e =>
        e.id === id ? { ...e, names: uniqNew } : e
      );
      await persist(next);
      set({ entries: next });
      return;
    }

    // Daemon: remove old names, add new names. Both lists keyed by entry.kind.
    const oldNames = target.names ?? [];
    const oldDelta = target.kind === 'process'
      ? { process: oldNames, package: [] }
      : { process: [], package: oldNames };
    const newDelta = target.kind === 'process'
      ? { process: uniqNew, package: [] }
      : { process: [], package: uniqNew };
    const snap = await daemonSetCustom(newDelta, oldDelta);
    if (snap == null) return;
    set({ entries: daemonStateToEntries(snap) });
  },

  async setRegion(region) {
    if (!isDaemonBacked()) return; // mobile picks region from country via config.store
    const snap = await daemonSetRegion(region);
    if (snap == null) return;
    set({ region: snap.region });
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
        } else {
          set({
            candidates: [],
            candidatesLoadedAt: Date.now(),
            candidatesError: null,
          });
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
