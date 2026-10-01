import { describe, it, expect, vi, afterEach } from 'vitest';
import { pxUrl, track, externalReferrerHost } from '../funnel';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('pxUrl', () => {
  it('encodes event, location and plan', () => {
    expect(pxUrl('plan_select', { plan: 'p1', loc: '/en-GB/pricing?utm_source=x' })).toBe(
      '/api/px?e=plan_select&u=%2Fen-GB%2Fpricing%3Futm_source%3Dx&p=p1',
    );
  });

  it('omits empty values', () => {
    expect(pxUrl('page_view', { loc: '/a', plan: '', source: undefined, ref: '' })).toBe('/api/px?e=page_view&u=%2Fa');
  });

  it('includes source and ref', () => {
    expect(pxUrl('checkout_start', { loc: '/a', source: 'self', ref: 'x.com' })).toBe(
      '/api/px?e=checkout_start&u=%2Fa&s=self&r=x.com',
    );
  });

  it('defaults loc to the current pathname + search', () => {
    window.history.pushState({}, '', '/zh-CN/purchase?plan=1');
    expect(pxUrl('page_view')).toBe('/api/px?e=page_view&u=%2Fzh-CN%2Fpurchase%3Fplan%3D1');
  });
});

describe('track', () => {
  it('loads the pixel through an Image', () => {
    const made: { src: string }[] = [];
    vi.stubGlobal('Image', function (this: { src: string }) { made.push(this); } as unknown as typeof Image);
    track('install_click', { plan: 'windows' });
    expect(made).toHaveLength(1);
    expect(made[0].src).toContain('e=install_click');
    expect(made[0].src).toContain('p=windows');
  });

  it('swallows any error', () => {
    vi.stubGlobal('Image', function () { throw new Error('boom'); } as unknown as typeof Image);
    expect(() => track('page_view')).not.toThrow();
  });
});

describe('externalReferrerHost', () => {
  it('is undefined when the referrer is empty', () => {
    vi.spyOn(document, 'referrer', 'get').mockReturnValue('');
    expect(externalReferrerHost()).toBeUndefined();
  });
  it('is undefined for same-site referrers', () => {
    vi.spyOn(document, 'referrer', 'get').mockReturnValue(`${window.location.origin}/x`);
    expect(externalReferrerHost()).toBeUndefined();
  });
  it('returns the host of an external referrer', () => {
    vi.spyOn(document, 'referrer', 'get').mockReturnValue('https://www.google.com/search?q=a');
    expect(externalReferrerHost()).toBe('www.google.com');
  });
  it('is undefined for an unparsable referrer', () => {
    vi.spyOn(document, 'referrer', 'get').mockReturnValue('not a url');
    expect(externalReferrerHost()).toBeUndefined();
  });
});
