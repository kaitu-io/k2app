// Unit tests for generateMetadata() — Brand Split Phase 2.
//
// The two brands are fully isolated (spec: 两站互不感知). hreflang links only
// the rendering brand's OWN locales on its OWN host; x-default is that brand's
// default locale. No cross-domain linking, ever.
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

// Ensure NEXT_PUBLIC_BASE_URL does not leak into these tests.
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

describe('generateMetadata — brand-isolated SEO (Phase 2)', () => {
  it('kaitu hreflang covers only kaitu locales on kaitu.io', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    const langs = meta.alternates!.languages as Record<string, string>;
    expect(Object.keys(langs).sort()).toEqual(['x-default', 'zh-cn', 'zh-hk', 'zh-tw']);
    expect(langs['zh-tw']).toBe('https://kaitu.io/zh-TW/install');
    expect(langs['x-default']).toBe('https://kaitu.io/zh-CN/install');
    expect(JSON.stringify(meta)).not.toContain('overleap');
  });

  it('overleap hreflang covers only overleap locales on overleap.io', async () => {
    const { OVERLEAP } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('en-US', '/purchase', {}, OVERLEAP);
    const langs = meta.alternates!.languages as Record<string, string>;
    expect(Object.keys(langs).sort()).toEqual(['en-au', 'en-gb', 'en-us', 'ja', 'x-default']);
    expect(langs['x-default']).toBe('https://overleap.io/en-US/purchase');
    expect(JSON.stringify(meta)).not.toContain('kaitu');
  });

  it('zh title uses brand wordmark (no hardcoded 开途 in source)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '', {}, KAITU);
    expect(meta.title).toContain('开途');
  });

  it('overleap icons come from the brand favicon prefix', async () => {
    const { OVERLEAP } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('en-US', '', {}, OVERLEAP);
    expect(JSON.stringify(meta.icons)).toContain('/brand/overleap/favicon-32x32.png');
  });

  it('kaitu icons keep the legacy root paths (no cache churn)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '', {}, KAITU);
    expect(JSON.stringify(meta.icons)).toContain('"/favicon-32x32.png"');
    expect((meta.icons as { shortcut: string }).shortcut).toBe('/favicon.ico');
  });

  it('preserves pathname across locales (empty pathname → bare locale URL)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '', {}, KAITU);
    const langs = meta.alternates!.languages as Record<string, string>;
    expect(langs['zh-cn']).toBe('https://kaitu.io/zh-CN');
  });

  it('ignores NEXT_PUBLIC_BASE_URL override for hreflang entries', async () => {
    process.env.NEXT_PUBLIC_BASE_URL = 'https://preview.example.com';
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '/install', {}, KAITU);
    const langs = meta.alternates!.languages as Record<string, string>;
    // hreflang must use the brand's own host so preview envs can't poison SEO.
    expect(langs['zh-cn']).toBe('https://kaitu.io/zh-CN/install');
  });
});
