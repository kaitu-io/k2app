import { describe, it, expect } from 'vitest';
import { matchLocale } from '../match-locale';
import { OVERLEAP_BRAND } from '../../brands/overleap';
import { KAITU_BRAND } from '../../brands/kaitu';

const OVERLEAP = OVERLEAP_BRAND.locales;
const KAITU = KAITU_BRAND.locales;
const match = (preferred: string[], supported: readonly string[] = OVERLEAP, fallback = 'en-US') =>
  matchLocale(preferred, supported, fallback);

describe('matchLocale', () => {
  it('returns an exact match regardless of case or separator', () => {
    expect(match(['pt-BR'])).toBe('pt-BR');
    expect(match(['PT-br'])).toBe('pt-BR');
    expect(match(['pt_BR'])).toBe('pt-BR');
    expect(match(['ko'])).toBe('ko');
  });

  it('strips region and script subtags down to a supported parent', () => {
    expect(match(['fr-CA'])).toBe('fr');
    expect(match(['de-AT'])).toBe('de');
    expect(match(['ar-EG'])).toBe('ar');
    expect(match(['fa-IR'])).toBe('fa');
    expect(match(['my-MM'])).toBe('my');
    expect(match(['km-KH'])).toBe('km');
    expect(match(['sr-Latn-RS'], ['sr', 'en-US'])).toBe('sr');
  });

  it('falls to the first supported locale of the same language', () => {
    expect(match(['pt-PT'])).toBe('pt-BR');
    expect(match(['pt'])).toBe('pt-BR');
  });

  it('routes regional and script variants through the alias table', () => {
    expect(match(['zh-Hant-HK'])).toBe('zh-HK');
    expect(match(['zh-Hant'])).toBe('zh-TW');
    expect(match(['zh-Hans-CN'])).toBe('zh-CN');
    expect(match(['zh-SG'])).toBe('zh-CN');
    expect(match(['en-NZ'])).toBe('en-AU');
    expect(match(['en-IE'])).toBe('en-GB');
    expect(match(['en'])).toBe('en-US');
    expect(match(['in-ID'])).toBe('id');
  });

  it('ignores an alias whose target is not supported', () => {
    // en-NZ → en-AU is an alias; without en-AU it must still land on English.
    expect(match(['en-NZ'], ['en-GB', 'ja'], 'ja')).toBe('en-GB');
  });

  it('walks the preference list before falling back', () => {
    // Swahili first, French second: French is honoured, the default is not used.
    expect(match(['sw-KE', 'fr-FR', 'en-US'])).toBe('fr');
    // The same list on a build without French lands on English by preference.
    expect(match(['sw-KE', 'fr-FR', 'en-GB'], KAITU, 'zh-CN')).toBe('en-GB');
  });

  it('uses the fallback only when nothing in the list matches', () => {
    expect(match(['sw-KE', 'nl'])).toBe('en-US');
    expect(match(['fr-FR'], KAITU, 'zh-CN')).toBe('zh-CN');
    expect(match([])).toBe('en-US');
    expect(match(['', '  '])).toBe('en-US');
  });

  it('never returns a locale outside the supported list', () => {
    for (const tag of ['ar', 'fa', 'ko', 'es-MX', 'th-TH', 'ms-MY']) {
      expect(KAITU).toContain(match([tag], KAITU, 'zh-CN'));
    }
  });
});
