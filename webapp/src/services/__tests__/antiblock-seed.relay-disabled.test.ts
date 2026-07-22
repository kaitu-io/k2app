import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../entry-pool', () => ({ addNodes: vi.fn() }));
vi.mock('../antiblock-crypto', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../antiblock-crypto')>();
  return { ...actual, loadJsonp: vi.fn() };
});

import { loadJsonp } from '../antiblock-crypto';
import { addNodes } from '../entry-pool';
import { bootstrapAntiblockSeed, ENTRY_KEY, SEEDED_KEY, CURSOR_KEY } from '../antiblock-seed';
import { EMBEDDED_SEED } from '../antiblock-seed-embedded';

// RELAY_ENABLED=false（真实 flag 值）：种子模块完全 no-op——不喂节点、不拉 CDN、
// 不写 localStorage（不得用 embedded CloudFront entry 覆盖直连的 k2_entry_url 缓存）。
describe('bootstrapAntiblockSeed with relay disabled (kill-switch)', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it('is a complete no-op: no node feed, no CDN fetch, no localStorage writes', async () => {
    await bootstrapAntiblockSeed();
    expect(addNodes).not.toHaveBeenCalled();
    expect(loadJsonp).not.toHaveBeenCalled();
    expect(localStorage.getItem(ENTRY_KEY)).toBeNull();
    expect(localStorage.getItem(SEEDED_KEY)).toBeNull();
    expect(localStorage.getItem(CURSOR_KEY)).toBeNull();
  });

  it('does not clobber an existing direct-entry cache', async () => {
    localStorage.setItem(ENTRY_KEY, 'https://cdn-resolved.example');
    await bootstrapAntiblockSeed();
    expect(localStorage.getItem(ENTRY_KEY)).toBe('https://cdn-resolved.example');
  });

  it('purges a poisoned entry cache left by the relay-era embedded clobber', async () => {
    localStorage.setItem(ENTRY_KEY, EMBEDDED_SEED.entries[0]!);
    await bootstrapAntiblockSeed();
    expect(localStorage.getItem(ENTRY_KEY)).toBeNull();
  });

  it('purge is value-matched: any embedded entry is purged, nothing else', async () => {
    for (const entry of EMBEDDED_SEED.entries) {
      localStorage.setItem(ENTRY_KEY, entry);
      await bootstrapAntiblockSeed();
      expect(localStorage.getItem(ENTRY_KEY)).toBeNull();
    }
  });
});
