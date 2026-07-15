import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import i18n, { normalizeLanguageCode, changeLanguage, i18nPromise } from '../i18n';
import { brandConfig } from '../../brand';
import { getBrandName } from '../../brand/i18n-vars';

// Unmappable input falls back to the BRAND's default locale by design
// (kaitu → zh-CN, overleap → en-US). Asserting brandConfig.defaultLocale
// pins that contract under either brand instead of hardcoding zh-CN.
const FALLBACK = brandConfig.defaultLocale;

describe('normalizeLanguageCode', () => {
  describe('supported languages', () => {
    it('should return the same code for already supported languages', () => {
      expect(normalizeLanguageCode('zh-CN')).toBe('zh-CN');
      expect(normalizeLanguageCode('zh-TW')).toBe('zh-TW');
      expect(normalizeLanguageCode('zh-HK')).toBe('zh-HK');
      expect(normalizeLanguageCode('en-US')).toBe('en-US');
      expect(normalizeLanguageCode('en-GB')).toBe('en-GB');
      expect(normalizeLanguageCode('en-AU')).toBe('en-AU');
      expect(normalizeLanguageCode('ja')).toBe('ja');
    });
  });

  describe('Chinese variants', () => {
    it('should map zh-SG to zh-CN (Singapore Chinese -> Simplified Chinese)', () => {
      expect(normalizeLanguageCode('zh-SG')).toBe('zh-CN');
    });

    it('should map zh-MY to zh-CN (Malaysia Chinese -> Simplified Chinese)', () => {
      expect(normalizeLanguageCode('zh-MY')).toBe('zh-CN');
    });

    it('should map zh-Hans to zh-CN (Simplified Chinese script tag)', () => {
      expect(normalizeLanguageCode('zh-Hans')).toBe('zh-CN');
    });

    it('should map zh-Hant to zh-TW (Traditional Chinese script tag)', () => {
      expect(normalizeLanguageCode('zh-Hant')).toBe('zh-TW');
    });

    it('should map zh-MO to zh-HK (Macau Chinese -> Hong Kong Chinese)', () => {
      expect(normalizeLanguageCode('zh-MO')).toBe('zh-HK');
    });

    it('should map generic zh to zh-CN (Generic Chinese -> Simplified Chinese)', () => {
      expect(normalizeLanguageCode('zh')).toBe('zh-CN');
    });
  });

  describe('English variants', () => {
    it('should map en to en-US (Generic English -> US English)', () => {
      expect(normalizeLanguageCode('en')).toBe('en-US');
    });

    it('should map en-CA to en-US (Canadian English -> US English)', () => {
      expect(normalizeLanguageCode('en-CA')).toBe('en-US');
    });

    it('should map en-NZ to en-AU (New Zealand English -> Australian English)', () => {
      expect(normalizeLanguageCode('en-NZ')).toBe('en-AU');
    });

    it('should map en-ZA to en-GB (South African English -> UK English)', () => {
      expect(normalizeLanguageCode('en-ZA')).toBe('en-GB');
    });

    it('should map en-IE to en-GB (Irish English -> UK English)', () => {
      expect(normalizeLanguageCode('en-IE')).toBe('en-GB');
    });
  });

  describe('Japanese variants', () => {
    it('should map ja-JP to ja (Japanese Japan -> Japanese)', () => {
      expect(normalizeLanguageCode('ja-JP')).toBe('ja');
    });
  });

  describe('case insensitivity', () => {
    it('should handle uppercase language codes', () => {
      expect(normalizeLanguageCode('ZH-SG')).toBe('zh-CN');
      expect(normalizeLanguageCode('EN-US')).toBe('en-US');
      expect(normalizeLanguageCode('JA-JP')).toBe('ja');
    });

    it('should handle mixed case language codes', () => {
      expect(normalizeLanguageCode('Zh-Sg')).toBe('zh-CN');
      expect(normalizeLanguageCode('En-Us')).toBe('en-US');
    });
  });

  describe('unsupported languages', () => {
    it("should fallback to the brand's default locale for completely unsupported languages", () => {
      expect(normalizeLanguageCode('fr-FR')).toBe(FALLBACK);
      expect(normalizeLanguageCode('de-DE')).toBe(FALLBACK);
      expect(normalizeLanguageCode('es-ES')).toBe(FALLBACK);
      expect(normalizeLanguageCode('ko-KR')).toBe(FALLBACK);
    });

    it('resolves the brand default locale to the expected per-brand value', () => {
      // Guards the brand↔locale wiring itself, so the assertions above can
      // never be vacuously satisfied by a wrong defaultLocale.
      expect(FALLBACK).toBe(brandConfig.id === 'overleap' ? 'en-US' : 'zh-CN');
    });
  });

  describe('edge cases', () => {
    it('should handle empty string', () => {
      expect(normalizeLanguageCode('')).toBe(FALLBACK);
    });

    it('should handle single character codes', () => {
      expect(normalizeLanguageCode('z')).toBe(FALLBACK);
      expect(normalizeLanguageCode('e')).toBe(FALLBACK);
    });

    it('should handle invalid formats', () => {
      expect(normalizeLanguageCode('invalid')).toBe(FALLBACK);
      expect(normalizeLanguageCode('123')).toBe(FALLBACK);
    });
  });
});

