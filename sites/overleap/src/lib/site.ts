import type nav from '../../messages/en-GB/nav.json';

/**
 * Site constants — the single place this app names itself.
 *
 * This app serves exactly one brand, so there is no brand registry and no
 * runtime brand switch. Anything that must agree with the Center API
 * (hosts, brand id) is checked against contracts/api-contract.json by
 * tests/cross-layer-contract.test.ts rather than shared as code.
 */

export const LOCALES = ['en-GB', 'en-US', 'en-AU', 'ja'] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = 'en-GB';

export function isLocale(value: string | undefined | null): value is Locale {
  return !!value && (LOCALES as readonly string[]).includes(value);
}

export const SITE = {
  /** Value of the X-K2-Brand header; Center resolves Host → X-K2-Brand → default. */
  brandId: 'overleap',
  name: 'Overleap',
  baseUrl: 'https://overleap.io',
  legalName: 'Overleap LLC',
  contactEmail: 'support@overleap.io',
  privacyEmail: 'privacy@overleap.io',
  legalEmail: 'legal@overleap.io',
  logoPath: '/overleap-icon.png',
  ogImagePath: '/overleap-og.png',
  faviconPrefix: '/brand/overleap',
  githubUrl: 'https://github.com/getoverleap',
} as const;

/** A key under the `nav` namespace (typed against the en-GB master). */
export type NavKey = Exclude<keyof typeof nav, 'footer'> | `footer.${keyof typeof nav.footer}`;

export interface NavItem {
  labelKey: NavKey;
  href: string;
  external?: boolean;
}

export const NAV: { primary: NavItem[] } = {
  primary: [{ labelKey: 'docs', href: '/k2' }],
};

export const FOOTER: { titleKey: NavKey; items: NavItem[] }[] = [
  {
    titleKey: 'footer.developers',
    items: [
      { labelKey: 'footer.k2Docs', href: '/k2' },
      { labelKey: 'footer.github', href: SITE.githubUrl, external: true },
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

/** Public, indexable static routes (sitemap). k2 docs are added from Velite. */
export const STATIC_ROUTES = ['', '/privacy', '/terms', '/delete-account'];
