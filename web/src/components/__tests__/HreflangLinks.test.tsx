import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';

/**
 * Tests for HreflangLinks — brand-aware cross-domain hreflang.
 *
 * After Task 2 (brands.ts) and this task (Task 8), each hreflang link points
 * to the host owning that locale:
 *   - zh-* → https://kaitu.io
 *   - en-*, ja → https://overleap.io
 * x-default points to https://kaitu.io/zh-CN (Chinese is the default locale).
 */

// Mock the routing locale list — avoids pulling next-intl/navigation into jsdom.
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
}));

// Import after mocks
import HreflangLinks from '../HreflangLinks';

type ParsedLink = { hreflang: string; href: string };

function parseLinks(html: string): ParsedLink[] {
  // React's renderToStaticMarkup preserves `hrefLang` attribute casing.
  const re = /<link[^>]*?hrefLang="([^"]+)"[^>]*?href="([^"]+)"[^>]*?\/?>/g;
  const out: ParsedLink[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(html)) !== null) {
    out.push({ hreflang: m[1], href: m[2] });
  }
  return out;
}

describe('HreflangLinks', () => {
  it('emits 7 locale alternates + 1 x-default on /install', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/install" />);
    const links = parseLinks(html);
    expect(links).toHaveLength(8);
    const hreflangs = links.map((l) => l.hreflang).sort();
    expect(hreflangs).toEqual(
      ['en-us', 'en-gb', 'en-au', 'zh-cn', 'zh-tw', 'zh-hk', 'ja', 'x-default'].sort(),
    );
  });

  it('each zh locale link points to kaitu.io', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/install" />);
    const links = parseLinks(html);
    const zhLinks = links.filter((l) => l.hreflang.startsWith('zh-'));
    expect(zhLinks.length).toBe(3);
    for (const l of zhLinks) {
      expect(l.href.startsWith('https://kaitu.io/')).toBe(true);
    }
  });

  it('each en locale and ja link points to overleap.io', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/install" />);
    const links = parseLinks(html);
    const overseasLinks = links.filter(
      (l) => l.hreflang.startsWith('en-') || l.hreflang === 'ja',
    );
    expect(overseasLinks.length).toBe(4);
    for (const l of overseasLinks) {
      expect(l.href.startsWith('https://overleap.io/')).toBe(true);
    }
  });

  it('x-default href is kaitu.io zh-CN install', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/install" />);
    const links = parseLinks(html);
    const xDefault = links.find((l) => l.hreflang === 'x-default');
    expect(xDefault?.href).toBe('https://kaitu.io/zh-CN/install');
  });

  it('root pathname produces kaitu.io zh-CN with no trailing slash', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/" />);
    const links = parseLinks(html);
    const zhCN = links.find((l) => l.hreflang === 'zh-cn');
    expect(zhCN?.href).toBe('https://kaitu.io/zh-CN');
    const xDefault = links.find((l) => l.hreflang === 'x-default');
    expect(xDefault?.href).toBe('https://kaitu.io/zh-CN');
  });

  it('root pathname produces overleap.io en-US with no trailing slash', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/" />);
    const links = parseLinks(html);
    const enUS = links.find((l) => l.hreflang === 'en-us');
    expect(enUS?.href).toBe('https://overleap.io/en-US');
  });

  it('preserves the exact pathname segment across both brands', () => {
    const html = renderToStaticMarkup(<HreflangLinks pathname="/purchase" />);
    const links = parseLinks(html);
    expect(links.find((l) => l.hreflang === 'zh-cn')?.href).toBe(
      'https://kaitu.io/zh-CN/purchase',
    );
    expect(links.find((l) => l.hreflang === 'en-us')?.href).toBe(
      'https://overleap.io/en-US/purchase',
    );
    expect(links.find((l) => l.hreflang === 'ja')?.href).toBe(
      'https://overleap.io/ja/purchase',
    );
  });
});
