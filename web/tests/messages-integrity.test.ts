import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

const MESSAGES_DIR = path.resolve(__dirname, '../messages');

const ZH_LOCALES = ['zh-CN', 'zh-TW', 'zh-HK'] as const;
const EN_LOCALES = ['en-US', 'en-GB', 'en-AU'] as const;
const OTHER_LOCALES = ['ja'] as const;

// Forbidden political/legacy tokens. These must not appear anywhere in hero.json.
const FORBIDDEN_TERMS_ALL = [
  '审查',         // zh
  'GFW',          // any
  '翻墙',         // zh
  '封锁',         // zh — soften to 干扰/阻断 per table
  '防火墙',       // zh
  '対抗審査',      // ja legacy
  '検閲',          // ja
  'censorship',    // en
  'circumvention', // en
  'anti-censorship', // en
];

// For zh-* hero.json: "Kaitu" must never appear as a standalone token (only inside
// "Kaitu by Overleap", "Kaitu LLC", "Kaitu.io" wordmark, none of which are used in hero.json).
// Regex: Kaitu not immediately followed by " by Overleap" / " LLC" / ".io" / "-"
const ZH_KAITU_BARE = /Kaitu(?!(\s+by\s+Overleap|\s+LLC|\.io|-))/;

// For en-*/ja hero.json: similar rule; "kaitu" (case-insensitive) forbidden except in
// the same exception substrings.
const EN_JA_KAITU_BARE = /kaitu(?!(\s+by\s+overleap|\s+llc|\.io|-))/i;

function readHero(locale: string): string {
  const p = path.join(MESSAGES_DIR, locale, 'hero.json');
  return fs.readFileSync(p, 'utf8');
}

function readHeroParsed(locale: string): Record<string, unknown> {
  return JSON.parse(readHero(locale));
}

describe('messages-integrity: forbidden political tokens', () => {
  const allLocales = [...ZH_LOCALES, ...EN_LOCALES, ...OTHER_LOCALES];
  for (const locale of allLocales) {
    for (const term of FORBIDDEN_TERMS_ALL) {
      it(`${locale}/hero.json does not contain "${term}"`, () => {
        const raw = readHero(locale);
        expect(raw, `Found forbidden term "${term}" in ${locale}/hero.json`).not.toContain(term);
      });
    }
  }
});

describe('messages-integrity: brand tokens per locale family', () => {
  for (const locale of ZH_LOCALES) {
    it(`${locale}/hero.json uses "开途" not standalone "Kaitu"`, () => {
      const raw = readHero(locale);
      expect(raw, `Found standalone "Kaitu" in ${locale}/hero.json (use "开途" or "Kaitu by Overleap")`).not.toMatch(ZH_KAITU_BARE);
    });
  }
  for (const locale of [...EN_LOCALES, ...OTHER_LOCALES]) {
    it(`${locale}/hero.json uses "Overleap" not standalone "Kaitu"/"kaitu"`, () => {
      const raw = readHero(locale);
      expect(raw, `Found standalone "kaitu" in ${locale}/hero.json (use "Overleap" or "Kaitu by Overleap")`).not.toMatch(EN_JA_KAITU_BARE);
    });
  }
});

describe('messages-integrity: FAQ key migration', () => {
  const allLocales = [...ZH_LOCALES, ...EN_LOCALES, ...OTHER_LOCALES];
  for (const locale of allLocales) {
    it(`${locale}/hero.json: faq.items.comparisonWithOthers removed`, () => {
      const j = readHeroParsed(locale) as { faq?: { items?: Record<string, unknown> } };
      expect(j.faq?.items).toBeDefined();
      expect(j.faq?.items).not.toHaveProperty('comparisonWithOthers');
    });
    it(`${locale}/hero.json: faq.items.gfwSpeed renamed to networkThrottlingSpeed`, () => {
      const j = readHeroParsed(locale) as { faq?: { items?: Record<string, unknown> } };
      expect(j.faq?.items).not.toHaveProperty('gfwSpeed');
      expect(j.faq?.items).toHaveProperty('networkThrottlingSpeed');
    });
  }
});
