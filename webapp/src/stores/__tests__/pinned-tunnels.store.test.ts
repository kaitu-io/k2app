/**
 * Pinned / recent tunnels store.
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/pinned-tunnels.store.test.ts
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import type { Tunnel } from '../../services/api-types';

const mockStorage = { get: vi.fn(), set: vi.fn(), remove: vi.fn() };

/** Each domain gets its own IP unless one is given. */
function tunnel(domain: string, country = 'jp', ipv4 = 'ip-' + domain.toLowerCase()): Tunnel {
  return {
    id: 1, domain, name: 'name-' + domain, protocol: 'k2v5', port: 443,
    serverUrl: 'k2v5://' + domain,
    node: { name: '', country, region: '', ipv4, ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore: 0.5,
  };
}

async function freshStore() {
  vi.resetModules();
  return import('../pinned-tunnels.store');
}

beforeEach(() => {
  (window as any)._platform = { storage: mockStorage };
  mockStorage.get.mockReset();
  mockStorage.set.mockReset().mockResolvedValue(undefined);
  mockStorage.remove.mockReset().mockResolvedValue(undefined);
});

describe('pin / unpin', () => {
  it('appends in order, lowercases, ignores duplicates, persists JSON', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.pin(tunnel('A.x', 'jp', 'ip-a.x'));
    s.pin(tunnel('b.x', 'sg'));
    s.pin(tunnel('a.x'));
    const pinned = usePinnedTunnelsStore.getState().pinned;
    expect(pinned.map(p => p.domain)).toEqual(['a.x', 'b.x']);
    expect(pinned[1]).toMatchObject({ name: 'name-b.x', country: 'sg' });
    expect(pinned.map(p => p.key)).toEqual(['ip-a.x', 'ip-b.x']);
    expect(usePinnedTunnelsStore.getState().isPinned(tunnel('a.x'))).toBe(true);
    const last = mockStorage.set.mock.calls.filter(c => c[0] === 'k2.connection.pinnedTunnels').pop()!;
    expect(JSON.parse(last[1]).map((p: { domain: string }) => p.domain)).toEqual(['a.x', 'b.x']);
  });

  it('refuses past PIN_LIMIT', async () => {
    const { usePinnedTunnelsStore, PIN_LIMIT } = await freshStore();
    expect(PIN_LIMIT).toBe(10);
    for (let i = 0; i < 12; i++) usePinnedTunnelsStore.getState().pin(tunnel(`t${i}.x`));
    expect(usePinnedTunnelsStore.getState().pinned).toHaveLength(10);
  });

  it('unpin removes and persists', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.pin(tunnel('a.x'));
    s.pin(tunnel('b.x'));
    s.unpin('ip-a.x');
    expect(usePinnedTunnelsStore.getState().pinned.map(p => p.domain)).toEqual(['b.x']);
    const last = mockStorage.set.mock.calls.filter(c => c[0] === 'k2.connection.pinnedTunnels').pop()!;
    expect(JSON.parse(last[1])).toHaveLength(1);
  });

  it('pinning removes the tunnel from recent', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.recordRecent(tunnel('a.x'));
    s.pin(tunnel('a.x'));
    expect(usePinnedTunnelsStore.getState().recent).toEqual([]);
  });
});

describe('recordRecent', () => {
  it('newest first, dedup, cap 3, skips pinned', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.pin(tunnel('p.x'));
    s.recordRecent(tunnel('a.x'));
    s.recordRecent(tunnel('b.x'));
    s.recordRecent(tunnel('p.x'));
    s.recordRecent(tunnel('c.x'));
    s.recordRecent(tunnel('A.x'));
    s.recordRecent(tunnel('d.x'));
    expect(usePinnedTunnelsStore.getState().recent.map(r => r.domain)).toEqual(['d.x', 'a.x', 'c.x']);
    const last = mockStorage.set.mock.calls.filter(c => c[0] === 'k2.connection.recentTunnels').pop()!;
    expect(JSON.parse(last[1])).toHaveLength(3);
  });
});

