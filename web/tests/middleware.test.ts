/**
 * Middleware Bidirectional 301 Cross-Domain Tests — PR-2 Task 4
 *
 * Verifies that locale-prefixed paths on the "wrong" brand host get redirected
 * with a permanent 301 to the owning brand's base URL, preserving query + hash.
 *
 * Owner map (post Task 2):
 *   Kaitu     → zh-CN, zh-TW, zh-HK
 *   Overleap  → en-US, en-GB, en-AU, ja
 */
import { describe, it, expect, vi } from 'vitest';
import { NextRequest } from 'next/server';

// Avoid next-intl trying to pull request config during middleware init in tests.
vi.mock('next-intl/middleware', () => ({
  default: () => () => new Response(null, { status: 200 }),
}));

// Stub routing to avoid pulling next-intl/navigation (which transitively needs
// next/navigation) through middleware's import chain.
vi.mock('../src/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
}));

import middleware from '../src/middleware';

function makeRequest(host: string, path: string, search = '', hash = ''): NextRequest {
  const protocol = host.startsWith('localhost') ? 'http' : 'https';
  const url = `${protocol}://${host}${path}${search}${hash}`;
  return new NextRequest(url, { headers: { host } });
}

async function runMiddleware(req: NextRequest): Promise<Response> {
  const res = await middleware(req);
  // next-intl middleware may return undefined in some paths; normalize.
  return res ?? new Response(null, { status: 200 });
}

describe('middleware: bidirectional 301 cross-domain on locale/brand mismatch', () => {
  describe('kaitu.io hosting Overleap locales → 301 to overleap.io', () => {
    it('1. kaitu.io /en-US/install → 301 overleap.io/en-US/install', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/en-US/install'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://overleap.io/en-US/install');
    });

    it('2. kaitu.io /en-GB/purchase → 301 overleap.io/en-GB/purchase', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/en-GB/purchase'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://overleap.io/en-GB/purchase');
    });

    it('3. kaitu.io /en-AU/ → 301 overleap.io/en-AU/', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/en-AU/'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://overleap.io/en-AU/');
    });

    it('4. kaitu.io /ja/k2 → 301 overleap.io/ja/k2 (ja is Overleap-owned post Task 2)', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/ja/k2'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://overleap.io/ja/k2');
    });
  });

  describe('kaitu.io hosting Kaitu locales → pass-through (NOT 301)', () => {
    it('5. kaitu.io /zh-CN/install → NOT 301', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/zh-CN/install'));
      expect(res.status).not.toBe(301);
      // If a redirect is returned (e.g. intl middleware), its Location must be same-host.
      const loc = res.headers.get('location');
      if (loc) {
        const locUrl = new URL(loc, 'https://kaitu.io');
        expect(locUrl.hostname).toBe('kaitu.io');
      }
    });

    it('6. kaitu.io /zh-TW/ → NOT 301', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/zh-TW/'));
      expect(res.status).not.toBe(301);
      const loc = res.headers.get('location');
      if (loc) {
        const locUrl = new URL(loc, 'https://kaitu.io');
        expect(locUrl.hostname).toBe('kaitu.io');
      }
    });
  });

  describe('overleap.io hosting Kaitu locales → 301 to kaitu.io', () => {
    it('7. overleap.io /zh-CN/blog → 301 kaitu.io/zh-CN/blog', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/zh-CN/blog'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://kaitu.io/zh-CN/blog');
    });

    it('8. overleap.io /zh-TW/support → 301 kaitu.io/zh-TW/support', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/zh-TW/support'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://kaitu.io/zh-TW/support');
    });

    it('9. overleap.io /zh-HK/k2/comparison → 301 kaitu.io/zh-HK/k2/comparison', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/zh-HK/k2/comparison'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://kaitu.io/zh-HK/k2/comparison');
    });
  });

  describe('overleap.io hosting Overleap locales → pass-through (NOT 301)', () => {
    it('10. overleap.io /ja/k2 → NOT 301 (ja now on Overleap)', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/ja/k2'));
      expect(res.status).not.toBe(301);
      const loc = res.headers.get('location');
      if (loc) {
        const locUrl = new URL(loc, 'https://overleap.io');
        expect(locUrl.hostname).toBe('overleap.io');
      }
    });

    it('11. overleap.io /en-US/blog → NOT 301', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/en-US/blog'));
      expect(res.status).not.toBe(301);
      const loc = res.headers.get('location');
      if (loc) {
        const locUrl = new URL(loc, 'https://overleap.io');
        expect(locUrl.hostname).toBe('overleap.io');
      }
    });
  });

  describe('query string + hash preservation', () => {
    it('12. kaitu.io /en-US/purchase?ref=abc → 301 overleap.io/en-US/purchase?ref=abc', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/en-US/purchase', '?ref=abc'));
      expect(res.status).toBe(301);
      expect(res.headers.get('location')).toBe('https://overleap.io/en-US/purchase?ref=abc');
    });
  });

  describe('non-production hosts bypass cross-domain redirect', () => {
    it('13. localhost:3000 /en-US/install → NOT 301 (dev host)', async () => {
      const res = await runMiddleware(makeRequest('localhost:3000', '/en-US/install'));
      expect(res.status).not.toBe(301);
    });

    it('14. localhost:3000 /zh-CN/install → NOT 301', async () => {
      const res = await runMiddleware(makeRequest('localhost:3000', '/zh-CN/install'));
      expect(res.status).not.toBe(301);
    });

    it('15. amplify preview host /en-US/install → NOT 301', async () => {
      const res = await runMiddleware(
        makeRequest('main.d3q8wll74rs94h.amplifyapp.com', '/en-US/install')
      );
      expect(res.status).not.toBe(301);
    });
  });

  // SEO hotfix: middleware must emit `x-middleware-request-x-pathname` on the
  // pass-through response so [locale]/layout.tsx can build correct hreflang
  // URLs for nested pages. Next.js converts `x-middleware-request-{X}` response
  // headers into downstream request headers named `{X}`.
  describe('x-pathname header injection for downstream RSC', () => {
    it('16. kaitu.io /zh-CN/install → response carries x-middleware-request-x-pathname=/install', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/zh-CN/install'));
      expect(res.status).not.toBe(301);
      expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/install');
    });

    it('17. overleap.io /en-US/k2/comparison → header preserves nested path', async () => {
      const res = await runMiddleware(makeRequest('overleap.io', '/en-US/k2/comparison'));
      expect(res.status).not.toBe(301);
      expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/k2/comparison');
    });

    it('18. kaitu.io /zh-CN (locale root, no trailing slash) → header is "/"', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/zh-CN'));
      expect(res.status).not.toBe(301);
      expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/');
    });

    it('19. kaitu.io /zh-CN/ (locale root with trailing slash) → header is "/"', async () => {
      const res = await runMiddleware(makeRequest('kaitu.io', '/zh-CN/'));
      expect(res.status).not.toBe(301);
      expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/');
    });

    it('20. localhost:3000 /en-US/purchase → header works on dev hosts too', async () => {
      const res = await runMiddleware(makeRequest('localhost:3000', '/en-US/purchase'));
      expect(res.status).not.toBe(301);
      expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/purchase');
    });
  });
});
