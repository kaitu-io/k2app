import { describe, test, expect, beforeEach, vi } from 'vitest';
import { useAppRoutesStore, migrateLegacyKey, STORAGE_KEY, OLD_STORAGE_KEY, OLD_MARKER } from './app-routes.store';
import { classifyApps } from '../services/classify-apps';
vi.mock('../services/classify-apps', () => ({ classifyApps: vi.fn() }));

function installStorageMock(): Map<string, unknown> {
  const m = new Map<string, unknown>();
  (window as any)._platform = {
    storage: {
      get: async (k: string) => (m.has(k) ? m.get(k) : null),
      set: async (k: string, v: unknown) => { m.set(k, v); },
      remove: async (k: string) => { m.delete(k); },
      has: async (k: string) => m.has(k),
      clear: async () => { m.clear(); },
      keys: async () => [...m.keys()],
    },
  };
  return m;
}

describe('app-routes.store (Plan B)', () => {
  let store: Map<string, unknown>;
  beforeEach(() => {
    store = installStorageMock();
    useAppRoutesStore.setState({ forceProxy: [], forceDirect: [], loaded: false }, false);
  });

  test('initial state: empty force lists, not loaded', () => {
    const s = useAppRoutesStore.getState();
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirect).toEqual([]);
    expect(s.loaded).toBe(false);
  });

  test('setForceProxy dedups + persists to _platform.storage under STORAGE_KEY', async () => {
    await useAppRoutesStore.getState().setForceProxy(['a', 'b', 'a']);
    expect(useAppRoutesStore.getState().forceProxy).toEqual(['a', 'b']);
    const saved = store.get(STORAGE_KEY) as any;
    expect(saved).toMatchObject({ v: 1, forceProxy: ['a', 'b'], forceDirect: [] });
  });

  test('load() migrates away the legacy keys from _platform.storage', async () => {
    store.set(OLD_STORAGE_KEY, { v: 1, entries: [{ kind: 'process', names: ['Steam.app'] }] });
    store.set(OLD_MARKER, '2026-01-01T00:00:00Z');
    await useAppRoutesStore.getState().load();
    expect(store.has(OLD_STORAGE_KEY)).toBe(false);
    expect(store.has(OLD_MARKER)).toBe(false);
    expect(useAppRoutesStore.getState().loaded).toBe(true);
  });

  test('load() hydrates force lists from STORAGE_KEY', async () => {
    store.set(STORAGE_KEY, { v: 1, forceProxy: ['x'], forceDirect: ['y'] });
    await useAppRoutesStore.getState().load();
    const s = useAppRoutesStore.getState();
    expect(s.forceProxy).toEqual(['x']);
    expect(s.forceDirect).toEqual(['y']);
    expect(s.loaded).toBe(true);
  });

  test('migrateLegacyKey is a no-op when no _platform.storage', async () => {
    (window as any)._platform = undefined;
    await expect(migrateLegacyKey()).resolves.toBeUndefined();
  });
});

describe('app-routes classify cache + toggles', () => {
  beforeEach(() => {
    installStorageMock();
    useAppRoutesStore.setState({ forceProxy: [], forceDirect: [], classifications: new Map(), loaded: true });
    (classifyApps as any).mockReset();
  });

  test('classifyInstalled stores the map', async () => {
    (classifyApps as any).mockResolvedValue(new Map([['a', 'direct'], ['b', 'proxy']]));
    await useAppRoutesStore.getState().classifyInstalled('cn', [
      { id: 'a', label: 'A', processNames: ['A'] },
      { id: 'b', label: 'B', processNames: ['B'] },
    ]);
    expect(useAppRoutesStore.getState().classifications.get('a')).toBe('direct');
  });

  // overrides store PROCESS NAMES (engine match.apps), NOT the app id.
  test('setOverride(direct) stores all process names + is exclusive with proxy', async () => {
    const steam = { id: '/Applications/Steam.app', processNames: ['Steam', 'steamwebhelper'] };
    await useAppRoutesStore.getState().setOverride(steam, 'proxy');
    await useAppRoutesStore.getState().setOverride(steam, 'direct');
    expect(useAppRoutesStore.getState().forceDirect).toEqual(
      expect.arrayContaining(['Steam', 'steamwebhelper']));
    expect(useAppRoutesStore.getState().forceProxy).not.toContain('Steam');
  });

  test('setOverride(default) clears all of the app process names', async () => {
    const steam = { id: '/Applications/Steam.app', processNames: ['Steam', 'steamwebhelper'] };
    await useAppRoutesStore.getState().setOverride(steam, 'direct');
    await useAppRoutesStore.getState().setOverride(steam, 'default');
    expect(useAppRoutesStore.getState().forceDirect).not.toContain('Steam');
    expect(useAppRoutesStore.getState().forceDirect).not.toContain('steamwebhelper');
    expect(useAppRoutesStore.getState().forceProxy).not.toContain('Steam');
  });

  test('resetOverrides clears both sets', async () => {
    await useAppRoutesStore.getState().setOverride({ id: 'x', processNames: ['x'] }, 'direct');
    await useAppRoutesStore.getState().resetOverrides();
    expect(useAppRoutesStore.getState().forceDirect).toEqual([]);
    expect(useAppRoutesStore.getState().forceProxy).toEqual([]);
  });
});
