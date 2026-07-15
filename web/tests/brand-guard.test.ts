/**
 * Brand-leak guard (spec: 两站互不感知; CI 品牌串包守卫).
 *
 * Locale partition invariant: kaitu serves the zh locales, overleap serves the
 * en locales plus ja — message values may therefore carry brand names literally
 * per locale file.
 * Source files must not carry user-facing brand literals outside the
 * allowlist (the registry itself + kaitu-only gated surfaces).
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'fs';
import path from 'path';
import { KAITU, OVERLEAP } from '../src/lib/brands';

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

describe('messages: locale files carry only their own brand', () => {
  it.each([...OVERLEAP.allowedLocales])('%s has zero kaitu words', (loc) => {
    expect(scan(path.join(WEB, 'messages', loc), KAITU_WORDS, (f) => f.endsWith('.json'))).toEqual([]);
  });
  it.each([...KAITU.allowedLocales])('%s has zero overleap words', (loc) => {
    expect(
      scan(path.join(WEB, 'messages', loc), OVERLEAP_WORDS, (f) => f.endsWith('.json'), OVERLEAP_ORG_URL),
    ).toEqual([]);
  });
  it('locale sets stay disjoint (the invariant this guard relies on)', () => {
    const overlap = KAITU.allowedLocales.filter((l) => (OVERLEAP.allowedLocales as readonly string[]).includes(l));
    expect(overlap).toEqual([]);
  });
});

describe('src: no user-facing brand literals outside the allowlist', () => {
  const SRC_ALLOW = [
    'src/lib/brands.ts',              // the registry IS the brand data
    'src/payload/',                   // Payload admin config — kaitu-deployment-only
    'src/app/[locale]/routers/',      // routers surface gated off overleap (features.routers)
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

describe('velite content served on overleap carries no kaitu words', () => {
  // en-US/ja markdown renders on the overleap deployment. Files frontmattered
  // `brand: kaitu` are exempt because they are genuinely unreachable there: the
  // /k2 route filters them out of findK2Post, the sidebar, generateStaticParams
  // and the sitemap, and off-brand requests 404 (tests/k2-route.test.ts →
  // test_k2_docs_are_brand_gated). The exemption rides on that gate — it is not
  // a licence to leave kaitu prose on a reachable page.
  const isOverleapContent = (f: string) =>
    f.endsWith('.md') && !/^brand:\s*kaitu\s*$/m.test(readFileSync(f, 'utf8'));

  it.each(['en-US', 'ja'].filter((l) => {
    try { return statSync(path.join(WEB, 'content', l)).isDirectory(); } catch { return false; }
  }))('content/%s has zero kaitu words (outside brand: kaitu files)', (loc) => {
    expect(scan(path.join(WEB, 'content', loc), KAITU_WORDS, isOverleapContent)).toEqual([]);
  });
});
