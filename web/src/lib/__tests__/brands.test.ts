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
  it('KAITU is Chinese-only', () => {
    expect(KAITU.allowedLocales).toEqual(['zh-CN', 'zh-TW', 'zh-HK']);
    expect(KAITU.defaultLocale).toBe('zh-CN');
    expect(KAITU.taglineZh).toBe('愿上帝为你开路');
  });

  it('brand base URLs are distinct HTTPS origins', () => {
    expect(KAITU.baseUrl).toBe('https://kaitu.io');
    expect(OVERLEAP.baseUrl).toBe('https://overleap.io');
  });

  it('brand ids are distinct', () => {
    expect(KAITU.id).toBe('kaitu');
    expect(OVERLEAP.id).toBe('overleap');
  });

  // Legal-signature decision (2026-07-15): legal documents sign as "Overleap LLC"
  // — the single approved cross-brand appearance (root CLAUDE.md).
  it('legal documents sign as Overleap LLC', () => {
    expect(KAITU.legalName).toBe('Overleap LLC');
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
