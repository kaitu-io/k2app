/**
 * Guard: the language registry, the locale files on disk and the brands'
 * whitelists agree, and every locale keeps en-US's keys, placeholders and
 * markup — checked through the same script the translation workflow runs.
 *
 * Blind spot: nothing here judges translation quality.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { languages, type LanguageCode } from '../i18n';
import { namespaces } from '../locales/namespaces';
import { KAITU_BRAND } from '../../brands/kaitu';
import { OVERLEAP_BRAND } from '../../brands/overleap';
// @ts-expect-error — plain .mjs script, shared with the translation workflow
import { checkLocale } from '../../../../scripts/i18n-check-translation.mjs';

const LOCALES = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../locales');
const MASTER = 'en-US';
const ALL = Object.keys(languages) as LanguageCode[];

describe('locale coverage', () => {
  it('registers a plausible number of languages (guards the loops below)', () => {
    expect(ALL.length).toBeGreaterThan(20);
  });

  it.each(ALL)('%s carries every namespace', (lang) => {
    const onDisk = fs
      .readdirSync(path.join(LOCALES, lang))
      .filter((f) => f.endsWith('.json'))
      .map((f) => f.slice(0, -5));
    expect(onDisk.sort()).toEqual([...namespaces].sort());
  });

  it.each(ALL.filter((l) => l !== MASTER))('%s matches en-US keys, placeholders and markup', (lang) => {
    expect(checkLocale(lang, ['webapp'])).toEqual([]);
  });
});

describe('brand language whitelists', () => {
  it('overleap offers every registered language', () => {
    expect([...OVERLEAP_BRAND.locales].sort()).toEqual([...ALL].sort());
  });

  it('kaitu offers its seven languages', () => {
    expect([...KAITU_BRAND.locales].sort()).toEqual(['en-AU', 'en-GB', 'en-US', 'ja', 'zh-CN', 'zh-HK', 'zh-TW']);
  });

  it.each([KAITU_BRAND, OVERLEAP_BRAND])('$id lists no duplicates and includes its default', (brand) => {
    expect(new Set(brand.locales).size).toBe(brand.locales.length);
    expect(brand.locales).toContain(brand.defaultLocale);
  });
});
