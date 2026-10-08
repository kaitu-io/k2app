import type nav from '../../messages/en-GB/nav.json';

/**
 * Site constants — the single place this app names itself.
 *
 * This app serves exactly one brand, so there is no brand registry and no
 * runtime brand switch. Anything that must agree with the Center API
 * (hosts, brand id) is checked against contracts/api-contract.json by
 * tests/cross-layer-contract.test.ts rather than shared as code.
 */

/** Served locales, in language-picker order. en-GB is the master. */
export const LOCALES = [
  'en-GB', 'en-US', 'en-AU',
  'es', 'pt-BR', 'fr', 'de', 'it', 'ru', 'tr',
  'ar', 'fa',
  'ja', 'ko',
  'id', 'ms', 'vi', 'th', 'my', 'km',
] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = 'en-GB';

/** `englishName` lets the picker's search match "korean" as well as "한국어". */
export const LOCALE_META: Record<Locale, { nativeName: string; englishName: string; dir: 'ltr' | 'rtl' }> = {
  'en-GB': { nativeName: 'English (UK)', englishName: 'English (UK)', dir: 'ltr' },
  'en-US': { nativeName: 'English (US)', englishName: 'English (US)', dir: 'ltr' },
  'en-AU': { nativeName: 'English (Australia)', englishName: 'English (Australia)', dir: 'ltr' },
  es: { nativeName: 'Español', englishName: 'Spanish', dir: 'ltr' },
  'pt-BR': { nativeName: 'Português (Brasil)', englishName: 'Portuguese (Brazil)', dir: 'ltr' },
  fr: { nativeName: 'Français', englishName: 'French', dir: 'ltr' },
  de: { nativeName: 'Deutsch', englishName: 'German', dir: 'ltr' },
  it: { nativeName: 'Italiano', englishName: 'Italian', dir: 'ltr' },
  ru: { nativeName: 'Русский', englishName: 'Russian', dir: 'ltr' },
  tr: { nativeName: 'Türkçe', englishName: 'Turkish', dir: 'ltr' },
  ar: { nativeName: 'العربية', englishName: 'Arabic', dir: 'rtl' },
  fa: { nativeName: 'فارسی', englishName: 'Persian', dir: 'rtl' },
  ja: { nativeName: '日本語', englishName: 'Japanese', dir: 'ltr' },
  ko: { nativeName: '한국어', englishName: 'Korean', dir: 'ltr' },
  id: { nativeName: 'Bahasa Indonesia', englishName: 'Indonesian', dir: 'ltr' },
  ms: { nativeName: 'Bahasa Melayu', englishName: 'Malay', dir: 'ltr' },
  vi: { nativeName: 'Tiếng Việt', englishName: 'Vietnamese', dir: 'ltr' },
  th: { nativeName: 'ไทย', englishName: 'Thai', dir: 'ltr' },
  my: { nativeName: 'မြန်မာ', englishName: 'Burmese', dir: 'ltr' },
  km: { nativeName: 'ខ្មែរ', englishName: 'Khmer', dir: 'ltr' },
};

export function isLocale(value: string | undefined | null): value is Locale {
  return !!value && (LOCALES as readonly string[]).includes(value);
}

/** Case-insensitive substring filter over native name, English name and code. */
export function filterLocales(query: string, locales: readonly Locale[] = LOCALES): Locale[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...locales];
  return locales.filter((l) => {
    const { nativeName, englishName } = LOCALE_META[l];
    return nativeName.toLowerCase().includes(q) || englishName.toLowerCase().includes(q) || l.toLowerCase().includes(q);
  });
}

export const SITE = {
  /** Value of the X-K2-Brand header; Center resolves Host → X-K2-Brand → default. */
  brandId: 'overleap',
  name: 'Overleap',
  baseUrl: 'https://overleap.io',
  legalName: 'Wordgate LLC',
  contactEmail: 'support@overleap.io',
  privacyEmail: 'privacy@overleap.io',
  legalEmail: 'legal@overleap.io',
  logoPath: '/overleap-icon.png',
  ogImagePath: '/overleap-og.png',
  faviconPrefix: '/brand/overleap',
} as const;

/** A key under the `nav` namespace (typed against the en-GB master). */
export type NavKey = Exclude<keyof typeof nav, 'footer'> | `footer.${keyof typeof nav.footer}`;

export interface NavItem {
  labelKey: NavKey;
  href: string;
  external?: boolean;
}

export const NAV: { primary: NavItem[]; cta: NavItem } = {
  // Few pages, so direct links; a label points at the same path in the header and the footer.
  primary: [
    { labelKey: 'features', href: '/#features' },
    { labelKey: 'pricing', href: '/pricing' },
    { labelKey: 'help', href: '/support' },
  ],
  cta: { labelKey: 'download', href: '/install' },
};

export const FOOTER: { titleKey: NavKey; items: NavItem[] }[] = [
  {
    titleKey: 'footer.product',
    items: [
      { labelKey: 'download', href: '/install' },
      { labelKey: 'pricing', href: '/pricing' },
      { labelKey: 'help', href: '/support' },
    ],
  },
  {
    titleKey: 'footer.company',
    items: [
      { labelKey: 'footer.privacy', href: '/privacy' },
      { labelKey: 'footer.terms', href: '/terms' },
      { labelKey: 'footer.deleteAccount', href: '/delete-account' },
      { labelKey: 'footer.contact', href: `mailto:${SITE.contactEmail}`, external: true },
    ],
  },
];

/** Public, indexable static routes (sitemap). */
export const STATIC_ROUTES = ['', '/install', '/pricing', '/purchase', '/support', '/privacy', '/terms', '/delete-account'];

/**
 * Static price table for the home and pricing pages, in minor units per currency.
 * Must equal the `ensure_price` lines of scripts/stripe-setup-overleap.sh, which
 * create the Stripe Prices Checkout actually charges (tests/pricing-source.test.ts).
 * The purchase page reads live prices from the API (`currencyPrices`) instead.
 */
export const PRICING = {
  yearly: { usd: 7900, gbp: 7900, eur: 8900 },
  monthly: { usd: 1199, gbp: 999, eur: 1199 },
  /** Center plan ids, as the purchase page sees them in GET /api/plans. */
  pids: { yearly: 'overleap-basic-1y', monthly: 'overleap-basic-1m' },
} as const;

/**
 * Download locations. Desktop artifacts: `<desktopBase>/<version>/Overleap_<version>_<arch>.<ext>`,
 * versions from `<desktopBase>/cloudfront.latest.json` (stable) and `/beta/cloudfront.latest.json`.
 * Mobile manifests: `<mobileBase>/{ios,android}/latest.json`. dl.overleap.io does not exist
 * yet — raw CloudFront until it is provisioned.
 */
export const DOWNLOADS = {
  desktopBases: ['https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop'],
  mobileBases: ['https://d13jc1jqzlg4yt.cloudfront.net/overleap'],
  artifactPrefix: 'Overleap',
  /** Store listings; '' = not live yet, the download page shows "Coming soon".
   *  iOS = App Store Connect app 6759199298, Android = Play package io.overleap (Play-only, no APK channel). */
  storeLinks: {
    ios: 'https://apps.apple.com/app/id6759199298',
    android: 'https://play.google.com/store/apps/details?id=io.overleap',
  },
} as const;
