import { describe, it, expect, vi, afterEach } from 'vitest';
import { brandById, KAITU, OVERLEAP, parseBrandId, siteBrand } from '../brands';


describe('brandById', () => {
  it('returns KAITU for "kaitu"', () => {
    expect(brandById('kaitu')).toBe(KAITU);
  });

  it('returns OVERLEAP for "overleap"', () => {
    expect(brandById('overleap')).toBe(OVERLEAP);
  });
});

describe('brand configs', () => {
  it('KAITU is Chinese-only', () => {
    expect(KAITU.allowedLocales).toEqual(['zh-CN', 'zh-TW', 'zh-HK']);
    expect(KAITU.allowedLocales.length).toBe(3);
    expect(KAITU.defaultLocale).toBe('zh-CN');
    expect(KAITU.taglineZh).toBe('愿上帝为你开路');
  });

  it('OVERLEAP is English-only', () => {
    expect(OVERLEAP.allowedLocales).toEqual(['en-US', 'en-GB', 'en-AU', 'ja']);
    expect(OVERLEAP.defaultLocale).toBe('en-US');
    expect(OVERLEAP.taglineZh).toBeUndefined();
  });

  it('brand base URLs are distinct HTTPS origins', () => {
    expect(KAITU.baseUrl).toBe('https://kaitu.io');
    expect(OVERLEAP.baseUrl).toBe('https://overleap.io');
  });

  it('brand ids are distinct', () => {
    expect(KAITU.id).toBe('kaitu');
    expect(OVERLEAP.id).toBe('overleap');
    expect(KAITU.id).not.toBe(OVERLEAP.id);
  });
});


describe('parseBrandId', () => {
  it('returns overleap only for the exact string "overleap"', () => {
    expect(parseBrandId('overleap')).toBe('overleap');
  });
  it('falls back to kaitu for undefined, empty, and unknown values', () => {
    expect(parseBrandId(undefined)).toBe('kaitu');
    expect(parseBrandId(null)).toBe('kaitu');
    expect(parseBrandId('')).toBe('kaitu');
    expect(parseBrandId('OVERLEAP')).toBe('kaitu'); // exact match only — build var, not user input
    expect(parseBrandId('kaitu')).toBe('kaitu');
  });
});

describe('siteBrand', () => {
  afterEach(() => vi.unstubAllEnvs());

  it('defaults to KAITU when NEXT_PUBLIC_BRAND is unset', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', '');
    expect(siteBrand().id).toBe('kaitu');
  });
  it('returns OVERLEAP when NEXT_PUBLIC_BRAND=overleap', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect(siteBrand().id).toBe('overleap');
  });
});

describe('extended brand config', () => {
  // Legal-signature decision (2026-07-15, overrides plan Open Question #5):
  // BOTH deployments sign legal documents as "Overleap LLC" — the single
  // approved cross-brand appearance (root CLAUDE.md: 法务文书署名 Overleap LLC 除外).
  it('both brands sign legal documents as Overleap LLC', () => {
    expect(OVERLEAP.legalName).toBe('Overleap LLC');
    expect(KAITU.legalName).toBe('Overleap LLC');
  });
  it('kaitu keeps current GA + chatwoot; overleap has neither yet', () => {
    expect(KAITU.gaMeasurementId).toBe('G-EH2PY4S0CX');
    expect(KAITU.chatwootToken).toBe('ZfFNvQRuoKzkik6X4KCSgp1h');
    expect(OVERLEAP.gaMeasurementId).toBe('');
    expect(OVERLEAP.chatwootToken).toBe('');
  });
  it('cdn config carries per-brand bases and artifact prefixes', () => {
    expect(KAITU.cdn.artifactPrefix).toBe('Kaitu');
    expect(KAITU.cdn.desktopBases[0]).toBe('https://dl.kaitu.io/kaitu/desktop');
    expect(OVERLEAP.cdn.artifactPrefix).toBe('Overleap');
    expect(OVERLEAP.cdn.desktopBases[0]).toBe('https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop');
  });
  it('feature gates: routers/linux/apk-guide are kaitu-only', () => {
    expect(KAITU.features).toEqual({ routers: true, linuxInstall: true, androidApkGuide: true });
    expect(OVERLEAP.features).toEqual({ routers: false, linuxInstall: false, androidApkGuide: false });
  });
  it('productName drives user-facing product badges', () => {
    expect(KAITU.productName).toBe('开途 VPN');
    expect(OVERLEAP.productName).toBe('Overleap');
  });
});
