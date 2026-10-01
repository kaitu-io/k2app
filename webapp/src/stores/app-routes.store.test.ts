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

function resetStore(loaded: boolean) {
  useAppRoutesStore.setState(
    { overrides: {}, forceProxy: [], forceDirect: [], forceProxyPaths: [], forceDirectPaths: [], classifications: new Map(), loaded },
    false,
  );
}

describe('app-routes.store (per-app overrides)', () => {
  let store: Map<string, unknown>;
  beforeEach(() => {
    store = installStorageMock();
    resetStore(false);
  });

  test('initial state: no overrides, empty derived lists, not loaded', () => {
    const s = useAppRoutesStore.getState();
    expect(s.overrides).toEqual({});
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirect).toEqual([]);
    expect(s.loaded).toBe(false);
  });

  test('load() migrates away the legacy keys from _platform.storage', async () => {
    store.set(OLD_STORAGE_KEY, { v: 1, entries: [{ kind: 'process', names: ['Steam.app'] }] });
    store.set(OLD_MARKER, '2026-01-01T00:00:00Z');
    await useAppRoutesStore.getState().load();
    expect(store.has(OLD_STORAGE_KEY)).toBe(false);
    expect(store.has(OLD_MARKER)).toBe(false);
    expect(useAppRoutesStore.getState().loaded).toBe(true);
  });

  test('load() hydrates overrides + derived lists from a v2 payload', async () => {
    store.set(STORAGE_KEY, {
      v: 2,
      apps: {
        'C:\\Apps\\X': { mode: 'proxy', names: ['x.exe'] },
        'C:\\Apps\\Y': { mode: 'direct', names: ['y.exe', 'y_helper.exe'] },
      },
    });
    await useAppRoutesStore.getState().load();
    const s = useAppRoutesStore.getState();
    expect(s.overrides['C:\\Apps\\Y']?.mode).toBe('direct');
    expect(s.forceProxy).toEqual(['x.exe']);
    expect(s.forceDirect).toEqual(expect.arrayContaining(['y.exe', 'y_helper.exe']));
    expect(s.loaded).toBe(true);
  });

  // v1 carried flat name lists with no app identity — it is discarded, never
  // guessed into per-app entries.
  test('load() discards a v1 payload', async () => {
    store.set(STORAGE_KEY, { v: 1, forceProxy: ['x'], forceDirect: ['y'] });
    await useAppRoutesStore.getState().load();
    const s = useAppRoutesStore.getState();
    expect(s.overrides).toEqual({});
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirect).toEqual([]);
    expect(s.loaded).toBe(true);
  });

  test('migrateLegacyKey is a no-op when no _platform.storage', async () => {
    (window as any)._platform = undefined;
    await expect(migrateLegacyKey()).resolves.toBeUndefined();
  });
});

