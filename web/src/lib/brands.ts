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
  wordmark: 'Kaitu.io',
  legalName: 'Kaitu LLC',
  baseUrl: 'https://kaitu.io',
  defaultLocale: 'zh-CN',
  allowedLocales: ALL_LOCALES,
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
  allowedLocales: ['en-US', 'en-GB', 'en-AU'],
  logoPath: '/overleap-icon.png',
  contactEmail: 'support@overleap.io',
  ogImagePath: '/overleap-og.png',
};

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
