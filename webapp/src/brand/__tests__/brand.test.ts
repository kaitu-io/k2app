/**
 * Brand config module tests.
 * Vitest runs with __K2_BRAND__ = 'kaitu' (vitest.config.ts define default),
 * so the active brandConfig under test is always kaitu unless K2_BRAND is set.
 */
import { describe, it, expect } from 'vitest';
import { brandConfig, getBrandId } from '../index';
import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';

describe('brand registry', () => {
  it('active brand defaults to kaitu', () => {
    expect(getBrandId()).toBe('kaitu');
    expect(brandConfig.id).toBe('kaitu');
    expect(brandConfig).toBe(KAITU_BRAND);
  });

  it('kaitu config carries the kaitu identity', () => {
    expect(KAITU_BRAND.productName).toBe('Kaitu');
    expect(KAITU_BRAND.domainLabel).toBe('Kaitu.io');
    expect(KAITU_BRAND.baseURL).toBe('https://www.kaitu.io');
    expect(KAITU_BRAND.websiteOrigins).toEqual(['https://www.kaitu.io', 'https://kaitu.io']);
    expect(KAITU_BRAND.supportEmail).toBe('support@kaitu.me');
    expect(KAITU_BRAND.defaultLocale).toBe('zh-CN');
    expect(KAITU_BRAND.names.zhHans).toBe('开途');
    expect(KAITU_BRAND.names.zhHant).toBe('開途');
    expect(KAITU_BRAND.features.invite).toBe(true);
    expect(KAITU_BRAND.features.wordgatePurchase).toBe(true);
    expect(KAITU_BRAND.features.stripeCheckout).toBe(false);
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
  });

  it('both brands expose all 7 locales', () => {
    for (const b of [KAITU_BRAND, OVERLEAP_BRAND]) {
      expect(b.locales).toEqual(
        expect.arrayContaining(['zh-CN', 'en-US', 'ja', 'zh-TW', 'zh-HK', 'en-AU', 'en-GB'])
      );
      expect(b.locales).toHaveLength(7);
    }
  });
});
