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
// Protocol-layer GitHub org — globally shared, allowed everywhere (naming strategy doc).
const LINE_ALLOW = /github\.com\/getoverleap/;

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else yield p;
  }
}

function scan(root: string, re: RegExp, fileFilter: (p: string) => boolean): string[] {
  const hits: string[] = [];
  for (const file of walk(root)) {
    if (!fileFilter(file)) continue;
    readFileSync(file, 'utf8').split('\n').forEach((line, i) => {
      if (re.test(line) && !LINE_ALLOW.test(line)) {
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
    expect(scan(path.join(WEB, 'messages', loc), OVERLEAP_WORDS, (f) => f.endsWith('.json'))).toEqual([]);
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
    expect(scan(path.join(WEB, 'src'), /\bOverleap\b|overleap\.io/, isSource)).toEqual([]);
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
