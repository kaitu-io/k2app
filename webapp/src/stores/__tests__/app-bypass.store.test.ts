import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAppBypassStore } from '../app-bypass.store';

const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  keys: vi.fn(),
  clear: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  mockStorage.get.mockResolvedValue(null);
  mockStorage.set.mockResolvedValue(undefined);
  (window as any)._platform = { storage: mockStorage };
  useAppBypassStore.setState({ entries: [], loaded: false });
});

describe('app-bypass store', () => {
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
