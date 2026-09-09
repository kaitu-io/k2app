import { siteBrand, type Brand } from './brands';

/**
 * Legal documents under `public/legal/` ship one file per document holding two
 * language masters, separated by these headings.
 *
 * Two things were wrong with rendering the raw file, and both are fixed here:
 *
 *  1. Every page rendered BOTH masters, so an English page carried a full
 *     Chinese copy of the policy above it.
 *  2. The masters named one brand literally, so the other deployment served a
 *     policy about a company the visitor had never heard of — and, worse, gave
 *     that company's contact address for data-subject requests. The URLs are
 *     filed with Apple and Google, so this was live in two app store records.
 *
 * The locale picks the master; the placeholders below keep both masters
 * brand-neutral **on disk**, which is what lets tests/brand-guard.test.ts
 * require `public/legal/` to hold zero brand literals of either brand. Deriving
 * the words from the registry instead of writing them per file is the point: a
 * third brand, or a renamed one, needs no edit here.
 */
const ZH_HEADING = '## 中文版本';
const EN_HEADING = '## English Version';

/**
 * The zh master serves the zh locales, the en master everything else — ja
 * included, since no Japanese master has been written. Falling back to English
 * is deliberate: an untranslated legal text is readable, a half-translated one
 * is a liability.
 */
export function isChineseLocale(locale: string): boolean {
  return locale.startsWith('zh');
}

/**
 * Brand words are locale-sensitive on purpose. `wordmark` is the form a reader
 * of that language expects (root CLAUDE.md bans the latin form in Chinese
 * user-facing copy); `displayName` is the latin form. They are equal for brands
 * that have only one.
 */
function tokens(brand: Brand, zh: boolean): Record<string, string> {
  return {
    brand: zh ? brand.wordmark : brand.displayName,
    legalName: brand.legalName,
    siteHost: brand.baseUrl.replace(/^https?:\/\//, ''),
    siteUrl: brand.baseUrl,
    privacyEmail: brand.privacyEmail,
    legalEmail: brand.legalEmail,
    supportEmail: brand.contactEmail,
  };
}

/**
 * Split a bilingual legal document to one language and fill in its brand.
 *
 * A file with no heading markers is returned whole (still substituted) rather
 * than blank: a document that has not been split yet must not silently render
 * as an empty page.
 */
export function renderLegalDoc(raw: string, locale: string, brand: Brand = siteBrand()): string {
  const zh = isChineseLocale(locale);
  const zhAt = raw.indexOf(ZH_HEADING);
  const enAt = raw.indexOf(EN_HEADING);

  let body = raw;
  if (zhAt !== -1 && enAt !== -1 && zhAt < enAt) {
    // The heading line itself is a marker, not content — "English Version" as a
    // title reads as nonsense once the Chinese half is gone — so slice past it.
    // The bilingual preamble above it goes too; each master carries its own
    // "last updated" line so the date survives the split.
    body = zh
      ? raw.slice(zhAt + ZH_HEADING.length, enAt)
      : raw.slice(enAt + EN_HEADING.length);
  }

  return body.replace(/\{\{(\w+)\}\}/g, (whole, key: string) => {
    const value = tokens(brand, zh)[key];
    // An unknown placeholder stays visible instead of resolving to an empty
    // string: a blank where the company name belongs reads as finished text,
    // and nothing downstream would ever flag it.
    return value ?? whole;
  });
}
