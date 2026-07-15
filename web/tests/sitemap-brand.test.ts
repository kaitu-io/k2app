import { describe, it, expect, vi, afterEach } from 'vitest';

// sitemap.ts → brand-server.ts → `server-only`, a side-effect module that
// throws outside RSC. Same stub as request-pathname.test.ts.
vi.mock('server-only', () => ({}));

// Velite posts: one kaitu-only, one both — mirrors the mock pattern used by
// tests/content-pages.test.ts.
vi.mock('#velite', () => ({
  posts: [
    { slug: 'cn-guide', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'kaitu' },
    { slug: 'k2/protocol', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'both' },
  ],
}));

// vi.hoisted: vi.mock factories are hoisted above module-level consts, so the
// mock fn must be created in a hoisted block to be referenceable inside them.
const { findMock } = vi.hoisted(() => ({ findMock: vi.fn().mockResolvedValue({ docs: [] }) }));
vi.mock('payload', () => ({ getPayload: vi.fn().mockResolvedValue({ find: findMock }) }));
vi.mock('@payload-config', () => ({ default: {} }));

import sitemap from '../src/app/sitemap';

afterEach(() => {
  vi.unstubAllEnvs();
  findMock.mockClear();
});

describe('sitemap — baked brand isolation', () => {
  it('overleap build: only overleap.io URLs, only overleap locales, no kaitu-only content', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.length).toBeGreaterThan(0);
    for (const e of entries) {
      expect(e.url).toMatch(/^https:\/\/overleap\.io/);
      expect(e.url).not.toMatch(/zh-CN|zh-TW|zh-HK/);
    }
    expect(entries.some((e) => e.url.includes('/cn-guide'))).toBe(false);
    expect(entries.some((e) => e.url.includes('/k2/protocol'))).toBe(true);
  });

  it('overleap build: Payload query filters on showOnOverleap', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    await sitemap();
    const where = findMock.mock.calls[0][0].where;
    expect(JSON.stringify(where)).toContain('showOnOverleap');
    expect(JSON.stringify(where)).not.toContain('showOnKaitu');
  });

  it('kaitu build: only kaitu.io URLs and Payload filters on showOnKaitu', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    for (const e of entries) {
      expect(e.url).toMatch(/^https:\/\/kaitu\.io/);
    }
    expect(JSON.stringify(findMock.mock.calls[0][0].where)).toContain('showOnKaitu');
  });
});