describe('load', () => {
  it('parses stored JSON and sets loaded', async () => {
    mockStorage.get.mockImplementation(async (k: string) =>
      k === 'k2.connection.pinnedTunnels'
        ? JSON.stringify([{ key: '1.1.1.1', domain: 'a.x', name: 'A', country: 'jp', at: 1 }, { domain: 'nokey.x' }])
        : k === 'k2.connection.recentTunnels'
          ? JSON.stringify([{ key: '2.2.2.2', domain: 'r.x', name: 'R', country: 'sg', at: 2 }])
          : null);
    const { usePinnedTunnelsStore } = await freshStore();
    await usePinnedTunnelsStore.getState().load();
    const st = usePinnedTunnelsStore.getState();
    expect(st.loaded).toBe(true);
    expect(st.pinned.map(p => p.domain)).toEqual(['a.x']);
    expect(st.recent.map(p => p.domain)).toEqual(['r.x']);
  });

  it('corrupt JSON or bad entries → empty, still loaded', async () => {
    mockStorage.get.mockImplementation(async (k: string) =>
      k === 'k2.connection.pinnedTunnels' ? '{not json' : JSON.stringify([{ nope: 1 }, 'x']));
    const { usePinnedTunnelsStore } = await freshStore();
    await usePinnedTunnelsStore.getState().load();
    const st = usePinnedTunnelsStore.getState();
    expect(st.loaded).toBe(true);
    expect(st.pinned).toEqual([]);
    expect(st.recent).toEqual([]);
  });

  it('storage throw → empty, still loaded', async () => {
    mockStorage.get.mockRejectedValue(new Error('boom'));
    const { usePinnedTunnelsStore } = await freshStore();
    await usePinnedTunnelsStore.getState().load();
    expect(usePinnedTunnelsStore.getState().loaded).toBe(true);
  });
});

describe('identity is the node IP', () => {
  it('two tunnels on one IP are one pin; either tunnel reads as pinned', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.pin(tunnel('a.x', 'jp', '9.9.9.9'));
    s.pin(tunnel('a2.x', 'jp', '9.9.9.9'));
    expect(usePinnedTunnelsStore.getState().pinned).toHaveLength(1);
    expect(usePinnedTunnelsStore.getState().isPinned(tunnel('a2.x', 'jp', '9.9.9.9'))).toBe(true);
  });

  it('same domain on a new IP is NOT the pinned node', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    usePinnedTunnelsStore.getState().pin(tunnel('a.x', 'jp', '1.1.1.1'));
    expect(usePinnedTunnelsStore.getState().isPinned(tunnel('a.x', 'jp', '7.7.7.7'))).toBe(false);
  });

  it('a tunnel without an address falls back to its domain', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    usePinnedTunnelsStore.getState().pin(tunnel('bare.x', 'jp', ''));
    expect(usePinnedTunnelsStore.getState().pinned[0].key).toBe('domain:bare.x');
  });

  it('recent dedups by IP', async () => {
    const { usePinnedTunnelsStore } = await freshStore();
    const s = usePinnedTunnelsStore.getState();
    s.recordRecent(tunnel('a.x', 'jp', '9.9.9.9'));
    s.recordRecent(tunnel('a2.x', 'jp', '9.9.9.9'));
    expect(usePinnedTunnelsStore.getState().recent.map(r => r.domain)).toEqual(['a2.x']);
  });
});

describe('resolveEntry', () => {
  it('prefers the last-used tunnel on the node, else any tunnel on it, else undefined', async () => {
    const { resolveEntry } = await freshStore();
    const a = tunnel('a.x', 'jp', '9.9.9.9');
    const a2 = tunnel('a2.x', 'jp', '9.9.9.9');
    const other = tunnel('b.x', 'jp', '8.8.8.8');
    const entry = { key: '9.9.9.9', domain: 'a2.x' };
    expect(resolveEntry(entry, [a, a2, other])).toBe(a2);
    expect(resolveEntry(entry, [a, other])).toBe(a);
    expect(resolveEntry(entry, [other])).toBeUndefined();
    expect(resolveEntry(entry, [{ ...a2, serverUrl: undefined }])).toBeUndefined();
  });

  it('IP changed (domain kept) → unresolved', async () => {
    const { resolveEntry } = await freshStore();
    expect(resolveEntry({ key: '1.1.1.1', domain: 'a.x' }, [tunnel('a.x', 'jp', '7.7.7.7')])).toBeUndefined();
  });
});
