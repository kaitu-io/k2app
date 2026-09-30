import type { Metadata } from 'next';
import { LOCALES, DEFAULT_LOCALE, SITE } from './site';

interface PageMeta {
  title: string;
  description: string;
  ogType?: 'website' | 'article';
  /** false for pages that must not be indexed (login, account). */
  index?: boolean;
}

/**
 * Per-page metadata. Every page passes its own locale-less `path` ('' for home,
 * '/privacy', '/terms') — canonical and hreflang are built from it, so
 * there is no request-header plumbing.
 *
 * Canonical / hreflang always use SITE.baseUrl (never a preview override), so a
 * preview deployment cannot poison the published SEO graph.
 */
export function pageMetadata(locale: string, path: string, meta: PageMeta): Metadata {
  const url = `${SITE.baseUrl}/${locale}${path}`;
  const languages: Record<string, string> = {};
  for (const l of LOCALES) languages[l.toLowerCase()] = `${SITE.baseUrl}/${l}${path}`;
  languages['x-default'] = `${SITE.baseUrl}/${DEFAULT_LOCALE}${path}`;
  const image = `${SITE.baseUrl}${SITE.ogImagePath}`;

  return {
    title: meta.title,
    description: meta.description,
    alternates: { canonical: url, languages },
    openGraph: {
      title: meta.title,
      description: meta.description,
      url,
      siteName: SITE.name,
      locale: locale.replace('-', '_'),
      type: meta.ogType ?? 'website',
      images: [{ url: image, width: 1200, height: 630, alt: SITE.name }],
    },
    twitter: { card: 'summary_large_image', title: meta.title, description: meta.description, images: [image] },
    robots: meta.index === false ? { index: false, follow: false } : undefined,
  };
}

export const ICONS: Metadata['icons'] = {
  icon: [16, 32].map((s) => ({ url: `${SITE.faviconPrefix}/favicon-${s}x${s}.png`, sizes: `${s}x${s}`, type: 'image/png' }))
    .concat([48, 96, 192, 512].map((s) => ({ url: `${SITE.faviconPrefix}/icon-${s}x${s}.png`, sizes: `${s}x${s}`, type: 'image/png' }))),
  shortcut: `${SITE.faviconPrefix}/favicon-32x32.png`,
  apple: [192, 512].map((s) => ({ url: `${SITE.faviconPrefix}/icon-${s}x${s}.png`, sizes: `${s}x${s}`, type: 'image/png' })),
};
