// Mirrors routing.locales in `src/i18n/routing.ts`. Kept inline to avoid pulling
// `next-intl/navigation` (and transitively `next/navigation`) into vitest's module
// graph when non-component code imports brand config.
export const ALL_LOCALES = ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'] as const;
type Locale = (typeof ALL_LOCALES)[number];

export type BrandId = 'kaitu' | 'overleap';

export type Brand = {
  id: BrandId;
  displayName: string;
  wordmark: string;
  legalName: string;
  baseUrl: string;
  defaultLocale: Locale;
  allowedLocales: readonly Locale[];
  logoPath: string;
  contactEmail: string;
  ogImagePath: string;
  taglineZh?: string;
};

export const KAITU: Brand = {
  id: 'kaitu',
  displayName: 'Kaitu',
  wordmark: '开途',
  legalName: 'Kaitu LLC',
  baseUrl: 'https://kaitu.io',
  defaultLocale: 'zh-CN',
  allowedLocales: ['zh-CN', 'zh-TW', 'zh-HK'],
  logoPath: '/kaitu-icon.png',
  contactEmail: 'support@kaitu.me',
  ogImagePath: '/images/og-default.png',
  taglineZh: '愿上帝为你开路',
};

export const OVERLEAP: Brand = {
  id: 'overleap',
  displayName: 'Overleap',
  wordmark: 'Overleap',
  legalName: 'Overleap',
  baseUrl: 'https://overleap.io',
  defaultLocale: 'en-US',
  allowedLocales: ['en-US', 'en-GB', 'en-AU', 'ja'],
  logoPath: '/overleap-icon.png',
  contactEmail: 'support@overleap.io',
  ogImagePath: '/overleap-og.png',
};

const LOCALE_BRAND: Record<Locale, BrandId> = {
  'zh-CN': 'kaitu',
  'zh-TW': 'kaitu',
  'zh-HK': 'kaitu',
  'en-US': 'overleap',
  'en-GB': 'overleap',
  'en-AU': 'overleap',
  'ja': 'overleap',
};

export function ownerBrand(locale: string): BrandId {
  return LOCALE_BRAND[locale as Locale] ?? 'kaitu';
}

const HOST_MAP: Record<string, Brand> = {
  'kaitu.io': KAITU,
  'www.kaitu.io': KAITU,
  'overleap.io': OVERLEAP,
  'www.overleap.io': OVERLEAP,
};

export function brandFromHost(host: string | null | undefined): Brand {
  if (!host) return KAITU;
  const h = host.toLowerCase().split(':')[0];
  return HOST_MAP[h] ?? KAITU;
}

export function brandById(id: BrandId): Brand {
  return id === 'overleap' ? OVERLEAP : KAITU;
}
