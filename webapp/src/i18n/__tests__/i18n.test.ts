import { describe, it, expect } from 'vitest';
import { normalizeLanguageCode } from '../i18n';

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
    it('should fallback to zh-CN for completely unsupported languages', () => {
      expect(normalizeLanguageCode('fr-FR')).toBe('zh-CN');
      expect(normalizeLanguageCode('de-DE')).toBe('zh-CN');
      expect(normalizeLanguageCode('es-ES')).toBe('zh-CN');
      expect(normalizeLanguageCode('ko-KR')).toBe('zh-CN');
    });
  });

  describe('edge cases', () => {
    it('should handle empty string', () => {
      expect(normalizeLanguageCode('')).toBe('zh-CN');
    });

    it('should handle single character codes', () => {
      expect(normalizeLanguageCode('z')).toBe('zh-CN');
      expect(normalizeLanguageCode('e')).toBe('zh-CN');
    });

    it('should handle invalid formats', () => {
      expect(normalizeLanguageCode('invalid')).toBe('zh-CN');
      expect(normalizeLanguageCode('123')).toBe('zh-CN');
    });
  });
});
