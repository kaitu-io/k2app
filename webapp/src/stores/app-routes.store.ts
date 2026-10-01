import { create } from 'zustand';
import { classifyApps, type RouteDefault } from '../services/classify-apps';
import type { InstalledApp } from '../types/kaitu-core';

export const STORAGE_KEY = 'k2.routes.overrides';
export const OLD_STORAGE_KEY = 'k2.advanced.app_bypass';
export const OLD_MARKER = 'k2.advanced.app_bypass.migrated_at';

export interface AppOverride {
  mode: 'direct' | 'proxy';
  /**
   * The app's full process-name set as of the last (re)write — these are the
   * engine `match.apps` values. Kept per app so the UI can read an override
   * back by APP identity: reading it back through the flat name lists (the v1
   * shape) misattributed overrides whenever two apps shared a helper basename
   * (crashpad_handler.exe & co) or when an app's known name set later grew.
   */
  names: string[];
  /**
   * Directories whose every executable belongs to the app — the engine
   * `match.app_paths` values (macOS: the .app bundle). Absent on overrides
   * saved before paths existed, and for apps identified by name only.
   */
  paths?: string[];
}

interface AppRoutesStorageShape {
  v: 2;
  apps: Record<string, AppOverride>;
}

/** Minimal app shape required by setOverride — both InstalledApp and RunningApp satisfy it. */
type OverrideApp = Pick<InstalledApp, 'id' | 'processNames' | 'paths'>;

interface AppRoutesState {
  /** Per-app overrides keyed by app id (install dir / bundle path / exe path). */
  overrides: Record<string, AppOverride>;
  /** Derived: union of overridden-direct apps' process names (feeds Tier-1 routes). */
  forceDirect: string[];
  /** Derived: union of overridden-proxy apps' process names. */
  forceProxy: string[];
  /** Derived: union of overridden-direct apps' directories (feeds match.app_paths). */
  forceDirectPaths: string[];
  /** Derived: union of overridden-proxy apps' directories. */
  forceProxyPaths: string[];
  /** Cached classify-apps result (keyed by app id). */
  classifications: Map<string, RouteDefault>;
  loaded: boolean;
  load: () => Promise<void>;
  classifyInstalled: (region: string, installed: InstalledApp[]) => Promise<void>;
  setOverride: (app: OverrideApp, mode: 'direct' | 'proxy' | 'default') => Promise<void>;
  refreshOverrideNames: (apps: OverrideApp[]) => Promise<void>;
  resetOverrides: () => Promise<void>;
}

type Derived = Pick<AppRoutesState, 'forceDirect' | 'forceProxy' | 'forceDirectPaths' | 'forceProxyPaths'>;

function derive(overrides: Record<string, AppOverride>): Derived {
  const direct = new Set<string>();
  const proxy = new Set<string>();
  const directPaths = new Set<string>();
  const proxyPaths = new Set<string>();
  for (const o of Object.values(overrides)) {
    for (const n of o.names) (o.mode === 'direct' ? direct : proxy).add(n);
    for (const p of o.paths ?? []) (o.mode === 'direct' ? directPaths : proxyPaths).add(p);
  }
  return {
    forceDirect: [...direct],
    forceProxy: [...proxy],
    forceDirectPaths: [...directPaths],
    forceProxyPaths: [...proxyPaths],
  };
}

async function persist(overrides: Record<string, AppOverride>): Promise<void> {
  if (!window._platform?.storage) return;
  await window._platform.storage.set<AppRoutesStorageShape>(STORAGE_KEY, { v: 2, apps: overrides });
}

/**
 * One-shot migration: remove the legacy app-bypass keys from _platform.storage
 * (encrypted bridge storage — NOT localStorage). The old shape
 * ({region, custom:{process_adds, package_adds}}) doesn't map to the new
 * per-app model, so we discard rather than carry a defensive migration
 * bridge. Exported for unit tests; invoked by load() at boot.
 */
export async function migrateLegacyKey(): Promise<void> {
  const s = window._platform?.storage;
  if (!s) return;
  if (await s.has(OLD_STORAGE_KEY)) await s.remove(OLD_STORAGE_KEY);
  if (await s.has(OLD_MARKER)) await s.remove(OLD_MARKER);
}

export const useAppRoutesStore = create<AppRoutesState>((set, get) => ({
  overrides: {},
  forceDirect: [],
  forceProxy: [],
  forceDirectPaths: [],
  forceProxyPaths: [],
  classifications: new Map(),
  loaded: false,
  load: async () => {
    await migrateLegacyKey();
    const stored = await window._platform?.storage?.get<AppRoutesStorageShape>(STORAGE_KEY);
    // v1 ({v:1, forceProxy, forceDirect} flat name lists) is discarded, not
    // migrated: it carried no app identity, so any conversion would guess.
    // Overrides are one tap to re-set.
    if (stored && stored.v === 2 && stored.apps) {
      set({ overrides: stored.apps, ...derive(stored.apps), loaded: true });
    } else {
      set({ loaded: true });
    }
  },
  classifyInstalled: async (region, installed) => {
    const map = await classifyApps(region, installed);
    set({ classifications: map });
  },
  setOverride: async (app, mode) => {
    const overrides = { ...get().overrides };
    const names = [...new Set(app.processNames ?? [])];
    const paths = [...new Set(app.paths ?? [])];
    if (mode === 'default' || (names.length === 0 && paths.length === 0)) {
      delete overrides[app.id];
    } else {
      overrides[app.id] = paths.length > 0 ? { mode, names, paths } : { mode, names };
    }
    set({ overrides, ...derive(overrides) });
    await persist(overrides);
  },
  // Stored name sets go stale when an app's known process set grows (richer
  // registry scan, running processes folded in). Re-sync overridden apps with
  // the freshly built lists so the user's earlier choice covers the full app.
  // MONOTONIC — union, never replace: today's list can be smaller than what
  // the user locked in (a helper folded in from the running list isn't
  // running right now), and dropping those names would silently unroute them
  // next time they run. Names only shrink via an explicit re-toggle
  // (setOverride) or reset.
  // Paths follow the same rule — this is also how an override saved before
  // paths existed picks up its app's directory.
  refreshOverrideNames: async (apps) => {
    const overrides = { ...get().overrides };
    let changed = false;
    for (const app of apps) {
      const o = overrides[app.id];
      if (!o) continue;
      const extra = (app.processNames ?? []).filter((n) => !o.names.includes(n));
      const known = o.paths ?? [];
      const extraPaths = (app.paths ?? []).filter((p) => !known.includes(p));
      if (extra.length === 0 && extraPaths.length === 0) continue;
      const next: AppOverride = { ...o, names: [...o.names, ...extra] };
      if (known.length + extraPaths.length > 0) next.paths = [...known, ...extraPaths];
      overrides[app.id] = next;
      changed = true;
    }
    if (!changed) return;
    set({ overrides, ...derive(overrides) });
    await persist(overrides);
  },
  resetOverrides: async () => {
    set({ overrides: {}, ...derive({}) });
    await persist({});
  },
}));