// ==================== {{brand}} interpolation across live language switches ====================
//
// i18n.ts installs interpolation.defaultVariables at init, then refreshes them
// on every 'languageChanged'. That listener is the only thing keeping the brand
// name locale-correct (kaitu renders 开途 in zh-*, Kaitu elsewhere) when the
// user switches language at runtime. It had no coverage: drop the listener and
// every {{brand}} string silently freezes at the boot locale's spelling.
describe('brand interpolation follows languageChanged', () => {
  // A brand-neutral locale string carrying {{brand}}, present in every locale.
  const KEY = 'purchase:deviceInstall.shareText';
  let originalLang: string;

  beforeAll(async () => {
    await i18nPromise;
    originalLang = i18n.language;
  });

  afterAll(async () => {
    await changeLanguage(originalLang as any);
  });

  it('renders the locale-correct brand name after switching to en-US', async () => {
    await changeLanguage('en-US');

    const expected = getBrandName('en-US');
    expect(i18n.t(KEY)).toContain(expected);
    // The template must actually have been interpolated, not passed through.
    expect(i18n.t(KEY)).not.toContain('{{brand}}');
    expect(i18n.options.interpolation?.defaultVariables?.brand).toBe(expected);
  });

  it('re-resolves the brand name when switching back to zh-CN', async () => {
    await changeLanguage('en-US');
    await changeLanguage('zh-CN');

    const expected = getBrandName('zh-CN');
    expect(i18n.t(KEY)).toContain(expected);
    expect(i18n.t(KEY)).not.toContain('{{brand}}');
    expect(i18n.options.interpolation?.defaultVariables?.brand).toBe(expected);
  });

  // The regression that matters for kaitu: zh-CN must say 开途, never the bare
  // Latin token, and switching away and back must not strand the wrong one.
  it.runIf(brandConfig.id === 'kaitu')('renders 开途 in zh-CN and Kaitu in en-US', async () => {
    await changeLanguage('zh-CN');
    expect(i18n.t(KEY)).toContain('开途');

    await changeLanguage('en-US');
    expect(i18n.t(KEY)).toContain('Kaitu');
    expect(i18n.t(KEY)).not.toContain('开途');
  });

  // Overleap has no zh variants — the name is Overleap in every locale, and no
  // kaitu token may ever leak through interpolation.
  it.runIf(brandConfig.id === 'overleap')('renders Overleap in every locale', async () => {
    for (const lng of ['zh-CN', 'en-US', 'zh-TW']) {
      await changeLanguage(lng as any);
      expect(i18n.t(KEY)).toContain('Overleap');
      expect(i18n.t(KEY)).not.toMatch(/开途|開途|Kaitu/);
    }
  });
});
