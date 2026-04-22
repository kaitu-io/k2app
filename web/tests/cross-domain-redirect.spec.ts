import { test, expect } from '@playwright/test';

// These tests use Host-header override to simulate production hosts against the
// local dev server. isProductionHost() in middleware reads the host header, so
// passing Host: kaitu.io makes the middleware treat localhost:3000 as kaitu.io
// and trigger the cross-domain 301.
//
// Playwright's request.fetch follows redirects by default. We disable follow
// with { maxRedirects: 0 } so we can assert on the 301 status + Location header.

test.describe('cross-domain 301 middleware', () => {
  test('kaitu.io host + /en-US/install → 301 → overleap.io/en-US/install', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/en-US/install', {
      headers: { Host: 'kaitu.io' },
      maxRedirects: 0,
    });
    expect(res.status()).toBe(301);
    expect(res.headers()['location']).toBe('https://overleap.io/en-US/install');
  });

  test('kaitu.io host + /ja/k2 → 301 → overleap.io/ja/k2', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/ja/k2', {
      headers: { Host: 'kaitu.io' },
      maxRedirects: 0,
    });
    expect(res.status()).toBe(301);
    expect(res.headers()['location']).toBe('https://overleap.io/ja/k2');
  });

  test('overleap.io host + /zh-CN/install → 301 → kaitu.io/zh-CN/install', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/zh-CN/install', {
      headers: { Host: 'overleap.io' },
      maxRedirects: 0,
    });
    expect(res.status()).toBe(301);
    expect(res.headers()['location']).toBe('https://kaitu.io/zh-CN/install');
  });

  test('overleap.io host + /zh-TW/support → 301 → kaitu.io/zh-TW/support', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/zh-TW/support', {
      headers: { Host: 'overleap.io' },
      maxRedirects: 0,
    });
    expect(res.status()).toBe(301);
    expect(res.headers()['location']).toBe('https://kaitu.io/zh-TW/support');
  });

  test('query string preserved: kaitu.io/en-US/purchase?ref=abc → 301 → overleap.io/en-US/purchase?ref=abc', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/en-US/purchase?ref=abc', {
      headers: { Host: 'kaitu.io' },
      maxRedirects: 0,
    });
    expect(res.status()).toBe(301);
    expect(res.headers()['location']).toBe('https://overleap.io/en-US/purchase?ref=abc');
  });

  test('overleap.io + /ja/k2 passes through (ja now on Overleap)', async ({ request }) => {
    // ja moved to Overleap's allowedLocales in Task 2; no cross-domain should happen.
    const res = await request.fetch('http://localhost:3000/ja/k2', {
      headers: { Host: 'overleap.io' },
      maxRedirects: 0,
    });
    // Pass-through: status 200 or 307/308 for next-intl internal rewrites, but NOT 301 cross-domain.
    expect(res.status()).not.toBe(301);
    // If any redirect happens, Location should NOT be a different host.
    const location = res.headers()['location'];
    if (location) {
      expect(location).not.toContain('kaitu.io');
    }
  });

  test('localhost host bypasses cross-domain (no 301)', async ({ request }) => {
    const res = await request.fetch('http://localhost:3000/en-US/install', {
      maxRedirects: 0,
    });
    // No Host override → request.headers.host is 'localhost:3000' → isProductionHost false → no cross-domain redirect.
    expect(res.status()).not.toBe(301);
  });
});
