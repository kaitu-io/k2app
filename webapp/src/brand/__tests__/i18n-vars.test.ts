import { describe, it, expect } from 'vitest';
import { getBrandName, getBrandSlogan, brandI18nVariables } from '../i18n-vars';
import { brandConfig } from '../index';
import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';

// The active brand follows the K2_BRAND build var, so the behavioural blocks
// below are gated on it — this keeps both `vitest run` and
// `K2_BRAND=overleap vitest run` exiting zero while still asserting each
// brand's real behaviour when that brand is active.
const isKaitu = brandConfig.id === 'kaitu';

describe.runIf(isKaitu)('brand i18n variables (active brand = kaitu)', () => {
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
});

describe.runIf(!isKaitu)('brand i18n variables (active brand = overleap)', () => {
  it('renders Overleap in every locale — never 开途/Kaitu', () => {
    for (const locale of ['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'ja', 'en-AU', 'en-GB']) {
      expect(getBrandName(locale)).toBe('Overleap');
    }
  });

  it('brandI18nVariables carries no cross-brand identity', () => {
    const vars = brandI18nVariables('zh-CN');
    expect(vars).toEqual({
      brand: 'Overleap',
      brandDomain: 'Overleap.io',
      brandBaseUrl: 'https://www.overleap.io',
      supportEmail: 'support@overleap.io',
    });
    expect(JSON.stringify(vars)).not.toMatch(/开途|開途|kaitu\.io/i);
  });

  it('slogan resolves per locale with default fallback', () => {
    expect(getBrandSlogan('zh-CN')).toBe(OVERLEAP_BRAND.slogans['zh-CN']);
    expect(getBrandSlogan('en-US')).toBe(OVERLEAP_BRAND.slogans.default);
  });
});

// Config-level contracts — brand-independent, always run.
describe('brand name config shape', () => {
  it('overleap name is Overleap in every locale (no zh variants)', () => {
    expect(OVERLEAP_BRAND.names.zhHans).toBeUndefined();
    expect(OVERLEAP_BRAND.names.zhHant).toBeUndefined();
    expect(OVERLEAP_BRAND.names.default).toBe('Overleap');
  });

  it('kaitu uses 开途/開途 in Chinese (中文语境禁用裸词 Kaitu)', () => {
    expect(KAITU_BRAND.names.zhHans).toBe('开途');
    expect(KAITU_BRAND.names.zhHant).toBe('開途');
  });
});
