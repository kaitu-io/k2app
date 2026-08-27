/**
 * Locale key parity for web/messages: every locale exposes exactly the key set
 * of en-US, namespace by namespace. Missing keys render as raw "ns.key" text on
 * a real page; extra keys are dead weight that hides real drift.
 *
 * Why this exists: webapp/ has had this gate (scripts/check-i18n.mjs, run by
 * `yarn build`) for a long time; web/ never did. At 0.4.8 web carried 11
 * divergences — all dead keys, so nothing rendered wrong, but nothing would
 * have said so if one had been live. The repo's own lesson
 * (tests/messages-integrity.test.ts, mocked next-intl in unit tests) is that
 * structural i18n errors only surface in a real browser; this catches the
 * structural half at the JSON.
 *
 * Arrays are leaves (a list's shape is content, not structure) — same rule as
 * the webapp checker.
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { namespaces } from '../messages/namespaces';

const MESSAGES_DIR = path.resolve(__dirname, '../messages');
const BASE_LOCALE = 'en-US';

const LOCALES = fs
  .readdirSync(MESSAGES_DIR)
  .filter((d) => fs.statSync(path.join(MESSAGES_DIR, d)).isDirectory())
  .sort();

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

describe('messages-parity: every locale mirrors en-US key-for-key', () => {
  it('discovers en-US plus at least one other locale', () => {
    expect(LOCALES).toContain(BASE_LOCALE);
    expect(LOCALES.length).toBeGreaterThan(1);
  });

  for (const ns of namespaces) {
    const base = readKeys(BASE_LOCALE, ns);

    it(`${BASE_LOCALE}/${ns}.json exists and is non-empty (guards the parser)`, () => {
      expect(base).not.toBeNull();
      expect(base!.size).toBeGreaterThan(0);
    });

    for (const locale of LOCALES) {
      if (locale === BASE_LOCALE) continue;
      it(`${locale}/${ns}.json has exactly the ${BASE_LOCALE} key set`, () => {
        const keys = readKeys(locale, ns);
        expect(keys, `${locale}/${ns}.json is missing`).not.toBeNull();
        const missing = [...base!].filter((k) => !keys!.has(k)).sort();
        const extra = [...keys!].filter((k) => !base!.has(k)).sort();
        expect({ missing, extra }).toEqual({ missing: [], extra: [] });
      });
    }
  }
});
