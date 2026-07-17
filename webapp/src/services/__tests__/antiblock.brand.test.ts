import { describe, it, expect } from 'vitest';
import { CDN_SOURCES, DEFAULT_ENTRY, resolveEntry } from '../antiblock';
import { brandConfig } from '../../brands';

describe('antiblock CDN sources brand derivation', () => {
  it('derives from the active brand config', () => {
    expect(CDN_SOURCES).toEqual(brandConfig.antiblockCdnSources);
  });
  it('empty source list resolves straight to DEFAULT_ENTRY (no CDN race)', async () => {
    if (brandConfig.antiblockCdnSources.length > 0) return; // kaitu run: n/a
    localStorage.removeItem('k2_entry_url');
    await expect(resolveEntry()).resolves.toBe(DEFAULT_ENTRY);
  });
});
