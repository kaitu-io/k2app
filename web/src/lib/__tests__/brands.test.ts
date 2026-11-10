import { describe, it, expect, vi, afterEach } from 'vitest';
import { brandById, KAITU, OVERLEAP, siteBrand } from '../brands';

describe('brandById (cross-brand registry for /manager)', () => {
  it('returns KAITU for "kaitu"', () => {
    expect(brandById('kaitu')).toBe(KAITU);
  });

  it('returns OVERLEAP for "overleap"', () => {
    expect(brandById('overleap')).toBe(OVERLEAP);
  });

  it('every brand has a distinct display wordmark (manager BrandBadge / BrandPicker)', () => {
    expect(brandById('kaitu').wordmark).toBe('开途');
    expect(brandById('overleap').wordmark).toBe('Overleap');
  });
});

describe('siteBrand', () => {
  afterEach(() => vi.unstubAllEnvs());

  it('is always KAITU — this site serves kaitu.io only', () => {
    expect(siteBrand()).toBe(KAITU);
  });

  it('ignores a stale NEXT_PUBLIC_BRAND (next.config.ts fails such a build instead)', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect(siteBrand()).toBe(KAITU);
  });
});

describe('brand configs', () => {
  it('KAITU serves Chinese plus the languages of foreigners in China', () => {
    expect(KAITU.allowedLocales).toEqual(['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'ko', 'ja', 'ru']);
    expect(KAITU.defaultLocale).toBe('zh-CN');
    expect(KAITU.taglineZh).toBe('愿上帝为你开路');
  });

  it('brand base URLs are distinct HTTPS origins', () => {
    expect(KAITU.baseUrl).toBe('https://www.kaitu.io');
    expect(OVERLEAP.baseUrl).toBe('https://overleap.io');
  });

  it('brand ids are distinct', () => {
    expect(KAITU.id).toBe('kaitu');
    expect(OVERLEAP.id).toBe('overleap');
  });

  // Legal documents sign as the operating company (2026-10-29: Wordgate LLC,
  // same as overleap.io) — not the other brand's name.
  it('legal documents sign as Wordgate LLC', () => {
    expect(KAITU.legalName).toBe('Wordgate LLC');
  });

  it('GA + in-house chat are configured', () => {
    expect(KAITU.gaMeasurementId).toBe('G-EH2PY4S0CX');
    expect(KAITU.chatEnabled).toBe(true);
  });

  it('cdn config carries the kaitu bases and artifact prefix', () => {
    expect(KAITU.cdn.artifactPrefix).toBe('Kaitu');
    expect(KAITU.cdn.desktopBases[0]).toBe('https://dl.kaitu.io/kaitu/desktop');
  });

  it('productName drives user-facing product badges', () => {
    expect(KAITU.productName).toBe('开途 VPN');
  });
});

describe('localizedWordmark', () => {
  it('Chinese locales use the Chinese wordmark alone; others pair it with the latin name', async () => {
    const { localizedWordmark } = await import('../brands');
    expect(localizedWordmark(KAITU, 'zh-CN')).toBe(KAITU.wordmark);
    expect(localizedWordmark(KAITU, 'zh-HK')).toBe(KAITU.wordmark);
    for (const l of ['en-US', 'ko', 'ja', 'ru']) {
      expect(localizedWordmark(KAITU, l)).toBe(`${KAITU.wordmark} ${KAITU.displayName}`);
    }
  });
});
