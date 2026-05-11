import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { namespaces } from '../messages/namespaces';

const MESSAGES_DIR = path.resolve(__dirname, '../messages');

const ZH_LOCALES = ['zh-CN', 'zh-TW', 'zh-HK'] as const;
const EN_LOCALES = ['en-US', 'en-GB', 'en-AU'] as const;
const OTHER_LOCALES = ['ja'] as const;
const ALL_LOCALES = [...ZH_LOCALES, ...EN_LOCALES, ...OTHER_LOCALES];

// Forbidden political/legacy tokens. Each has a `matcher` (regex) so we can express
// exceptions (e.g. 安全审查 = "security audit", a legitimate non-political term).
type ForbiddenTerm = { term: string; matcher: RegExp };

const FORBIDDEN_TERMS: ForbiddenTerm[] = [
  // zh — simplified
  { term: '审查', matcher: /(?<!安全)审查/ },   // allow 安全审查 ("security audit")
  { term: '翻墙', matcher: /翻墙/ },
  { term: '封锁', matcher: /封锁/ },             // soften to 干扰/阻断
  { term: '防火墙', matcher: /防火墙/ },
  // zh — traditional
  { term: '審查', matcher: /(?<!安全)審查/ },   // allow 安全審查 ("security audit")
  { term: '翻牆', matcher: /翻牆/ },
  { term: '封鎖', matcher: /封鎖/ },
  { term: '防火牆', matcher: /防火牆/ },
  // ja
  { term: '対抗審査', matcher: /対抗審査/ },
  { term: '検閲', matcher: /検閲/ },
  // en — cross-locale
  { term: 'GFW', matcher: /GFW/ },
  { term: 'censorship', matcher: /censorship/i },
  { term: 'circumvention', matcher: /circumvention/i },
  { term: 'anti-censorship', matcher: /anti-censorship/i },
];

// For zh-* content: "Kaitu" must never appear as a standalone token (only inside
// "Kaitu by Overleap", "Kaitu LLC", "Kaitu.io" wordmark).
// Regex: Kaitu not immediately followed by " by Overleap" / " LLC" / ".io" / ".service" / "-"
// Note: `.service` whitelist covers the Linux systemd unit name `kaitu.service`,
// which is hardcoded in packaging/linux/install.sh and cannot be renamed without
// breaking production installs (see install.json installSteps.linux).
const ZH_KAITU_BARE = /Kaitu(?!(\s+by\s+Overleap|\s+LLC|\.io|\.service|-))/;

// For en-*/ja content: similar rule; "kaitu" (case-insensitive) forbidden except in
// the same exception substrings. `.service` whitelist: same rationale as above.
// `/` whitelist: CDN paths like `dl.kaitu.io/kaitu/k2r/` and the fallback
// `d0.all7.cc/kaitu/k2r/` are real distribution URLs whose `kaitu/` directory
// segment cannot be renamed without breaking existing installs.
const EN_JA_KAITU_BARE = /kaitu(?!(\s+by\s+overleap|\s+llc|\.io|\.service|-|\/))/i;

function readNamespace(locale: string, namespace: string): string {
  const p = path.join(MESSAGES_DIR, locale, `${namespace}.json`);
  if (!fs.existsSync(p)) return '';
  return fs.readFileSync(p, 'utf8');
}

function readHero(locale: string): string {
  return readNamespace(locale, 'hero');
}

function readHeroParsed(locale: string): Record<string, unknown> {
  return JSON.parse(readHero(locale));
}

describe('messages-integrity: forbidden political tokens — all namespaces', () => {
  for (const locale of ALL_LOCALES) {
    for (const ns of namespaces) {
      for (const { term, matcher } of FORBIDDEN_TERMS) {
        it(`${locale}/${ns}.json does not contain "${term}"`, () => {
          const raw = readNamespace(locale, ns);
          if (!raw) return; // skip missing namespace files
          expect(
            raw,
            `Found forbidden term "${term}" in ${locale}/${ns}.json`
          ).not.toMatch(matcher);
        });
      }
    }
  }
});

describe('messages-integrity: brand tokens per locale family — all namespaces', () => {
  for (const locale of ZH_LOCALES) {
    for (const ns of namespaces) {
      it(`${locale}/${ns}.json uses "开途" not standalone "Kaitu"`, () => {
        const raw = readNamespace(locale, ns);
        if (!raw) return;
        expect(
          raw,
          `Found standalone "Kaitu" in ${locale}/${ns}.json (use "开途" or "Kaitu by Overleap")`
        ).not.toMatch(ZH_KAITU_BARE);
      });
    }
  }
  for (const locale of [...EN_LOCALES, ...OTHER_LOCALES]) {
    for (const ns of namespaces) {
      it(`${locale}/${ns}.json uses "Overleap" not standalone "Kaitu"/"kaitu"`, () => {
        const raw = readNamespace(locale, ns);
        if (!raw) return;
        expect(
          raw,
          `Found standalone "kaitu" in ${locale}/${ns}.json (use "Overleap" or "Kaitu by Overleap")`
        ).not.toMatch(EN_JA_KAITU_BARE);
      });
    }
  }
});

describe('messages-integrity: FAQ key migration (hero.json only)', () => {
  for (const locale of ALL_LOCALES) {
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
