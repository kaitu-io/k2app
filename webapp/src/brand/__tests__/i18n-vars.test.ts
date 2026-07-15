import { describe, it, expect } from 'vitest';
import { getBrandName, getBrandSlogan, brandI18nVariables } from '../i18n-vars';
import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';

// Active brand under vitest is kaitu (__K2_BRAND__ define default).
describe('brand i18n variables (active brand = kaitu)', () => {
  it('Chinese locales render 开途/開途, others render Kaitu', () => {
    expect(getBrandName('zh-CN')).toBe('开途');
    expect(getBrandName('zh-TW')).toBe('開途');
    expect(getBrandName('zh-HK')).toBe('開途');
    expect(getBrandName('en-US')).toBe('Kaitu');
    expect(getBrandName('ja')).toBe('Kaitu');
  });

  it('slogan resolves per locale with default fallback', () => {
    expect(getBrandSlogan('zh-CN')).toBe('越拥堵，越从容');
    expect(getBrandSlogan('en-US')).toBe(KAITU_BRAND.slogans.default);
    expect(getBrandSlogan('en-GB')).toBe(KAITU_BRAND.slogans.default);
  });

  it('brandI18nVariables exposes the interpolation contract', () => {
    expect(brandI18nVariables('zh-CN')).toEqual({
      brand: '开途',
      brandDomain: 'Kaitu.io',
      brandBaseUrl: 'https://www.kaitu.io',
      supportEmail: 'support@kaitu.me',
    });
  });

  it('overleap name would be Overleap in every locale (config-level check)', () => {
    // brandConfig is baked; assert the config shape that getBrandName relies on.
    expect(OVERLEAP_BRAND.names.zhHans).toBeUndefined();
    expect(OVERLEAP_BRAND.names.zhHant).toBeUndefined();
    expect(OVERLEAP_BRAND.names.default).toBe('Overleap');
  });
});
