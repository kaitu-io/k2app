/**
 * Brand-leak guard (root CLAUDE.md: the two brands never mention each other in
 * any user-facing context — the legal signature "Overleap LLC" excepted).
 *
 * This site serves kaitu only (overleap.io is sites/overleap/). Message files may
 * carry the kaitu wordmark literally; they must never carry the other brand.
 * Source files must not carry user-facing brand literals outside the allowlist
 * (the registry itself + the routers surface).
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'fs';
import path from 'path';
import { KAITU } from '../src/lib/brands';

const WEB = path.resolve(__dirname, '..');
const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const OVERLEAP_WORDS = /[Oo]verleap/;
/**
 * Protocol-layer GitHub org — globally shared, so an `overleap` substring inside
 * this URL is not a brand leak (naming strategy doc).
 *
 * Passed ONLY to the overleap-word scans. It used to be a module-level constant
 * applied inside scan() to every scan, which meant any line containing
 * `github.com/getoverleap` also had its Kaitu / 开途 / kaitu.io occurrences
 * waved through — an exemption written for one brand's scan silently issuing a
 * free pass to the other's.
 */
const OVERLEAP_ORG_URL = /github\.com\/getoverleap/;

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else yield p;
  }
}

function scan(
  root: string,
  re: RegExp,
  fileFilter: (p: string) => boolean,
  lineAllow?: RegExp,
): string[] {
  const hits: string[] = [];
  for (const file of walk(root)) {
    if (!fileFilter(file)) continue;
    readFileSync(file, 'utf8').split('\n').forEach((line, i) => {
      if (re.test(line) && !(lineAllow && lineAllow.test(line))) {
        hits.push(`${path.relative(WEB, file)}:${i + 1}: ${line.trim().slice(0, 120)}`);
      }
    });
  }
  return hits;
}

describe('messages: locale files never carry the other brand', () => {
  it.each([...KAITU.allowedLocales])('%s has zero overleap words', (loc) => {
    expect(
      scan(path.join(WEB, 'messages', loc), OVERLEAP_WORDS, (f) => f.endsWith('.json'), OVERLEAP_ORG_URL),
    ).toEqual([]);
  });
});

describe('src: no user-facing brand literals outside the allowlist', () => {
  const SRC_ALLOW = [
    'src/lib/brands.ts',              // the registry IS the brand data
    'src/app/[locale]/routers/',      // routers surface (开途-worded product copy)
  ];
  const isSource = (f: string) =>
    (f.endsWith('.ts') || f.endsWith('.tsx')) &&
    !f.includes('__tests__') && !/\.(test|spec)\./.test(f) &&
    !SRC_ALLOW.some((a) => path.relative(WEB, f).startsWith(a));

  it('no kaitu literals', () => {
    expect(scan(path.join(WEB, 'src'), /开途|開途|kaitu\.(io|me)|\bKaitu\b/, isSource)).toEqual([]);
  });
  it("no overleap literals (brand ids like 'overleap' are fine — only the display word/domain are banned)", () => {
    expect(scan(path.join(WEB, 'src'), /\bOverleap\b|overleap\.io/, isSource, OVERLEAP_ORG_URL)).toEqual([]);
  });
});

describe('the getoverleap exemption does not leak across brands', () => {
  // Guard-on-the-guard. The exemption exists so the shared protocol org URL
  // doesn't read as an Overleap brand mention; it must never excuse a kaitu
  // mention that happens to share a line with it.
  const line = 'see https://github.com/getoverleap/k2 — built by Kaitu, docs at kaitu.io';

  it('waves the org URL past the overleap scan', () => {
    expect(OVERLEAP_WORDS.test(line) && OVERLEAP_ORG_URL.test(line)).toBe(true);
  });

  it('but never past the kaitu scan', () => {
    // The kaitu scans get no lineAllow at all, so a kaitu word on this line is
    // still a hit. Previously scan() applied the exemption unconditionally and
    // this line passed every scan clean.
    expect(KAITU_WORDS.test(line)).toBe(true);
  });
});

describe('public/legal: documents carry no brand literals of either brand', () => {
  // The legal markdown lives outside messages/, src/ and content/. Brand words
  // are substituted at render time from the registry (src/lib/legal.ts), so the
  // files themselves carry neither brand's words.
  //
  // retailer-rules.md is exempt: the 分销 programme copy is 开途-worded.
  const LEGAL = path.join(WEB, 'public/legal');
  const isSharedLegal = (f: string) =>
    f.endsWith('.md') && path.basename(f) !== 'retailer-rules.md';

  it('no kaitu literals', () => {
    expect(scan(LEGAL, KAITU_WORDS, isSharedLegal)).toEqual([]);
  });
  it('no overleap literals', () => {
    expect(scan(LEGAL, OVERLEAP_WORDS, isSharedLegal, OVERLEAP_ORG_URL)).toEqual([]);
  });
});
