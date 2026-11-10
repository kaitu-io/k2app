/**
 * kaitu.io and the kaitu app offer the same languages (2026-11-10 decision):
 * a visitor who reads the site in Korean must find Korean in the app, and the
 * other way round. The app's list lives in webapp/src/brands/kaitu/index.ts —
 * a separate package, so the source is parsed rather than imported.
 *
 * Blind spot: only the list is compared, not whether either side's copy exists
 * (messages-parity / the webapp locale-coverage test cover that per side).
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { KAITU } from '../src/lib/brands';

const APP_BRAND = path.resolve(__dirname, '../../webapp/src/brands/kaitu/index.ts');

function appLocales(): string[] {
  const src = fs.readFileSync(APP_BRAND, 'utf8');
  const m = src.match(/^\s*locales:\s*\[([^\]]*)\]/m);
  if (!m) throw new Error(`no "locales: [...]" line in ${APP_BRAND}`);
  return [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1]);
}

describe('site and app language lists', () => {
  it('parses a plausible app list (guards the regex)', () => {
    expect(appLocales()).toContain('zh-CN');
    expect(appLocales().length).toBeGreaterThanOrEqual(3);
  });

  it('kaitu.io serves exactly the languages the kaitu app offers', () => {
    expect([...KAITU.allowedLocales].sort()).toEqual(appLocales().sort());
  });

  it('routing.ts lists the same locales as brands.ts (its inline mirror)', () => {
    // routing.ts pulls next/navigation, which does not load under vitest — parse it.
    const src = fs.readFileSync(path.resolve(__dirname, '../src/i18n/routing.ts'), 'utf8');
    const m = src.match(/^\s*locales:\s*\[([^\]]*)\]/m);
    expect(m).not.toBeNull();
    const routed = [...m![1].matchAll(/'([^']+)'/g)].map((x) => x[1]);
    expect(routed).toEqual([...KAITU.allowedLocales]);
  });
});
