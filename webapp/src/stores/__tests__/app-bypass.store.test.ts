import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAppBypassStore, __resetAppBypassInflightForTests } from '../app-bypass.store';
import { useConfigStore } from '../config.store';

const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  keys: vi.fn(),
  clear: vi.fn(),
};

const mockListInstalled = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  mockStorage.get.mockResolvedValue(null);
  mockStorage.set.mockResolvedValue(undefined);
  mockListInstalled.mockResolvedValue([]);
  (window as any)._platform = {
    storage: mockStorage,
    appList: { listInstalled: mockListInstalled },
  };
  useAppBypassStore.setState({
    entries: [],
    loaded: false,
    candidates: [],
    candidatesLoadedAt: 0,
    candidatesLoading: false,
    candidatesError: null,
  });
  __resetAppBypassInflightForTests();
  // Default: no country → dispatcher resolves to noop unless a test sets it.
  useConfigStore.setState({ country: null } as any);
});

describe('app-bypass store (mobile / local-storage path)', () => {
  it('load() reads from _platform.storage and parses v1 shape', async () => {
    mockStorage.get.mockResolvedValueOnce({
      v: 1,
      entries: [{ id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'], addedAt: 1 }],
    });
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
    expect(useAppBypassStore.getState().loaded).toBe(true);
    expect(mockStorage.get).toHaveBeenCalledWith('k2.advanced.app_bypass');
  });

  it('load() tolerates missing/corrupt data', async () => {
    mockStorage.get.mockResolvedValueOnce(null);
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toEqual([]);
    expect(useAppBypassStore.getState().loaded).toBe(true);
  });

  it('load() tolerates wrong schema version', async () => {
    mockStorage.get.mockResolvedValueOnce({ v: 99, entries: [{}] });
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('add() persists and updates state', async () => {
    useAppBypassStore.setState({ entries: [], loaded: true });
    await useAppBypassStore.getState().add({
      id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'],
    });
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
    expect(useAppBypassStore.getState().entries[0].addedAt).toBeGreaterThan(0);
    expect(mockStorage.set).toHaveBeenCalledWith(
      'k2.advanced.app_bypass',
      expect.objectContaining({ v: 1, entries: expect.any(Array) })
    );
  });

  it('add() de-duplicates by id', async () => {
    useAppBypassStore.setState({
      entries: [{ id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().add({
      id: 'com.test', label: 'Test 2', kind: 'package', names: ['com.test'],
    });
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
  });

  it('remove() filters by id and persists', async () => {
    useAppBypassStore.setState({
      entries: [
        { id: 'a', label: 'A', kind: 'package', names: ['a'], addedAt: 1 },
        { id: 'b', label: 'B', kind: 'package', names: ['b'], addedAt: 2 },
      ],
      loaded: true,
    });
    await useAppBypassStore.getState().remove('a');
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['b']);
    expect(mockStorage.set).toHaveBeenCalled();
  });

  it('clear() empties entries and persists', async () => {
    useAppBypassStore.setState({
      entries: [{ id: 'a', label: 'A', kind: 'package', names: ['a'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().clear();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('rescan() updates names of a single entry', async () => {
    useAppBypassStore.setState({
      entries: [{ id: '/Apps/Chrome.app', label: 'Chrome', kind: 'process', names: ['Chrome'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().rescan('/Apps/Chrome.app', ['Chrome', 'Chrome Helper']);
    expect(useAppBypassStore.getState().entries[0].names).toEqual(['Chrome', 'Chrome Helper']);
  });

  it('save failure logs and keeps in-memory state unchanged', async () => {
    mockStorage.set.mockRejectedValueOnce(new Error('disk full'));
    useAppBypassStore.setState({ entries: [], loaded: true });
    await expect(
      useAppBypassStore.getState().add({ id: 'a', label: 'A', kind: 'package', names: ['a'] })
    ).rejects.toThrow();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });
});

// App Bypass v2 retired client-side regional detection — the Go engine now
// owns it via region presets shipped through k2-rules. ClientConfig.app_bypass
// passes the region + custom adds at every connect.

describe('refreshCandidates', () => {
  it('on Android (listInstalled provider) calls listInstalled exactly once and caches', async () => {
    mockListInstalled.mockResolvedValueOnce([
      { packageName: 'com.x', label: 'X', iconUrl: 'kaitu-icon://package/com.x' },
    ]);
    await useAppBypassStore.getState().refreshCandidates();
    expect(mockListInstalled).toHaveBeenCalledTimes(1);
    const s = useAppBypassStore.getState();
    expect(s.candidates).toEqual([
      { kind: 'package', id: 'com.x', label: 'X', iconUrl: 'kaitu-icon://package/com.x' },
    ]);
    expect(s.candidatesLoadedAt).toBeGreaterThan(0);
    expect(s.candidatesLoading).toBe(false);
    expect(s.candidatesError).toBeNull();
  });

  it('on desktop (listRunning provider only) calls listRunning and maps to process kind', async () => {
    const mockListRunning = vi.fn().mockResolvedValue([
      { id: '/Apps/Foo.app', label: 'Foo', processNames: ['Foo'], iconUrl: undefined },
    ]);
    (window as any)._platform = {
      storage: mockStorage,
      appList: { listRunning: mockListRunning },
    };
    await useAppBypassStore.getState().refreshCandidates();
    expect(mockListRunning).toHaveBeenCalledTimes(1);
    expect(useAppBypassStore.getState().candidates).toEqual([
      { kind: 'process', id: '/Apps/Foo.app', label: 'Foo', processNames: ['Foo'], iconUrl: undefined },
    ]);
  });

  it('preserves stale candidates during in-flight refresh', async () => {
    useAppBypassStore.setState({
      candidates: [{ kind: 'package', id: 'com.cached', label: 'Cached' }],
      candidatesLoadedAt: 100,
    });
    let resolveIpc: (v: any) => void = () => {};
    mockListInstalled.mockReturnValueOnce(new Promise((r) => { resolveIpc = r; }));
    const p = useAppBypassStore.getState().refreshCandidates();
    expect(useAppBypassStore.getState().candidatesLoading).toBe(true);
    expect(useAppBypassStore.getState().candidates).toHaveLength(1);
    expect(useAppBypassStore.getState().candidates[0].id).toBe('com.cached');
    resolveIpc([{ packageName: 'com.new', label: 'New' }]);
    await p;
    expect(useAppBypassStore.getState().candidatesLoading).toBe(false);
    expect(useAppBypassStore.getState().candidates).toHaveLength(1);
    expect(useAppBypassStore.getState().candidates[0].id).toBe('com.new');
  });

  it('dedups concurrent calls (returns same Promise, IPC fired once)', async () => {
    let resolveIpc: (v: any) => void = () => {};
    mockListInstalled.mockReturnValueOnce(new Promise((r) => { resolveIpc = r; }));
    const p1 = useAppBypassStore.getState().refreshCandidates();
    const p2 = useAppBypassStore.getState().refreshCandidates();
    // Resolve + await BEFORE asserting identity so a failure doesn't leak an
    // unresolved in-flight Promise into the next test (which would hit the
    // dedup-guard and hang on a never-resolving cached promise).
    resolveIpc([]);
    await Promise.all([p1, p2]);
    expect(mockListInstalled).toHaveBeenCalledTimes(1);
    expect(p1).toBe(p2);
  });

  it('IPC failure preserves cached candidates and sets i18n error key', async () => {
    useAppBypassStore.setState({
      candidates: [{ kind: 'package', id: 'com.cached', label: 'Cached' }],
    });
    mockListInstalled.mockRejectedValueOnce(new Error('IPC boom'));
    await useAppBypassStore.getState().refreshCandidates();
    const s = useAppBypassStore.getState();
    expect(s.candidates).toHaveLength(1);
    expect(s.candidates[0].id).toBe('com.cached');
    expect(s.candidatesError).toBe('dashboard:appBypass.loadFailed');
    expect(s.candidatesLoading).toBe(false);
  });

  it('no appList provider resolves cleanly with empty candidates', async () => {
    (window as any)._platform = { storage: mockStorage }; // no appList
    await useAppBypassStore.getState().refreshCandidates();
    const s = useAppBypassStore.getState();
    expect(s.candidates).toEqual([]);
    expect(s.candidatesError).toBeNull();
    expect(s.candidatesLoading).toBe(false);
  });

  it('on Android single PackageManager call populates candidates only', async () => {
    mockListInstalled.mockResolvedValueOnce([
      { packageName: 'com.tencent.mm', label: '微信' },
    ]);
    await useAppBypassStore.getState().refreshCandidates();
    expect(mockListInstalled).toHaveBeenCalledTimes(1);
    expect(useAppBypassStore.getState().candidates).toHaveLength(1);
    expect(useAppBypassStore.getState().candidates[0].id).toBe('com.tencent.mm');
  });
});
