import { describe, test, expect, beforeEach } from 'vitest';
import { useAppRoutesStore, migrateLegacyKey, STORAGE_KEY, OLD_STORAGE_KEY, OLD_MARKER } from './app-routes.store';

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
