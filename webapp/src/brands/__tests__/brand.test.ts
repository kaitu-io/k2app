/**
 * Brand config module tests.
 * Vitest defines __K2_BRAND__ from env K2_BRAND (default 'kaitu'), so the
 * active brand here follows the build exactly as production does. The
 * active-brand test below asserts against that env so the suite stays green
 * under BOTH `vitest run` and `K2_BRAND=overleap vitest run`.
 */
import { describe, it, expect } from 'vitest';
import { brandConfig, getBrandId } from '../index';
import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';

// Mirrors the vitest.config.ts define normalization (anything != overleap → kaitu).
const EXPECTED_ACTIVE = process.env.K2_BRAND === 'overleap' ? 'overleap' : 'kaitu';

describe('brand registry', () => {
  it('active brand follows the K2_BRAND build var (default kaitu)', () => {
    expect(getBrandId()).toBe(EXPECTED_ACTIVE);
    expect(brandConfig.id).toBe(EXPECTED_ACTIVE);
    expect(brandConfig).toBe(EXPECTED_ACTIVE === 'overleap' ? OVERLEAP_BRAND : KAITU_BRAND);
  });

  it('kaitu config carries the kaitu identity', () => {
    expect(KAITU_BRAND.productName).toBe('Kaitu');
    expect(KAITU_BRAND.domainLabel).toBe('Kaitu.io');
    expect(KAITU_BRAND.baseURL).toBe('https://www.kaitu.io');
    expect(KAITU_BRAND.supportEmail).toBe('support@kaitu.io');
    expect(KAITU_BRAND.defaultLocale).toBe('zh-CN');
    expect(KAITU_BRAND.names.zhHans).toBe('开途');
    expect(KAITU_BRAND.names.zhHant).toBe('開途');
    expect(KAITU_BRAND.features.invite).toBe(true);
    expect(KAITU_BRAND.features.wordgatePurchase).toBe(true);
    expect(KAITU_BRAND.features.stripeCheckout).toBe(false);
    expect(KAITU_BRAND.features.multiCountryRouting).toBe(false); // China-market: cn-fixed
  });

  it('overleap config carries the overleap identity', () => {
    expect(OVERLEAP_BRAND.productName).toBe('Overleap');
    expect(OVERLEAP_BRAND.domainLabel).toBe('Overleap.io');
    expect(OVERLEAP_BRAND.baseURL).toBe('https://www.overleap.io');
    expect(OVERLEAP_BRAND.supportEmail).toBe('support@overleap.io');
    expect(OVERLEAP_BRAND.defaultLocale).toBe('en-US');
    expect(OVERLEAP_BRAND.names.zhHans).toBeUndefined(); // Overleap is "Overleap" in every locale
    expect(OVERLEAP_BRAND.names.zhHant).toBeUndefined();
    expect(OVERLEAP_BRAND.features.invite).toBe(false);
    expect(OVERLEAP_BRAND.features.retailer).toBe(false);
    expect(OVERLEAP_BRAND.features.wordgatePurchase).toBe(false);
    expect(OVERLEAP_BRAND.features.stripeCheckout).toBe(true);
    expect(OVERLEAP_BRAND.features.multiCountryRouting).toBe(true); // serves all countries
  });

  it('kaitu offers 7 locales, all of which overleap also carries; overleap adds more', () => {
    expect(OVERLEAP_BRAND.locales).toEqual(expect.arrayContaining([...KAITU_BRAND.locales]));
    expect(KAITU_BRAND.locales).toHaveLength(7);
    // Exact overleap membership is pinned to the language registry in
    // i18n/__tests__/locale-coverage.test.ts.
    expect(OVERLEAP_BRAND.locales.length).toBeGreaterThan(7);
  });
  it('netcheck targets: plain-HTTP vendor endpoints with an expected status', () => {
    // The engine compares status (and body, when given) exactly; an https URL or a
    // redirecting one would read as a captive portal on a healthy network.
    for (const b of [KAITU_BRAND, OVERLEAP_BRAND]) {
      expect(b.netcheck.length).toBeGreaterThanOrEqual(3);
      for (const t of b.netcheck) {
        expect(t.url.startsWith('http://')).toBe(true);
        expect([200, 204]).toContain(t.status);
        if (t.status === 200) expect(t.body).toBeTruthy();
      }
    }
  });

  it('netcheck DNS: overleap overrides the engine default (mainland resolvers), kaitu keeps it', () => {
    expect(KAITU_BRAND.netcheckDns).toEqual([]);
    expect(OVERLEAP_BRAND.netcheckDns.length).toBeGreaterThan(0);
    for (const mainland of ['114.114.114.114:53', '223.5.5.5:53', '119.29.29.29:53']) {
      expect(OVERLEAP_BRAND.netcheckDns).not.toContain(mainland);
    }
  });
});
