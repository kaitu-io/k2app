/**
 * Unit tests for the smart-mode countries helper.
 *
 * Covers:
 * - Flag emoji generation for all 14 supported country codes
 * - Case-insensitivity of the flag emoji generator
 * - Malformed input handling for the flag emoji generator
 * - Locale name resolution for zh-CN and en-US
 * - Fallback behaviour for unknown codes + unknown locales
 * - `isSupportedCountry` matching for supported + unsupported codes
 */

import { describe, it, expect } from 'vitest';

import {
  countryFlagEmoji,
  countryName,
  isSupportedCountry,
  SUPPORTED_COUNTRY_CODES,
} from '../countries';

describe('countryFlagEmoji', () => {
  // Each expected flag is the two regional indicator characters combined.
  const expected: Record<string, string> = {
    cn: '\u{1F1E8}\u{1F1F3}',
    ir: '\u{1F1EE}\u{1F1F7}',
    ru: '\u{1F1F7}\u{1F1FA}',
    tr: '\u{1F1F9}\u{1F1F7}',
    pk: '\u{1F1F5}\u{1F1F0}',
    vn: '\u{1F1FB}\u{1F1F3}',
    mm: '\u{1F1F2}\u{1F1F2}',
    eg: '\u{1F1EA}\u{1F1EC}',
    id: '\u{1F1EE}\u{1F1E9}',
    sa: '\u{1F1F8}\u{1F1E6}',
    ae: '\u{1F1E6}\u{1F1EA}',
    th: '\u{1F1F9}\u{1F1ED}',
    bd: '\u{1F1E7}\u{1F1E9}',
    by: '\u{1F1E7}\u{1F1FE}',
  };

  it('produces the correct flag emoji for all 14 supported country codes', () => {
    for (const cc of SUPPORTED_COUNTRY_CODES) {
      expect(countryFlagEmoji(cc)).toBe(expected[cc]);
    }
  });

  it('is case-insensitive', () => {
    expect(countryFlagEmoji('CN')).toBe(expected.cn);
    expect(countryFlagEmoji('Ir')).toBe(expected.ir);
    expect(countryFlagEmoji('rU')).toBe(expected.ru);
  });

  it('returns empty string for missing / malformed input', () => {
    expect(countryFlagEmoji(null)).toBe('');
    expect(countryFlagEmoji(undefined)).toBe('');
    expect(countryFlagEmoji('')).toBe('');
    expect(countryFlagEmoji('c')).toBe('');
    expect(countryFlagEmoji('cnx')).toBe('');
    // Non-ASCII letters — outside a-z should fall through.
    expect(countryFlagEmoji('中文')).toBe('');
    expect(countryFlagEmoji('1a')).toBe('');
  });
});

describe('countryName', () => {
  it('resolves all 14 supported country codes in en-US', () => {
    expect(countryName('cn', 'en-US')).toBe('China');
    expect(countryName('ir', 'en-US')).toBe('Iran');
    expect(countryName('ru', 'en-US')).toBe('Russia');
    expect(countryName('tr', 'en-US')).toBe('Turkey');
    expect(countryName('pk', 'en-US')).toBe('Pakistan');
    expect(countryName('vn', 'en-US')).toBe('Vietnam');
    expect(countryName('mm', 'en-US')).toBe('Myanmar');
    expect(countryName('eg', 'en-US')).toBe('Egypt');
    expect(countryName('id', 'en-US')).toBe('Indonesia');
    expect(countryName('sa', 'en-US')).toBe('Saudi Arabia');
    expect(countryName('ae', 'en-US')).toBe('UAE');
    expect(countryName('th', 'en-US')).toBe('Thailand');
    expect(countryName('bd', 'en-US')).toBe('Bangladesh');
    expect(countryName('by', 'en-US')).toBe('Belarus');
  });

  it('resolves all 14 supported country codes in zh-CN', () => {
    expect(countryName('cn', 'zh-CN')).toBe('中国');
    expect(countryName('ir', 'zh-CN')).toBe('伊朗');
    expect(countryName('ru', 'zh-CN')).toBe('俄罗斯');
    expect(countryName('tr', 'zh-CN')).toBe('土耳其');
    expect(countryName('pk', 'zh-CN')).toBe('巴基斯坦');
    expect(countryName('vn', 'zh-CN')).toBe('越南');
    expect(countryName('mm', 'zh-CN')).toBe('缅甸');
    expect(countryName('eg', 'zh-CN')).toBe('埃及');
    expect(countryName('id', 'zh-CN')).toBe('印度尼西亚');
    expect(countryName('sa', 'zh-CN')).toBe('沙特阿拉伯');
    expect(countryName('ae', 'zh-CN')).toBe('阿联酋');
    expect(countryName('th', 'zh-CN')).toBe('泰国');
    expect(countryName('bd', 'zh-CN')).toBe('孟加拉国');
    expect(countryName('by', 'zh-CN')).toBe('白俄罗斯');
  });

  it('falls back to English for unsupported locale base language', () => {
    expect(countryName('jp', 'ja')).toBe('Japan');
    expect(countryName('cn', 'de-DE')).toBe('China');
    expect(countryName('ru', 'fr')).toBe('Russia');
  });

  it('resolves zh-TW and zh-HK via zh base language', () => {
    expect(countryName('cn', 'zh-TW')).toBe('中国');
    expect(countryName('cn', 'zh-HK')).toBe('中国');
  });

  it('case-insensitive input', () => {
    expect(countryName('CN', 'en-US')).toBe('China');
    expect(countryName('Ru', 'zh-CN')).toBe('俄罗斯');
  });

  it('returns uppercased code for unknown countries', () => {
    expect(countryName('zz', 'en-US')).toBe('ZZ');
    expect(countryName('xx', 'zh-CN')).toBe('XX');
  });

  it('returns empty string for null / undefined input', () => {
    expect(countryName(null, 'en-US')).toBe('');
    expect(countryName(undefined, 'zh-CN')).toBe('');
  });
});

describe('isSupportedCountry', () => {
  it('returns true for all 14 supported codes (any case)', () => {
    for (const cc of SUPPORTED_COUNTRY_CODES) {
      expect(isSupportedCountry(cc)).toBe(true);
      expect(isSupportedCountry(cc.toUpperCase())).toBe(true);
    }
  });

  it('returns false for countries outside the 14-profile list', () => {
    expect(isSupportedCountry('jp')).toBe(false);
    expect(isSupportedCountry('us')).toBe(false);
    expect(isSupportedCountry('gb')).toBe(false);
  });

  it('returns false for null / undefined / empty input', () => {
    expect(isSupportedCountry(null)).toBe(false);
    expect(isSupportedCountry(undefined)).toBe(false);
    expect(isSupportedCountry('')).toBe(false);
  });
});
