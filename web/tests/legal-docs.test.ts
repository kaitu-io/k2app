/**
 * Legal documents render per brand and per language, against the real files.
 *
 * These exist because tests/static-pages-ssr.test.ts mocks `fs/promises` to a
 * one-line stub, so it asserts the privacy/terms pages are Server Components
 * and nothing about what they actually serve. Under that coverage,
 * overleap.io/privacy told visitors they were registering a Kaitu account and
 * gave a kaitu.io address for data-subject requests, and every page rendered a
 * full Chinese copy of the policy above the English one.
 *
 * So read the shipped markdown here, run it through the real renderer for both
 * brands, and assert on the output a reader would see.
 */
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { KAITU, OVERLEAP, type Brand } from '../src/lib/brands';
import { renderLegalDoc } from '../src/lib/legal';

const LEGAL = path.resolve(__dirname, '../public/legal');

/** Documents both deployments serve. retailer-rules is kaitu-only (page.kaitu.tsx). */
const SHARED_DOCS = ['privacy-policy', 'terms-of-service', 'delete-account'] as const;

const read = (doc: string) => readFileSync(path.join(LEGAL, `${doc}.md`), 'utf8');

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const OVERLEAP_WORDS = /[Oo]verleap/;

/** One locale per language master, per brand. */
const CASES: { brand: Brand; locale: string; zh: boolean }[] = [
  { brand: KAITU, locale: 'zh-CN', zh: true },
  { brand: KAITU, locale: 'zh-TW', zh: true },
  { brand: OVERLEAP, locale: 'en-GB', zh: false },
  { brand: OVERLEAP, locale: 'ja', zh: false },
];

describe.each(SHARED_DOCS)('%s', (doc) => {
  const raw = read(doc);

  it('holds no brand literal on disk — the words come from the registry', () => {
    expect(raw.match(KAITU_WORDS)).toBeNull();
    expect(raw.match(OVERLEAP_WORDS)).toBeNull();
  });

  it.each(CASES)('$brand.id / $locale renders one language, fully substituted', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);

    // Substitution is complete. An unresolved {{token}} on a legal page is the
    // kind of thing that ships: it reads as a typo, not as a missing value.
    expect(out).not.toMatch(/\{\{/);

    // The split happened: exactly one language master survives. Section
    // numbering is the discriminator — the zh master numbers 一、二、三, the
    // en master 1. 2. 3. — because both masters share every other marker.
    expect(out).not.toContain('## 中文版本');
    expect(out).not.toContain('## English Version');
    if (zh) {
      expect(out).toContain('### 一、');
      expect(out).not.toContain('### 1. ');
    } else {
      expect(out).toContain('### 1. ');
      expect(out).not.toContain('### 一、');
    }

    // Each master carries its own date; the bilingual preamble that used to
    // hold it is dropped with the other language.
    expect(out).toMatch(zh ? /\*\*最后更新：/ : /\*\*Last updated: /);

    // The reader sees this deployment's brand and only this deployment's brand.
    // Legal-signature exception (root CLAUDE.md, 法务文书署名): both deployments
    // sign as Overleap LLC, so strip exactly that string first — the same
    // scoping tests/brand-leak-ssr.test.tsx uses — and assert separately that it
    // is present, so the exception can't widen into a general free pass.
    expect(out).toContain(brand.legalName);
    const body = out.replaceAll(brand.legalName, '');
    const own = brand.id === 'kaitu' ? KAITU_WORDS : OVERLEAP_WORDS;
    const other = brand.id === 'kaitu' ? OVERLEAP_WORDS : KAITU_WORDS;
    expect(body).toMatch(own);
    expect(body.match(other)).toBeNull();
  });

  it.each(CASES)('$brand.id / $locale names only its own contact addresses', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    const foreign = (brand.id === 'kaitu' ? OVERLEAP : KAITU);
    for (const addr of [foreign.privacyEmail, foreign.legalEmail, foreign.contactEmail]) {
      expect(out).not.toContain(addr);
    }
  });
});

describe('delete-account satisfies the Play Data safety URL requirements', () => {
  // Play requires the linked page to (a) name the app or developer shown in the
  // listing, (b) show the steps prominently, and (c) state which data is deleted
  // and which is retained, with any extra retention period. A reviewer checks
  // all three by eye; these assert the substance survives an edit.
  const raw = read('delete-account');

  it.each(CASES)('$brand.id / $locale names the app and the developer', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toContain(brand.legalName);
  });

  it.each(CASES)('$brand.id / $locale gives numbered in-app steps', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toMatch(/^1\. /m);
    expect(out).toMatch(/^4\. /m);
    // The step list must name the control the app actually shows, or the
    // reviewer cannot follow it.
    expect(out).toContain(zh ? '注销账号' : 'Delete Account');
  });

  it.each(CASES)('$brand.id / $locale states retention periods, not just deletion', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toMatch(zh ? /保留 7 年/ : /kept for 7 years/);
    expect(out).toMatch(zh ? /30 天内/ : /within 30 days/);
  });

  it.each(CASES)('$brand.id / $locale offers a route for uninstalled users', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toContain(brand.privacyEmail);
  });
});

describe('renderLegalDoc edge cases', () => {
  it('returns a document with no language markers whole, still substituted', () => {
    const out = renderLegalDoc('Operated by {{legalName}}.', 'en-GB', OVERLEAP);
    expect(out).toBe('Operated by Overleap LLC.');
  });

  it('leaves an unknown placeholder visible rather than blanking it', () => {
    // A blank where a company name belongs reads as finished prose, and nothing
    // downstream would flag it.
    expect(renderLegalDoc('Contact {{nope}}.', 'en-GB', OVERLEAP)).toBe('Contact {{nope}}.');
  });

  it('uses the wordmark in Chinese and the latin name elsewhere', () => {
    // Root CLAUDE.md bans the latin form in Chinese user-facing copy, so the
    // token has to be locale-sensitive rather than one fixed string.
    expect(renderLegalDoc('{{brand}}', 'zh-CN', KAITU)).toBe(KAITU.wordmark);
    expect(renderLegalDoc('{{brand}}', 'en-GB', KAITU)).toBe(KAITU.displayName);
  });
});