describe('app-routes classify cache + toggles', () => {
  let store: Map<string, unknown>;
  beforeEach(() => {
    store = installStorageMock();
    resetStore(true);
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

  test('setOverride(direct) keys by app id, derives all process names, exclusive with proxy', async () => {
    const steam = { id: '/Applications/Steam.app', processNames: ['Steam', 'steamwebhelper'] };
    await useAppRoutesStore.getState().setOverride(steam, 'proxy');
    await useAppRoutesStore.getState().setOverride(steam, 'direct');
    const s = useAppRoutesStore.getState();
    expect(s.overrides[steam.id]).toEqual({ mode: 'direct', names: ['Steam', 'steamwebhelper'] });
    expect(s.forceDirect).toEqual(expect.arrayContaining(['Steam', 'steamwebhelper']));
    expect(s.forceProxy).toEqual([]);
    expect(store.get(STORAGE_KEY)).toMatchObject({
      v: 2,
      apps: { [steam.id]: { mode: 'direct' } },
    });
  });

  test('setOverride(default) removes the app entry and its derived names', async () => {
    const steam = { id: '/Applications/Steam.app', processNames: ['Steam', 'steamwebhelper'] };
    await useAppRoutesStore.getState().setOverride(steam, 'direct');
    await useAppRoutesStore.getState().setOverride(steam, 'default');
    const s = useAppRoutesStore.getState();
    expect(s.overrides).toEqual({});
    expect(s.forceDirect).toEqual([]);
  });

  // A shared helper basename (crashpad_handler.exe & co) must not bleed one
  // app's override onto another app: identity is the app id.
  test('two apps sharing a helper name keep independent overrides', async () => {
    const a = { id: 'C:\\Apps\\A', processNames: ['a.exe', 'crashpad_handler.exe'] };
    const b = { id: 'C:\\Apps\\B', processNames: ['b.exe', 'crashpad_handler.exe'] };
    await useAppRoutesStore.getState().setOverride(a, 'direct');
    const s = useAppRoutesStore.getState();
    expect(s.overrides[a.id]?.mode).toBe('direct');
    expect(s.overrides[b.id]).toBeUndefined();
    // The engine still matches by name: the shared helper IS in forceDirect.
    expect(s.forceDirect).toEqual(expect.arrayContaining(['a.exe', 'crashpad_handler.exe']));
  });

  test('refreshOverrideNames grows a stale name set and re-persists', async () => {
    const app = { id: 'C:\\Program Files\\Tencent\\Weixin', processNames: ['Weixin.exe'] };
    await useAppRoutesStore.getState().setOverride(app, 'direct');
    const richer = { ...app, processNames: ['Weixin.exe', 'WeChatAppEx.exe'] };
    await useAppRoutesStore.getState().refreshOverrideNames([richer]);
    const s = useAppRoutesStore.getState();
    expect(s.overrides[app.id]?.names).toEqual(['Weixin.exe', 'WeChatAppEx.exe']);
    expect(s.forceDirect).toContain('WeChatAppEx.exe');
    expect(store.get(STORAGE_KEY)).toMatchObject({
      v: 2,
      apps: { [app.id]: { names: ['Weixin.exe', 'WeChatAppEx.exe'] } },
    });
  });

  test('refreshOverrideNames ignores apps without overrides and empty name sets', async () => {
    const app = { id: 'C:\\Apps\\A', processNames: ['a.exe'] };
    await useAppRoutesStore.getState().setOverride(app, 'proxy');
    const persistedBefore = store.get(STORAGE_KEY);
    await useAppRoutesStore.getState().refreshOverrideNames([
      { id: 'C:\\Apps\\Other', processNames: ['other.exe'] }, // no override
      { id: app.id, processNames: [] }, // empty set must not wipe names
    ]);
    const s = useAppRoutesStore.getState();
    expect(s.overrides[app.id]?.names).toEqual(['a.exe']);
    expect(store.get(STORAGE_KEY)).toBe(persistedBefore); // untouched (no re-persist)
  });

  // Monotonic: a helper the user locked in while it was running must survive
  // a later page load where it happens not to be running — the refreshed
  // (smaller) list unions in, never replaces.
  test('refreshOverrideNames never shrinks a stored name set', async () => {
    const app = { id: 'C:\\Apps\\A', processNames: ['a.exe', 'a_helper.exe'] };
    await useAppRoutesStore.getState().setOverride(app, 'direct');
    const persistedBefore = store.get(STORAGE_KEY);
    await useAppRoutesStore.getState().refreshOverrideNames([
      { id: app.id, processNames: ['a.exe'] }, // helper not running today
    ]);
    const s = useAppRoutesStore.getState();
    expect(s.overrides[app.id]?.names).toEqual(['a.exe', 'a_helper.exe']);
    expect(s.forceDirect).toEqual(expect.arrayContaining(['a.exe', 'a_helper.exe']));
    expect(store.get(STORAGE_KEY)).toBe(persistedBefore); // no change → no re-persist
  });

  // macOS: the app is its bundle directory. The override stores it and the
  // derived lists feed match.app_paths; the main executable name rides along
  // as the fallback identity.
  test('setOverride stores the app directory and derives path lists per mode', async () => {
    const doubao = { id: '/Applications/Doubao.app', processNames: ['Doubao'], paths: ['/Applications/Doubao.app'] };
    const capcut = { id: '/Applications/VideoFusion-macOS.app', processNames: ['VideoFusion-macOS'], paths: ['/Applications/VideoFusion-macOS.app'] };
    await useAppRoutesStore.getState().setOverride(doubao, 'direct');
    await useAppRoutesStore.getState().setOverride(capcut, 'proxy');
    const s = useAppRoutesStore.getState();
    expect(s.overrides[doubao.id]).toEqual({ mode: 'direct', names: ['Doubao'], paths: ['/Applications/Doubao.app'] });
    expect(s.forceDirectPaths).toEqual(['/Applications/Doubao.app']);
    expect(s.forceProxyPaths).toEqual(['/Applications/VideoFusion-macOS.app']);
    expect(s.forceDirect).toEqual(['Doubao']);
    expect(store.get(STORAGE_KEY)).toMatchObject({
      v: 2,
      apps: { [doubao.id]: { paths: ['/Applications/Doubao.app'] } },
    });

    // Flipping the mode moves the directory with it; clearing removes it.
    await useAppRoutesStore.getState().setOverride(doubao, 'proxy');
    expect(useAppRoutesStore.getState().forceDirectPaths).toEqual([]);
    expect(useAppRoutesStore.getState().forceProxyPaths).toEqual(
      expect.arrayContaining(['/Applications/Doubao.app', '/Applications/VideoFusion-macOS.app']),
    );
    await useAppRoutesStore.getState().setOverride(doubao, 'default');
    expect(useAppRoutesStore.getState().forceProxyPaths).toEqual(['/Applications/VideoFusion-macOS.app']);
  });

  // An app with a directory but no declared executable is still routable.
  test('setOverride keeps a paths-only app', async () => {
    const odd = { id: '/Applications/Odd.app', processNames: [], paths: ['/Applications/Odd.app'] };
    await useAppRoutesStore.getState().setOverride(odd, 'direct');
    const s = useAppRoutesStore.getState();
    expect(s.overrides[odd.id]).toEqual({ mode: 'direct', names: [], paths: ['/Applications/Odd.app'] });
    expect(s.forceDirectPaths).toEqual(['/Applications/Odd.app']);
  });

  // Name-only apps (Windows, Linux, standalone binaries) keep the old shape:
  // no `paths` key appears in storage.
  test('setOverride without paths stores names only', async () => {
    const app = { id: 'C:\\Apps\\A', processNames: ['a.exe'] };
    await useAppRoutesStore.getState().setOverride(app, 'direct');
    expect(useAppRoutesStore.getState().overrides[app.id]).toEqual({ mode: 'direct', names: ['a.exe'] });
    expect(useAppRoutesStore.getState().forceDirectPaths).toEqual([]);
  });

  // An override saved by a version that only knew names must pick up the
  // app's directory on the next list refresh — without losing the names it
  // already locked in (they still cover the app if it runs from elsewhere).
  test('refreshOverrideNames adds the directory to an override saved before paths existed', async () => {
    const id = '/Applications/Doubao.app';
    store.set(STORAGE_KEY, { v: 2, apps: { [id]: { mode: 'direct', names: ['Doubao', 'Doubao Browser Helper'] } } });
    await useAppRoutesStore.getState().load();
    expect(useAppRoutesStore.getState().forceDirectPaths).toEqual([]);

    await useAppRoutesStore.getState().refreshOverrideNames([{ id, processNames: ['Doubao'], paths: [id] }]);
    const s = useAppRoutesStore.getState();
    expect(s.overrides[id]).toEqual({ mode: 'direct', names: ['Doubao', 'Doubao Browser Helper'], paths: [id] });
    expect(s.forceDirectPaths).toEqual([id]);
    expect(store.get(STORAGE_KEY)).toMatchObject({ v: 2, apps: { [id]: { paths: [id] } } });

    // Idempotent: a second refresh with the same list does not re-persist.
    const persisted = store.get(STORAGE_KEY);
    await useAppRoutesStore.getState().refreshOverrideNames([{ id, processNames: ['Doubao'], paths: [id] }]);
    expect(store.get(STORAGE_KEY)).toBe(persisted);
  });

  test('load restores stored paths into the derived lists', async () => {
    const id = '/Applications/Doubao.app';
    store.set(STORAGE_KEY, { v: 2, apps: { [id]: { mode: 'proxy', names: ['Doubao'], paths: [id] } } });
    await useAppRoutesStore.getState().load();
    expect(useAppRoutesStore.getState().forceProxyPaths).toEqual([id]);
    expect(useAppRoutesStore.getState().forceDirectPaths).toEqual([]);
  });

  test('resetOverrides clears everything', async () => {
    await useAppRoutesStore.getState().setOverride({ id: 'x', processNames: ['x'] }, 'direct');
    await useAppRoutesStore.getState().resetOverrides();
    const s = useAppRoutesStore.getState();
    expect(s.overrides).toEqual({});
    expect(s.forceDirect).toEqual([]);
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirectPaths).toEqual([]);
    expect(s.forceProxyPaths).toEqual([]);
  });
});
