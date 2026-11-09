/**
 * Pinned / recent tunnels store.
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/pinned-tunnels.store.test.ts
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import type { Tunnel } from '../../services/api-types';

const mockStorage = { get: vi.fn(), set: vi.fn(), remove: vi.fn() };

function tunnel(domain: string, country = 'jp'): Tunnel {
  return {
    id: 1, domain, name: 'name-' + domain, protocol: 'k2v5', port: 443,
    serverUrl: 'k2v5://' + domain,
    node: { name: '', country, region: '', ipv4: '', ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
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
    s.pin(tunnel('A.x'));
    s.pin(tunnel('b.x', 'sg'));
    s.pin(tunnel('a.x'));
    const pinned = usePinnedTunnelsStore.getState().pinned;
    expect(pinned.map(p => p.domain)).toEqual(['a.x', 'b.x']);
    expect(pinned[1]).toMatchObject({ name: 'name-b.x', country: 'sg' });
    expect(usePinnedTunnelsStore.getState().isPinned('A.X')).toBe(true);
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
    s.unpin('A.x');
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
        ? JSON.stringify([{ domain: 'a.x', name: 'A', country: 'jp', at: 1 }])
        : k === 'k2.connection.recentTunnels'
          ? JSON.stringify([{ domain: 'r.x', name: 'R', country: 'sg', at: 2 }])
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
