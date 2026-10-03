// Unit tests for generateMetadata(). hreflang links only this site's own
// locales on its own host; x-default is the default locale. No cross-domain
// linking to the other brand's site.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Mock @/i18n/routing to avoid pulling next-intl/navigation (and transitively
// next/navigation) into vitest's module graph — same pattern used by sibling
// tests (brands.ts inlines ALL_LOCALES for the same reason).
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'zh-TW', 'zh-HK'],
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

describe('generateMetadata', () => {
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

  it('zh title uses brand wordmark (no hardcoded 开途 in source)', async () => {
    const { KAITU } = await loadBrands();
    const generateMetadata = await loadMetadata();
    const meta = generateMetadata('zh-CN', '', {}, KAITU);
    expect(meta.title).toContain('开途');
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

  it('omitted brand yields kaitu metadata', async () => {
    const generateMetadata = await loadMetadata();

    const meta = generateMetadata('zh-CN', '/support');

    expect(meta.alternates!.canonical).toBe('https://kaitu.io/zh-CN/support');
    expect(JSON.stringify(meta)).not.toContain('overleap');
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
