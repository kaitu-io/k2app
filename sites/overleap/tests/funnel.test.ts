/**
 * Cookieless funnel pixel: events the Center accepts from the web surface, a
 * per-load in-memory visitor id, nothing persisted on the device, and only the
 * path + utm_* leaving the browser.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';
import { FUNNEL_EVENTS, newVisitorId, pixelLocation, pxUrl, resetVisitorForTest, track } from '@/lib/funnel';

const contract = JSON.parse(
  readFileSync(path.resolve(__dirname, '../../../contracts/api-contract.json'), 'utf8'),
) as { funnelEvents: { name: string; surfaces: string[] }[] };

describe('funnel pixel', () => {
  beforeEach(() => resetVisitorForTest());
  afterEach(() => vi.restoreAllMocks());

  it('every event is one the Center accepts from the web surface', () => {
    const web = new Set(contract.funnelEvents.filter((e) => e.surfaces.includes('web')).map((e) => e.name));
    for (const e of FUNNEL_EVENTS) expect(web.has(e), e).toBe(true);
  });

  it('visitor id has the Center sid format and is stable for the loaded app only', () => {
    expect(newVisitorId()).toMatch(/^[A-Za-z0-9_-]{22}$/);
    const a1 = new URL(pxUrl('page_view', { loc: '/en-GB' }), 'https://x').searchParams.get('a');
    const a2 = new URL(pxUrl('pricing_view', { loc: '/en-GB/pricing' }), 'https://x').searchParams.get('a');
    expect(a1).toMatch(/^[A-Za-z0-9_-]{22}$/);
    expect(a2).toBe(a1);
    resetVisitorForTest(); // = a full reload
    const a3 = new URL(pxUrl('page_view', { loc: '/en-GB' }), 'https://x').searchParams.get('a');
    expect(a3).not.toBe(a1);
  });

  it('only the path and utm_* tags leave the browser', () => {
    expect(pixelLocation('/en-GB/purchase?session_id=cs_123&utm_source=news&next=/x#chat=tok')).toBe(
      '/en-GB/purchase?utm_source=news',
    );
    expect(pixelLocation('/en-GB/login?next=/account')).toBe('/en-GB/login');
  });

  it('track() stores nothing on the device and sends same-origin keepalive', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null));
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const cookieBefore = document.cookie;
    track('checkout_start', { plan: 'overleap-yearly' });
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    const [url, init] = fetchSpy.mock.calls[0];
    expect(String(url)).toMatch(/^\/api\/px\?e=checkout_start&a=[A-Za-z0-9_-]{22}/);
    expect(String(url)).toContain('p=overleap-yearly');
    expect(init).toMatchObject({ keepalive: true, credentials: 'same-origin' });
    expect(setItem).not.toHaveBeenCalled();
    expect(document.cookie).toBe(cookieBefore);
  });

  it('a failing network never throws into the product path', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(() => {
      throw new Error('offline');
    });
    expect(() => track('auth_done')).not.toThrow();
  });
});
