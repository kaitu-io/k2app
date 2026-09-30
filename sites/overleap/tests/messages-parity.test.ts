import { describe, it, expect } from 'vitest';
import { existsSync, readFileSync, readdirSync } from 'fs';
import path from 'path';
import { NAMESPACES } from '../messages/namespaces';
import { DEFAULT_LOCALE, LOCALES } from '@/lib/site';

const DIR = path.resolve(__dirname, '../messages');

function keys(obj: unknown, prefix = ''): string[] {
  if (typeof obj !== 'object' || obj === null) return [prefix];
  return Object.entries(obj).flatMap(([k, v]) => keys(v, prefix ? `${prefix}.${k}` : k));
}
const load = (locale: string, ns: string) => JSON.parse(readFileSync(path.join(DIR, locale, `${ns}.json`), 'utf8'));

describe('messages', () => {
  it('every file on disk is a registered namespace', () => {
    for (const locale of LOCALES) {
      const onDisk = readdirSync(path.join(DIR, locale)).map((f) => f.replace(/\.json$/, ''));
      expect(onDisk.sort()).toEqual([...NAMESPACES].sort());
    }
  });

  describe.each(LOCALES.filter((l) => l !== DEFAULT_LOCALE))('%s', (locale) => {
    it.each(NAMESPACES)('%s has exactly the master keys', (ns) => {
      expect(existsSync(path.join(DIR, locale, `${ns}.json`))).toBe(true);
      expect(keys(load(locale, ns)).sort()).toEqual(keys(load(DEFAULT_LOCALE, ns)).sort());
    });
  });

  it.each(LOCALES)('%s: no empty strings', (locale) => {
    for (const ns of NAMESPACES) {
      const empty = keys(load(locale, ns)).filter((k) => {
        const v = k.split('.').reduce<unknown>((o, p) => (o as Record<string, unknown>)[p], load(locale, ns));
        return typeof v === 'string' && v.trim() === '';
      });
      expect(empty, `${locale}/${ns}`).toEqual([]);
    }
  });
});
