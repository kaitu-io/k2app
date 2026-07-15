import { describe, it, expect, vi, afterEach } from 'vitest';
import { brandFromHost, brandById, ownerBrand, KAITU, OVERLEAP, parseBrandId, siteBrand } from '../brands';

describe('brandFromHost', () => {
  it('maps kaitu.io to KAITU', () => {
    expect(brandFromHost('kaitu.io')).toBe(KAITU);
  });

  it('maps www.kaitu.io to KAITU', () => {
    expect(brandFromHost('www.kaitu.io')).toBe(KAITU);
  });

  it('maps overleap.io to OVERLEAP', () => {
    expect(brandFromHost('overleap.io')).toBe(OVERLEAP);
  });

  it('maps www.overleap.io to OVERLEAP', () => {
    expect(brandFromHost('www.overleap.io')).toBe(OVERLEAP);
  });

  it('strips port when matching', () => {
    expect(brandFromHost('overleap.io:3000')).toBe(OVERLEAP);
    expect(brandFromHost('kaitu.io:8080')).toBe(KAITU);
  });

  it('is case-insensitive', () => {
    expect(brandFromHost('OverLeap.IO')).toBe(OVERLEAP);
    expect(brandFromHost('KAITU.IO')).toBe(KAITU);
  });

  it('falls back to KAITU on null/undefined host', () => {
    expect(brandFromHost(undefined)).toBe(KAITU);
    expect(brandFromHost(null)).toBe(KAITU);
    expect(brandFromHost('')).toBe(KAITU);
  });

  it('falls back to KAITU on unknown host', () => {
    expect(brandFromHost('random.example.com')).toBe(KAITU);
    expect(brandFromHost('localhost')).toBe(KAITU);
    expect(brandFromHost('preview-abc.amplifyapp.com')).toBe(KAITU);
  });
});

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

describe('ownerBrand', () => {
  it('maps zh-CN to kaitu', () => {
    expect(ownerBrand('zh-CN')).toBe('kaitu');
  });

  it('maps zh-TW to kaitu', () => {
    expect(ownerBrand('zh-TW')).toBe('kaitu');
  });

  it('maps zh-HK to kaitu', () => {
    expect(ownerBrand('zh-HK')).toBe('kaitu');
  });

  it('maps en-US to overleap', () => {
    expect(ownerBrand('en-US')).toBe('overleap');
  });

  it('maps en-GB to overleap', () => {
    expect(ownerBrand('en-GB')).toBe('overleap');
  });

  it('maps en-AU to overleap', () => {
    expect(ownerBrand('en-AU')).toBe('overleap');
  });

  it('maps ja to overleap', () => {
    expect(ownerBrand('ja')).toBe('overleap');
  });

  it('falls back to kaitu on unknown locale', () => {
    expect(ownerBrand('unknown')).toBe('kaitu');
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
