import type { Brand } from './brands';

/**
 * Locales to prerender a content page (guide / k2 doc) in at build time.
 *
 * A locale with its own version is prerendered. A locale without one only gets
 * the default-locale fallback prerendered when its readers read that language
 * (zh-TW / zh-HK reading the zh-CN text). A foreign-language fallback of a
 * Chinese post is noindex + canonical zh-CN (app/[locale]/metadata.ts), so it
 * is NOT prerendered — the URL still works, rendered on demand (no route sets
 * `dynamicParams = false`).
 *
 * Why it matters: each prerendered page carries the full locale message bundle
 * in its .html and .rsc (~300-450 KB per page). Prerendering every Chinese post
 * in en-US / ko / ja / ru added ~55 MB and pushed the Amplify build output past
 * its 220 MiB cap (job 145, 2026-11-10).
 */
export function prerenderLocales(
  ownLocales: readonly string[],
  brand: Pick<Brand, 'allowedLocales' | 'defaultLocale'>
): string[] {
  const lang = (l: string) => l.split('-')[0];
  const hasFallback = ownLocales.includes(brand.defaultLocale);
  return brand.allowedLocales.filter(
    (l) => ownLocales.includes(l) || (hasFallback && lang(l) === lang(brand.defaultLocale))
  );
}
