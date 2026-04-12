/**
 * Supported-country helpers for the smart-mode Dashboard UX.
 *
 * Scope: the 14 country profiles currently routed by the Center
 * `suggestedProfile` hint (plus a minimal set of "travel" destinations that
 * may surface via `detectedCountry` without having a dedicated `{cc}route`
 * profile — those fall back to `global`).
 *
 * Intentionally NOT a replacement for `utils/country` / `i18n/countries` —
 * this file only covers what the smart-mode chip + travel banner need, so
 * new copy can ship without touching the larger i18n bundle.
 */

/** The 14 country codes with a dedicated `{cc}route` profile on Center. */
export const SUPPORTED_COUNTRY_CODES = [
  'cn', 'ir', 'ru', 'tr', 'pk', 'vn', 'mm',
  'eg', 'id', 'sa', 'ae', 'th', 'bd', 'by',
] as const;

export type SupportedCountryCode = typeof SUPPORTED_COUNTRY_CODES[number];

/**
 * Country names keyed by `en` / `zh` base language.
 *
 * Other locales (ja, zh-TW, zh-HK, en-AU, en-GB) fall back to the English
 * entries until professional translation lands.
 */
const COUNTRY_NAMES: Readonly<Record<string, Readonly<Record<string, string>>>> = {
  en: {
    cn: 'China',
    ir: 'Iran',
    ru: 'Russia',
    tr: 'Turkey',
    pk: 'Pakistan',
    vn: 'Vietnam',
    mm: 'Myanmar',
    eg: 'Egypt',
    id: 'Indonesia',
    sa: 'Saudi Arabia',
    ae: 'UAE',
    th: 'Thailand',
    bd: 'Bangladesh',
    by: 'Belarus',
    // Common travel destinations that resolve to the `global` profile.
    jp: 'Japan',
    kr: 'South Korea',
    us: 'United States',
    gb: 'United Kingdom',
    de: 'Germany',
    fr: 'France',
    sg: 'Singapore',
    hk: 'Hong Kong',
    tw: 'Taiwan',
    au: 'Australia',
    ca: 'Canada',
  },
  zh: {
    cn: '中国',
    ir: '伊朗',
    ru: '俄罗斯',
    tr: '土耳其',
    pk: '巴基斯坦',
    vn: '越南',
    mm: '缅甸',
    eg: '埃及',
    id: '印度尼西亚',
    sa: '沙特阿拉伯',
    ae: '阿联酋',
    th: '泰国',
    bd: '孟加拉国',
    by: '白俄罗斯',
    jp: '日本',
    kr: '韩国',
    us: '美国',
    gb: '英国',
    de: '德国',
    fr: '法国',
    sg: '新加坡',
    hk: '香港',
    tw: '台湾',
    au: '澳大利亚',
    ca: '加拿大',
  },
};

/**
 * Derive the flag emoji for an ISO 3166-1 alpha-2 country code.
 *
 * Uses the Regional Indicator Symbol trick: each A-Z letter maps to the
 * corresponding regional indicator (U+1F1E6 + offset). Two letters together
 * render as a flag glyph on any modern emoji font.
 *
 * Returns empty string for malformed input (missing, wrong length, non-ASCII).
 * Case-insensitive.
 */
export function countryFlagEmoji(cc: string | null | undefined): string {
  if (!cc || cc.length !== 2) return '';
  const lower = cc.toLowerCase();
  const a = lower.charCodeAt(0);
  const b = lower.charCodeAt(1);
  // Must be a-z.
  if (a < 97 || a > 122 || b < 97 || b > 122) return '';
  return (
    String.fromCodePoint(0x1F1E6 + (a - 97))
    + String.fromCodePoint(0x1F1E6 + (b - 97))
  );
}

/**
 * Localized country name for an ISO 3166-1 alpha-2 code.
 *
 * @param cc     Country code (case-insensitive)
 * @param locale i18next locale (e.g. `en-US`, `zh-CN`). Only the base
 *               language is consulted (`en` / `zh`); everything else falls
 *               back to English.
 *
 * If the code isn't in the hardcoded map, returns the uppercased code so
 * callers always get a non-empty, displayable string.
 */
export function countryName(cc: string | null | undefined, locale: string): string {
  if (!cc) return '';
  const lower = cc.toLowerCase();
  const base = (locale || 'en').split('-')[0].toLowerCase();
  const table = COUNTRY_NAMES[base] ?? COUNTRY_NAMES.en;
  return table[lower] ?? cc.toUpperCase();
}

/**
 * Convenience: is this country code one of the 14 with a dedicated profile?
 * Other countries detected by Center still surface in the UI but resolve to
 * the `global` profile at connect time.
 */
export function isSupportedCountry(cc: string | null | undefined): boolean {
  if (!cc) return false;
  return (SUPPORTED_COUNTRY_CODES as readonly string[]).includes(cc.toLowerCase());
}
