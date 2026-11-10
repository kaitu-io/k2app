import type { Brand } from './brands';

/**
 * The hreflang alternates of one page — the single source for BOTH the page's
 * `<link rel="alternate">` tags (`app/[locale]/metadata.ts`) and its sitemap
 * entry (`app/sitemap.ts`). Search engines treat a mismatch between the two as
 * conflicting signals, so neither side builds the map by hand.
 *
 * - `en-US` is advertised as plain `en`: the English pages are written for any
 *   English reader in China, not for US residents only.
 * - A plain `zh` → zh-CN entry catches Chinese readers outside CN / TW / HK
 *   (Singapore, Malaysia …), who would otherwise fall through to x-default.
 * - x-default is the page a reader of none of these languages should get —
 *   `brand.xDefaultLocale` (English for this site, matching the root redirect for
 *   an unrecognised Accept-Language), else the default locale, else the first
 *   served one.
 */
const HREFLANG_CODE: Record<string, string> = { 'en-US': 'en' };

export function hreflangCode(locale: string): string {
  return HREFLANG_CODE[locale] ?? locale.toLowerCase();
}

export function xDefaultLocale(
  served: readonly string[],
  brand: Pick<Brand, 'defaultLocale' | 'xDefaultLocale'>
): string {
  if (served.includes(brand.xDefaultLocale)) return brand.xDefaultLocale;
  if (served.includes(brand.defaultLocale)) return brand.defaultLocale;
  return served[0];
}

export function hreflangAlternates(
  served: readonly string[],
  pathname: string,
  brand: Pick<Brand, 'baseUrl' | 'defaultLocale' | 'xDefaultLocale'>
): Record<string, string> {
  const url = (locale: string) => `${brand.baseUrl}/${locale}${pathname}`;
  const languages: Record<string, string> = {};
  for (const l of served) languages[hreflangCode(l)] = url(l);
  if (served.includes('zh-CN')) languages['zh'] = url('zh-CN');
  if (served.length > 0) languages['x-default'] = url(xDefaultLocale(served, brand));
  return languages;
}

/**
 * Locales that have their own version of a static route. Routes whose body is
 * Chinese-only content (`siteConfig().chineseOnlyRoutes`) are served in the
 * Chinese locales only; a foreign URL of such a route renders the Chinese text
 * and is noindex + canonicalised to zh-CN.
 */
export function staticRouteLocales(
  route: string,
  brand: Pick<Brand, 'allowedLocales'>,
  chineseOnlyRoutes: readonly string[]
): string[] {
  const all = [...brand.allowedLocales];
  return chineseOnlyRoutes.includes(route) ? all.filter((l) => l.startsWith('zh')) : all;
}
