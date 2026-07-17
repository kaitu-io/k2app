/**
 * Guard: base locale files must be brand-neutral.
 * Brand identity enters strings only via {{brand}}/{{brandDomain}}/
 * {{brandBaseUrl}}/{{supportEmail}} interpolation (Task 4) or via the
 * per-brand overlay dir src/brands/<brand>/locales/ (which this test does NOT scan).
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const localesDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../locales');
const FORBIDDEN = /开途|開途|Kaitu|kaitu\.io|Overleap|overleap\.io/;

describe('locale files are brand-neutral', () => {
  const langs = fs
    .readdirSync(localesDir)
    .filter((d) => fs.statSync(path.join(localesDir, d)).isDirectory());

  it('found the expected 7 locale dirs', () => {
    expect(langs.sort()).toEqual(['en-AU', 'en-GB', 'en-US', 'ja', 'zh-CN', 'zh-HK', 'zh-TW']);
  });

  for (const lang of langs) {
    it(`${lang} contains no brand literals`, () => {
      const violations: string[] = [];
      for (const f of fs.readdirSync(path.join(localesDir, lang))) {
        if (!f.endsWith('.json')) continue;
        const text = fs.readFileSync(path.join(localesDir, lang, f), 'utf8');
        for (const [i, line] of text.split('\n').entries()) {
          if (FORBIDDEN.test(line)) violations.push(`${lang}/${f}:${i + 1}: ${line.trim()}`);
        }
      }
      expect(violations).toEqual([]);
    });
  }
});
