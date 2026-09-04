/**
 * Locale key parity for web/messages — per brand.
 *
 * Message files partition by brand (kaitu = zh-*, overleap = en-* + ja), and each brand
 * loads only its own namespaces (messages/namespaces.ts BRAND_NAMESPACES). So parity is
 * checked per brand: every locale of a brand exposes exactly the key set of that brand's
 * default locale, namespace by namespace. Missing keys render as raw "ns.key" text on a
 * real page; extra keys are dead weight that hides real drift.
 *
 * Arrays are leaves (a list's shape is content, not structure) — same rule as the
 * webapp checker.
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { BRAND_NAMESPACES } from '../messages/namespaces';
import { KAITU, OVERLEAP } from '../src/lib/brands';

const MESSAGES_DIR = path.resolve(__dirname, '../messages');

function flatKeys(value: unknown, prefix = '', out: Set<string> = new Set()): Set<string> {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      flatKeys(v, prefix ? `${prefix}.${k}` : k, out);
    }
  } else if (prefix) {
    out.add(prefix);
  }
  return out;
}

function readKeys(locale: string, ns: string): Set<string> | null {
  const p = path.join(MESSAGES_DIR, locale, `${ns}.json`);
  if (!fs.existsSync(p)) return null;
  return flatKeys(JSON.parse(fs.readFileSync(p, 'utf8')));
}

describe.each([KAITU, OVERLEAP])('messages-parity for $id: every locale mirrors the brand default key-for-key', (brand) => {
  const base = brand.defaultLocale;
  for (const ns of BRAND_NAMESPACES[brand.id]) {
    const baseKeys = readKeys(base, ns);

    it(`${base}/${ns}.json exists and is non-empty (guards the parser)`, () => {
      expect(baseKeys).not.toBeNull();
      expect(baseKeys!.size).toBeGreaterThan(0);
    });

    for (const locale of brand.allowedLocales) {
      if (locale === base) continue;
      it(`${locale}/${ns}.json has exactly the ${base} key set`, () => {
        const keys = readKeys(locale, ns);
        expect(keys, `${locale}/${ns}.json is missing`).not.toBeNull();
        const missing = [...baseKeys!].filter((k) => !keys!.has(k)).sort();
        const extra = [...keys!].filter((k) => !baseKeys!.has(k)).sort();
        expect({ missing, extra }).toEqual({ missing: [], extra: [] });
      });
    }
  }
});

describe('no locale carries the other brand\'s namespaces', () => {
  // A stray en-US/hero.json (kaitu narrative in English) is exactly the dead file that
  // used to ship the kaitu home copy to the overleap deployment.
  it.each([
    [OVERLEAP, BRAND_NAMESPACES.kaitu.filter((ns) => !(BRAND_NAMESPACES.overleap as readonly string[]).includes(ns))],
    [KAITU, BRAND_NAMESPACES.overleap.filter((ns) => !(BRAND_NAMESPACES.kaitu as readonly string[]).includes(ns))],
  ] as const)('$0.id locales have none of the other brand\'s namespace files', (brand, foreign) => {
    const stray = brand.allowedLocales.flatMap((loc) =>
      foreign.filter((ns) => fs.existsSync(path.join(MESSAGES_DIR, loc, `${ns}.json`))).map((ns) => `${loc}/${ns}.json`),
    );
    expect(stray).toEqual([]);
  });
});
