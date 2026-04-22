// Unit tests for generateMetadata() cross-domain hreflang.
//
// After the cross-domain rebrand (PR-2), each locale's hreflang link points
// to the host that owns that locale (kaitu.io for zh-*, overleap.io for
// en-* and ja), regardless of which brand is currently rendering. This lets
// Googlebot link the two domains as a single multi-regional property.
//
// x-default points to kaitu.io/zh-CN (Chinese is the product's main market).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Mock @/i18n/routing to avoid pulling next-intl/navigation (and transitively
// next/navigation) into vitest's module graph — same pattern used by sibling
// tests (brands.ts inlines ALL_LOCALES for the same reason).
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
}));

// Ensure NEXT_PUBLIC_BASE_URL does not leak into these tests — hreflang must
// use the brand-resolved base URL, not an env override (the override is only
// honored for the current brand's own canonical/og.url below).
beforeEach(() => {
  delete process.env.NEXT_PUBLIC_BASE_URL;
  vi.resetModules();
});

afterEach(() => {
  delete process.env.NEXT_PUBLIC_BASE_URL;
});

async function loadMetadata() {
  return (await import('../metadata')).generateMetadata;
}

async function loadBrands() {
  return await import('@/lib/brands');
}

describe('generateMetadata — cross-domain hreflang (alternates.languages)', () => {
  it('emits zh-CN → kaitu.io when brand=KAITU', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    expect(meta.alternates?.languages?.['zh-cn']).toBe('https://kaitu.io/zh-CN/install');
  });

  it('emits en-US → overleap.io (cross-domain) when brand=KAITU', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    expect(meta.alternates?.languages?.['en-us']).toBe('https://overleap.io/en-US/install');
  });

  it('emits ja → overleap.io (cross-domain) when brand=KAITU', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    expect(meta.alternates?.languages?.['ja']).toBe('https://overleap.io/ja/install');
  });

  it('emits the same languages record when brand=OVERLEAP (hreflang is brand-independent)', async () => {
    const { KAITU, OVERLEAP } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const metaKaitu = generateMetadata('zh-CN', '/install', {}, KAITU);
    const metaOverleap = generateMetadata('en-US', '/install', {}, OVERLEAP);
    expect(metaOverleap.alternates?.languages).toEqual(metaKaitu.alternates?.languages);
  });

  it('emits x-default → kaitu.io/zh-CN', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    expect(meta.alternates?.languages?.['x-default']).toBe('https://kaitu.io/zh-CN/install');
  });

  it('emits all 7 locales + x-default (8 entries total)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    const languages = meta.alternates?.languages ?? {};
    expect(Object.keys(languages).sort()).toEqual(
      ['en-au', 'en-gb', 'en-us', 'ja', 'x-default', 'zh-cn', 'zh-hk', 'zh-tw'],
    );
  });

  it('zh-TW and zh-HK also point to kaitu.io', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/purchase', {}, KAITU);
    expect(meta.alternates?.languages?.['zh-tw']).toBe('https://kaitu.io/zh-TW/purchase');
    expect(meta.alternates?.languages?.['zh-hk']).toBe('https://kaitu.io/zh-HK/purchase');
  });

  it('en-GB and en-AU also point to overleap.io', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/purchase', {}, KAITU);
    expect(meta.alternates?.languages?.['en-gb']).toBe('https://overleap.io/en-GB/purchase');
    expect(meta.alternates?.languages?.['en-au']).toBe('https://overleap.io/en-AU/purchase');
  });

  it('preserves pathname across locales (empty pathname → bare locale URL)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '', {}, KAITU);
    expect(meta.alternates?.languages?.['zh-cn']).toBe('https://kaitu.io/zh-CN');
    expect(meta.alternates?.languages?.['en-us']).toBe('https://overleap.io/en-US');
    expect(meta.alternates?.languages?.['x-default']).toBe('https://kaitu.io/zh-CN');
  });

  it('ignores NEXT_PUBLIC_BASE_URL override for hreflang entries', async () => {
    // Simulate a preview/staging env with a custom base URL. Hreflang must
    // still use the production brand hosts so SEO is not broken.
    process.env.NEXT_PUBLIC_BASE_URL = 'https://preview.kaitu.io';
    vi.resetModules();
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    expect(meta.alternates?.languages?.['zh-cn']).toBe('https://kaitu.io/zh-CN/install');
    expect(meta.alternates?.languages?.['en-us']).toBe('https://overleap.io/en-US/install');
    // canonical still uses the override for the current brand (preview env).
    expect(meta.alternates?.canonical).toBe('https://preview.kaitu.io/zh-CN/install');
  });
});
